package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const (
	CatalogIndexURL          = "https://raw.githubusercontent.com/tommilevs/Mounvix/main/plugins/index.json"
	CatalogSignatureURL      = "https://raw.githubusercontent.com/tommilevs/Mounvix/main/plugins/index.json.sig"
	maxCatalogIndexBytes     = 1 << 20
	maxSignatureEnvelopeSize = 4096
	maxCatalogArtifactBytes  = 256 << 20
	catalogAssetURLPrefix    = "/tommilevs/Mounvix/main/plugins/"
)

// CatalogKey identifies one trusted Ed25519 key used to verify the exact catalog bytes.
type CatalogKey struct {
	ID        string
	PublicKey ed25519.PublicKey
	Role      string
}

type signatureEnvelope struct {
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// Catalog is the signed plugin index. Only VerifyCatalog returns a trusted catalog.
type Catalog struct {
	Plugins  []CatalogEntry `json:"plugins"`
	verified bool
}

// CatalogEntry describes one plugin release and its platform packages.
type CatalogEntry struct {
	ID      string                  `json:"id"`
	Name    string                  `json:"name"`
	Version string                  `json:"version"`
	MinApp  string                  `json:"min_app,omitempty"`
	Arch    []string                `json:"arch"`
	Assets  map[string]CatalogAsset `json:"assets"`
	Home    string                  `json:"home,omitempty"`
}

// CatalogAsset is package metadata from a signed index. Its private trust bit
// is set only after VerifyCatalog has checked the detached signature.
type CatalogAsset struct {
	URL        string   `json:"url"`
	Size       int64    `json:"size"`
	SHA256     string   `json:"sha256"`
	Arch       string   `json:"arch,omitempty"`
	ID         string   `json:"-"`
	Version    string   `json:"-"`
	MinApp     string   `json:"-"`
	AppVersion string   `json:"-"`
	Format     string   `json:"-"`
	verified   bool     `json:"-"`
	binding    [32]byte `json:"-"`
}

func (asset *CatalogAsset) seal() {
	asset.verified = true
	asset.binding = assetBinding(*asset)
}

func (asset CatalogAsset) trusted() bool {
	return asset.verified && asset.binding == assetBinding(asset)
}

func assetBinding(asset CatalogAsset) [32]byte {
	canonical := strings.Join([]string{asset.ID, asset.Version, asset.MinApp, asset.AppVersion, asset.Format, asset.URL, fmt.Sprint(asset.Size), strings.ToLower(asset.SHA256), asset.Arch}, "\x00")
	return sha256.Sum256([]byte(canonical))
}

// CatalogStatus describes whether the server can safely mutate installed plugins.
type CatalogStatus struct {
	ReadOnly bool   `json:"read_only"`
	IndexURL string `json:"index_url"`
	Reason   string `json:"reason,omitempty"`
}

// DefaultCatalogStatus remains read-only until release keys and a signed index are provisioned.
func DefaultCatalogStatus() CatalogStatus {
	return CatalogStatus{ReadOnly: true, IndexURL: CatalogIndexURL, Reason: "release signing keys and signed catalog are not configured"}
}

// CatalogVerificationReady reports whether both current and next production key roles exist.
func CatalogVerificationReady(keys []CatalogKey) bool {
	var current, next *CatalogKey
	for i := range keys {
		key := &keys[i]
		if key.ID == "" || len(key.PublicKey) != ed25519.PublicKeySize {
			continue
		}
		switch key.Role {
		case "current":
			if current != nil {
				return false
			}
			current = key
		case "next":
			if next != nil {
				return false
			}
			next = key
		}
	}
	return current != nil && next != nil && current.ID != next.ID && string(current.PublicKey) != string(next.PublicKey)
}

// VerifyCatalog verifies the detached Ed25519 signature over the exact index bytes
// before decoding or validating the signed JSON document.
func VerifyCatalog(indexBytes, signatureBytes []byte, keys []CatalogKey) (Catalog, error) {
	var catalog Catalog
	if len(indexBytes) == 0 || len(indexBytes) > maxCatalogIndexBytes {
		return catalog, errors.New("catalog index size is invalid")
	}
	if len(signatureBytes) == 0 || len(signatureBytes) > maxSignatureEnvelopeSize {
		return catalog, errors.New("catalog signature envelope size is invalid")
	}
	var envelope signatureEnvelope
	if err := decodeSingleJSON(signatureBytes, &envelope); err != nil {
		return catalog, fmt.Errorf("decode catalog signature envelope: %w", err)
	}
	if envelope.KeyID == "" || envelope.Signature == "" {
		return catalog, errors.New("catalog signature envelope is incomplete")
	}
	var publicKey ed25519.PublicKey
	for _, key := range keys {
		if key.ID == envelope.KeyID {
			if len(key.PublicKey) != ed25519.PublicKeySize {
				return catalog, errors.New("catalog public key has invalid length")
			}
			publicKey = key.PublicKey
			break
		}
	}
	if len(publicKey) == 0 {
		return catalog, fmt.Errorf("unknown catalog signing key %q", envelope.KeyID)
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return catalog, errors.New("catalog signature encoding is invalid")
	}
	if !ed25519.Verify(publicKey, indexBytes, signature) {
		return catalog, errors.New("catalog signature verification failed")
	}
	if err := decodeSingleJSON(indexBytes, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode signed catalog: %w", err)
	}
	if err := catalog.validateAndMark(); err != nil {
		return Catalog{}, err
	}
	catalog.verified = true
	return catalog, nil
}

func decodeSingleJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("document must contain exactly one JSON value")
	}
	return nil
}

