package backup

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href  string        `xml:"href"`
	Props []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Prop davProp `xml:"prop"`
}

type davProp struct {
	ResourceType davResType `xml:"resourcetype"`
	Length       string     `xml:"getcontentlength"`
}

type davResType struct {
	Collection *struct{} `xml:"collection"`
}

type davEntry struct {
	rel        string
	collection bool
}

func (m *Manager) restoreWebDAV(ctx context.Context, job *Job, t Target, dest string, rep *Report) error {
	client := &http.Client{Timeout: 120 * time.Second}
	base, err := url.Parse(strings.TrimRight(t.URL, "/"))
	if err != nil {
		return fmt.Errorf("webdav url: %w", err)
	}
	if base.Path == "" {
		base.Path = "/"
	}
	return m.restoreDAVDir(ctx, client, base, t, "", dest, rep)
}

func (m *Manager) restoreDAVDir(ctx context.Context, client *http.Client, base *url.URL, t Target, rel, dest string, rep *Report) error {
	entries, err := listDAV(ctx, client, base, t, rel)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.collection {
			if err := m.restoreDAVDir(ctx, client, base, t, e.rel, dest, rep); err != nil {
				rep.Errors = append(rep.Errors, e.rel+": "+err.Error())
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := downloadDAV(ctx, client, base, t, e.rel, filepath.Join(dest, filepath.FromSlash(e.rel)))
		if err != nil {
			rep.Errors = append(rep.Errors, e.rel+": "+err.Error())
			continue
		}
		rep.Files++
		rep.Bytes += n
	}
	return nil
}

func listDAV(ctx context.Context, client *http.Client, base *url.URL, t Target, rel string) ([]davEntry, error) {
	u := *base
	requestDir := strings.TrimRight(base.Path, "/")
	relClean := strings.Trim(strings.TrimPrefix(rel, "/"), "/")
	full := requestDir
	if relClean != "" {
		full += "/" + relClean
	}
	u.Path = full
	body := `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/></d:prop></d:propfind>`
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", u.String(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	applyAuth(req, t)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("PROPFIND %d", resp.StatusCode)
	}
	var ms davMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("multistatus: %w", err)
	}
	var out []davEntry
	for _, r := range ms.Responses {
		p := r.Href
		if parsed, err := url.Parse(r.Href); err == nil && parsed.Path != "" {
			p = parsed.Path
		}
		relPath := strings.Trim(strings.TrimPrefix(p, full), "/")
		if relPath == "" || strings.Contains(relPath, "/") {
			continue
		}
		col := false
		for _, ps := range r.Props {
			if ps.Prop.ResourceType.Collection != nil {
				col = true
			}
		}
		childRel := relPath
		if relClean != "" {
			childRel = relClean + "/" + relPath
		}
		out = append(out, davEntry{rel: childRel, collection: col})
	}
	return out, nil
}

func downloadDAV(ctx context.Context, client *http.Client, base *url.URL, t Target, rel, dst string) (int64, error) {
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(rel, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	applyAuth(req, t)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, resp.Body)
		return 0, fmt.Errorf("GET %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, resp.Body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return 0, err
	}
	return n, nil
}

var _ = path.Base
