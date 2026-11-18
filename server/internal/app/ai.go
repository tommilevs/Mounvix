package app

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tommilevs/Mounvix/server/internal/ai"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

type aiView struct {
	Title   string
	Lang    string
	User    *User
	Cfg     Config
	Version string
	Result  string
	Msg     string
	Err     string
}

func (a *App) aiClient() *ai.Client {
	return &ai.Client{BaseURL: a.cfg.AIBaseURL, Model: a.cfg.AIModel, Key: a.cfg.AIKey}
}

// describeImage читает файл из корня и просит модель описать изображение.
func (a *App) describeImage(ctx context.Context, rootName, rel, prompt string) (string, error) {
	root, ok := a.findRoot(rootName)
	if !ok {
		return "", errors.New("root not found")
	}
	full, err := safeJoin(root.Path, rel)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", errors.New("file not found")
	}
	if st.IsDir() {
		return "", errors.New("path is a directory")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(full)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return a.aiClient().Describe(ctx, data, mimeType, prompt)
}

func (a *App) handleAIPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	v := &aiView{Title: "AI", Lang: a.cfg.Lang, Version: version.Version, User: u, Cfg: a.cfg}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		switch r.PostFormValue("action") {
		case "save":
			a.cfg.AIBaseURL = strings.TrimSpace(r.PostFormValue("base_url"))
			a.cfg.AIModel = strings.TrimSpace(r.PostFormValue("model"))
			a.cfg.AIKey = r.PostFormValue("key")
			if err := SaveConfig(a.cfgPath, a.cfg); err != nil {
				v.Err = err.Error()
			} else {
				v.Msg = "Настройки AI сохранены"
				a.store.Audit(u.Name, "ai_config", a.cfg.AIBaseURL+" "+a.cfg.AIModel)
			}
			v.Cfg = a.cfg
		case "describe":
			text, err := a.describeImage(r.Context(),
				r.PostFormValue("root"), r.PostFormValue("path"), r.PostFormValue("prompt"))
			if err != nil {
				v.Err = err.Error()
			} else {
				v.Result = text
				a.store.Audit(u.Name, "ai_describe", r.PostFormValue("root")+"/"+r.PostFormValue("path"))
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "ai.html", v)
}

func (a *App) handleAIStatusAPI(w http.ResponseWriter, r *http.Request) {
	_, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{
		"configured": a.cfg.AIBaseURL != "" && a.cfg.AIModel != "",
		"base_url":   a.cfg.AIBaseURL,
		"model":      a.cfg.AIModel,
	})
}

func (a *App) handleAIDescribeAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Root   string `json:"root"`
		Path   string `json:"path"`
		Prompt string `json:"prompt"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Root == "" || req.Path == "" {
		_ = r.ParseForm()
		if req.Root == "" {
			req.Root = r.PostFormValue("root")
		}
		if req.Path == "" {
			req.Path = r.PostFormValue("path")
		}
		req.Prompt = r.PostFormValue("prompt")
	}
	text, err := a.describeImage(r.Context(), req.Root, req.Path, req.Prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.store.Audit(u.Name, "ai_describe", req.Root+"/"+req.Path)
	writeJSON(w, map[string]any{"text": text})
}