func (catalog *Catalog) validateAndMark() error {
	seen := make(map[string]bool, len(catalog.Plugins))
	for i := range catalog.Plugins {
		entry := &catalog.Plugins[i]
		if !pluginIDPattern.MatchString(entry.ID) || entry.ID == "rclone" {
			return fmt.Errorf("catalog contains invalid or reserved plugin id %q", entry.ID)
		}
		if seen[entry.ID] {
			return fmt.Errorf("catalog contains duplicate plugin id %q", entry.ID)
		}
		seen[entry.ID] = true
		if strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("catalog plugin %q has no name", entry.ID)
		}
		if _, err := parseVersion(entry.Version); err != nil {
			return fmt.Errorf("catalog plugin %q has invalid version: %w", entry.ID, err)
		}
		if entry.MinApp != "" {
			if _, err := parseVersion(entry.MinApp); err != nil {
				return fmt.Errorf("catalog plugin %q has invalid min_app: %w", entry.ID, err)
			}
		}
		if len(entry.Arch) == 0 || len(entry.Assets) == 0 {
			return fmt.Errorf("catalog plugin %q must declare architectures and assets", entry.ID)
		}
		archSeen := make(map[string]bool, len(entry.Arch))
		for _, arch := range entry.Arch {
			if strings.TrimSpace(arch) == "" || archSeen[normalizeCatalogArch(arch)] {
				return fmt.Errorf("catalog plugin %q has invalid or duplicate architecture", entry.ID)
			}
			archSeen[normalizeCatalogArch(arch)] = true
		}
		for format, asset := range entry.Assets {
			if format != "ipk" && format != "deb" {
				return fmt.Errorf("catalog plugin %q has unsupported asset format %q", entry.ID, format)
			}
			if !allowedCatalogAssetURL(asset.URL) {
				return fmt.Errorf("catalog plugin %q has an untrusted asset URL", entry.ID)
			}
			if asset.Size <= 0 || asset.Size > maxCatalogArtifactBytes || !validSHA256(asset.SHA256) {
				return fmt.Errorf("catalog plugin %q has invalid asset size or SHA-256", entry.ID)
			}
			if asset.Arch == "" {
				if len(entry.Arch) != 1 {
					return fmt.Errorf("catalog plugin %q asset %q must declare its architecture", entry.ID, format)
				}
				asset.Arch = entry.Arch[0]
			}
			if !containsCatalogArch(entry.Arch, asset.Arch) {
				return fmt.Errorf("catalog plugin %q asset %q architecture is not declared", entry.ID, format)
			}
			asset.ID = entry.ID
			asset.Version = entry.Version
			asset.MinApp = entry.MinApp
			asset.Format = format
			asset.seal()
			entry.Assets[format] = asset
		}
	}
	return nil
}

// SelectAsset returns one verified release asset after minimum-version and architecture checks.
func (catalog Catalog) SelectAsset(id, format, appVersion, arch string) (CatalogAsset, error) {
	if !catalog.verified {
		return CatalogAsset{}, errors.New("catalog is not signature-verified")
	}
	if !pluginIDPattern.MatchString(id) || id == "rclone" {
		return CatalogAsset{}, errors.New("invalid or reserved plugin id")
	}
	for _, entry := range catalog.Plugins {
		if entry.ID != id {
			continue
		}
		asset, ok := entry.Assets[format]
		if !ok || !asset.trusted() || asset.ID != id || asset.Format != format {
			return CatalogAsset{}, fmt.Errorf("verified %s asset is unavailable", format)
		}
		if asset.MinApp != "" {
			if strings.TrimSpace(appVersion) == "" {
				return CatalogAsset{}, errors.New("application version is required for min_app validation")
			}
			minimum, err := parseVersion(asset.MinApp)
			if err != nil {
				return CatalogAsset{}, fmt.Errorf("invalid minimum application version: %w", err)
			}
			current, err := parseVersion(appVersion)
			if err != nil {
				return CatalogAsset{}, fmt.Errorf("invalid application version: %w", err)
			}
			if compareVersion(current, minimum) < 0 {
				return CatalogAsset{}, fmt.Errorf("plugin requires app version %s", asset.MinApp)
			}
		}
		if !catalogArchEqual(asset.Arch, arch) {
			return CatalogAsset{}, fmt.Errorf("asset architecture %s does not match %s", asset.Arch, arch)
		}
		asset.AppVersion = appVersion
		asset.seal()
		return asset, nil
	}
	return CatalogAsset{}, errors.New("plugin is not present in the signed catalog")
}

func containsCatalogArch(architectures []string, target string) bool {
	for _, arch := range architectures {
		if catalogArchEqual(arch, target) {
			return true
		}
	}
	return false
}

func catalogArchEqual(left, right string) bool {
	return normalizeCatalogArch(left) == normalizeCatalogArch(right)
}

func normalizeCatalogArch(arch string) string {
	switch arch {
	case "aarch64":
		return "arm64"
	case "x86_64":
		return "amd64"
	case "armv7l":
		return "arm"
	default:
		return arch
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func allowedCatalogAssetURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "raw.githubusercontent.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.Opaque != "" {
		return false
	}
	if strings.Contains(parsed.EscapedPath(), "%") || path.Clean(parsed.Path) != parsed.Path || !strings.HasPrefix(parsed.Path, catalogAssetURLPrefix) {
		return false
	}
	relative := strings.TrimPrefix(parsed.Path, catalogAssetURLPrefix)
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// VerifiedArtifact is a temporary package file bound to verified catalog metadata.
type VerifiedArtifact struct {
	ID         string
	Version    string
	Arch       string
	AppVersion string
	Format     string
	Path       string
	SHA256     string
	verified   bool
	binding    [32]byte
}

func (artifact *VerifiedArtifact) seal() {
	artifact.verified = true
	artifact.binding = artifactBinding(*artifact)
}

func (artifact VerifiedArtifact) trusted() bool {
	return artifact.verified && artifact.binding == artifactBinding(artifact)
}

func artifactBinding(artifact VerifiedArtifact) [32]byte {
	canonical := strings.Join([]string{artifact.ID, artifact.Version, artifact.Arch, artifact.AppVersion, artifact.Format, artifact.Path, strings.ToLower(artifact.SHA256)}, "\x00")
	return sha256.Sum256([]byte(canonical))
}

// Cleanup removes the temporary artifact file produced by FetchVerifiedArtifact.
func (artifact VerifiedArtifact) Cleanup() error {
	if !artifact.trusted() || artifact.Path == "" {
		return errors.New("artifact is not a verified temporary file")
	}
	if err := os.Remove(artifact.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove verified artifact: %w", err)
	}
	return nil
}

// FetchVerifiedArtifact downloads a catalog-bound HTTPS artifact and verifies its exact size and SHA-256.
func FetchVerifiedArtifact(ctx context.Context, asset CatalogAsset) (VerifiedArtifact, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	return fetchVerifiedArtifactWithClient(ctx, asset, client)
}

func fetchVerifiedArtifactWithClient(ctx context.Context, asset CatalogAsset, client *http.Client) (VerifiedArtifact, error) {
	if ctx == nil {
		return VerifiedArtifact{}, errors.New("artifact download context is required")
	}
	if !asset.trusted() || !allowedCatalogAssetURL(asset.URL) || asset.Size <= 0 || asset.Size > maxCatalogArtifactBytes || !validSHA256(asset.SHA256) || !validInstallID(asset.ID) || asset.Version == "" || (asset.Format != "ipk" && asset.Format != "deb") || asset.Arch == "" {
		return VerifiedArtifact{}, errors.New("artifact metadata is not trusted or is invalid")
	}
	if client == nil {
		return VerifiedArtifact{}, errors.New("artifact HTTP client is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return VerifiedArtifact{}, fmt.Errorf("create artifact request: %w", err)
	}
	request.Header.Set("Accept-Encoding", "identity")
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safeClient.Do(request)
	if err != nil {
		return VerifiedArtifact{}, fmt.Errorf("download verified artifact: %w", err)
	}
	if response.Body == nil {
		return VerifiedArtifact{}, errors.New("artifact response has no body")
	}
	if response.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("artifact server returned HTTP %d", response.StatusCode)
		if closeErr := response.Body.Close(); closeErr != nil {
			return VerifiedArtifact{}, errors.Join(statusErr, fmt.Errorf("close artifact response: %w", closeErr))
		}
		return VerifiedArtifact{}, statusErr
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, asset.Size+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return VerifiedArtifact{}, fmt.Errorf("read artifact response: %w", readErr)
	}
	if closeErr != nil {
		return VerifiedArtifact{}, fmt.Errorf("close artifact response: %w", closeErr)
	}
	if response.ContentLength >= 0 && response.ContentLength != asset.Size {
		return VerifiedArtifact{}, errors.New("artifact Content-Length does not match signed size")
	}
	if int64(len(body)) != asset.Size {
		return VerifiedArtifact{}, errors.New("artifact size does not match signed size")
	}
	digest := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), asset.SHA256) {
		return VerifiedArtifact{}, errors.New("artifact SHA-256 does not match signed digest")
	}
	file, err := os.CreateTemp("", "mounvix-plugin-*."+asset.Format)
	if err != nil {
		return VerifiedArtifact{}, fmt.Errorf("create temporary artifact: %w", err)
	}
	filePath := file.Name()
	if _, err = file.Write(body); err != nil {
		cleanupErr := removePartialArtifact(file)
		return VerifiedArtifact{}, errors.Join(fmt.Errorf("write temporary artifact: %w", err), cleanupErr)
	}
	if err = file.Sync(); err != nil {
		cleanupErr := removePartialArtifact(file)
		return VerifiedArtifact{}, errors.Join(fmt.Errorf("sync temporary artifact: %w", err), cleanupErr)
	}
	if err = file.Close(); err != nil {
		removeErr := os.Remove(filePath)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return VerifiedArtifact{}, errors.Join(fmt.Errorf("close temporary artifact: %w", err), wrapCleanupError(removeErr))
	}
	artifact := VerifiedArtifact{ID: asset.ID, Version: asset.Version, Arch: asset.Arch, AppVersion: asset.AppVersion, Format: asset.Format, Path: filePath, SHA256: strings.ToLower(asset.SHA256)}
	artifact.seal()
	return artifact, nil
}

func removePartialArtifact(file *os.File) error {
	closeErr := file.Close()
	removeErr := os.Remove(file.Name())
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, wrapCleanupError(removeErr))
}

func wrapCleanupError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("remove partial artifact: %w", err)
}
