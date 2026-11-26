package plugins

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const testAssetURL = "https://raw.githubusercontent.com/tommilevs/Mounvix/main/plugins/demo/1.0.0/demo.ipk"

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func catalogTestKeys(t *testing.T) (CatalogKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return CatalogKey{ID: "test-current", PublicKey: publicKey, Role: "current"}, privateKey
}

func signCatalogForTest(t *testing.T, keyID string, privateKey ed25519.PrivateKey, index []byte) []byte {
	t.Helper()
	envelope, err := json.Marshal(signatureEnvelope{KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, index))})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func marshalTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func catalogIndexForTest(t *testing.T, assetURL, digest string, size int64, minApp string, arch []string) []byte {
	t.Helper()
	catalog := Catalog{Plugins: []CatalogEntry{{
		ID: "demo", Name: "Demo", Version: "1.0.0", MinApp: minApp, Arch: arch,
		Assets: map[string]CatalogAsset{"ipk": {URL: assetURL, Size: size, SHA256: digest, Arch: "aarch64"}},
		Home:   "https://github.com/tommilevs/Mounvix",
	}}}
	index, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func TestVerifyCatalogSignatureAndExactRawBytes(t *testing.T) {
	key, privateKey := catalogTestKeys(t)
	payload := []byte("plugin package bytes")
	digest := sha256.Sum256(payload)
	index := catalogIndexForTest(t, testAssetURL, hex.EncodeToString(digest[:]), int64(len(payload)), "0.1.0", []string{"aarch64"})
	envelope := signCatalogForTest(t, key.ID, privateKey, index)

	catalog, err := VerifyCatalog(index, envelope, []CatalogKey{key})
	if err != nil {
		t.Fatalf("VerifyCatalog(): %v", err)
	}
	asset, err := catalog.SelectAsset("demo", "ipk", "v0.1.3", "aarch64")
	if err != nil {
		t.Fatalf("SelectAsset(): %v", err)
	}
	if !asset.verified || asset.ID != "demo" || asset.Version != "1.0.0" || asset.Format != "ipk" {
		t.Fatalf("asset was not bound to the signed catalog entry: %+v", asset)
	}

	tampered := append(append([]byte(nil), index...), '\n')
	if _, err := VerifyCatalog(tampered, envelope, []CatalogKey{key}); err == nil {
		t.Fatal("signature over different raw bytes was accepted")
	}
}

func TestVerifyCatalogRejectsInvalidSignatureAndMetadata(t *testing.T) {
	key, privateKey := catalogTestKeys(t)
	index := catalogIndexForTest(t, testAssetURL, strings.Repeat("a", 64), 20, "0.1.0", []string{"aarch64"})
	validEnvelope := signCatalogForTest(t, key.ID, privateKey, index)
	invalidSignature := marshalTestJSON(t, signatureEnvelope{KeyID: key.ID, Signature: base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))})
	invalidBase64 := marshalTestJSON(t, signatureEnvelope{KeyID: key.ID, Signature: "%%%"})
	unknownKey := marshalTestJSON(t, signatureEnvelope{KeyID: "unknown", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, index))})
	malformedJSON := []byte("{not-json")

	tests := []struct {
		name     string
		index    []byte
		envelope []byte
		keys     []CatalogKey
	}{
		{"unknown key", index, unknownKey, []CatalogKey{key}},
		{"invalid base64", index, invalidBase64, []CatalogKey{key}},
		{"invalid signature", index, invalidSignature, []CatalogKey{key}},
		{"malformed index after valid signature", malformedJSON, signCatalogForTest(t, key.ID, privateKey, malformedJSON), []CatalogKey{key}},
		{"unsigned catalog", index, validEnvelope, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := VerifyCatalog(tt.index, tt.envelope, tt.keys); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func TestVerifyCatalogRejectsReservedIDAndUntrustedAssetURL(t *testing.T) {
	key, privateKey := catalogTestKeys(t)
	tests := []struct {
		name string
		id   string
		url  string
	}{
		{"reserved builtin ID", "rclone", testAssetURL},
		{"non-HTTPS asset", "demo", "http://raw.githubusercontent.com/tommilevs/Mounvix/main/plugins/demo/demo.ipk"},
		{"foreign repository", "demo", "https://raw.githubusercontent.com/attacker/Mounvix/main/plugins/demo/demo.ipk"},
		{"foreign path", "demo", "https://raw.githubusercontent.com/tommilevs/Mounvix/other/plugins/demo/demo.ipk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := Catalog{Plugins: []CatalogEntry{{ID: tt.id, Name: "Demo", Version: "1.0.0", Arch: []string{"aarch64"}, Assets: map[string]CatalogAsset{"ipk": {URL: tt.url, Size: 20, SHA256: strings.Repeat("a", 64), Arch: "aarch64"}}}}}
			index, err := json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			envelope := signCatalogForTest(t, key.ID, privateKey, index)
			if _, err := VerifyCatalog(index, envelope, []CatalogKey{key}); err == nil {
				t.Fatal("untrusted catalog metadata accepted")
			}
		})
	}
}

func TestCatalogSelectAssetRejectsUnsupportedArchAndMinApp(t *testing.T) {
	key, privateKey := catalogTestKeys(t)
	index := catalogIndexForTest(t, testAssetURL, strings.Repeat("a", 64), 20, "2.0.0", []string{"aarch64"})
	catalog, err := VerifyCatalog(index, signCatalogForTest(t, key.ID, privateKey, index), []CatalogKey{key})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SelectAsset("demo", "ipk", "1.0.0", "aarch64"); err == nil {
		t.Fatal("plugin requiring newer Mounvix version was selected")
	}
	if _, err := catalog.SelectAsset("demo", "ipk", "2.0.0", "amd64"); err == nil {
		t.Fatal("asset for unsupported architecture was selected")
	}
	if _, err := catalog.SelectAsset("demo", "ipk", "", "aarch64"); err == nil {
		t.Fatal("plugin with min_app was selected when app version was unknown")
	}
	if _, err := catalog.SelectAsset("demo", "ipk", "dev", "aarch64"); err == nil {
		t.Fatal("development version bypassed the signed min_app requirement")
	}
	if _, err := catalog.SelectAsset("demo", "ipk", "2.0.0", "aarch64"); err != nil {
		t.Fatalf("compatible asset rejected: %v", err)
	}
	catalog.Plugins[0].MinApp = ""
	if _, err := catalog.SelectAsset("demo", "ipk", "1.0.0", "aarch64"); err == nil {
		t.Fatal("mutating public catalog metadata bypassed the signed minimum version")
	}
}

func TestFetchVerifiedArtifactChecksSizeAndSHA256(t *testing.T) {
	body := []byte("verified package bytes")
	digest := sha256.Sum256(body)
	key, privateKey := catalogTestKeys(t)
	index := catalogIndexForTest(t, testAssetURL, hex.EncodeToString(digest[:]), int64(len(body)), "", []string{"aarch64"})
	catalog, err := VerifyCatalog(index, signCatalogForTest(t, key.ID, privateKey, index), []CatalogKey{key})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := catalog.SelectAsset("demo", "ipk", "dev", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: testRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != testAssetURL {
			return nil, errors.New("unexpected request URL")
		}
		if req.Header.Get("Accept-Encoding") != "identity" {
			return nil, errors.New("artifact request did not disable content encoding")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
	})}
	artifact, err := fetchVerifiedArtifactWithClient(context.Background(), asset, client)
	if err != nil {
		t.Fatalf("fetch artifact: %v", err)
	}
	t.Cleanup(func() {
		if err := artifact.Cleanup(); err != nil {
			t.Errorf("cleanup verified artifact: %v", err)
		}
	})
	if artifact.ID != "demo" || artifact.Version != "1.0.0" || artifact.Arch != "aarch64" || artifact.Format != "ipk" {
		t.Fatalf("artifact metadata: %+v", artifact)
	}
	got, err := os.ReadFile(artifact.Path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("artifact bytes=%q err=%v", got, err)
	}

	wrongHashIndex := catalogIndexForTest(t, testAssetURL, strings.Repeat("0", 64), int64(len(body)), "", []string{"aarch64"})
	wrongCatalog, err := VerifyCatalog(wrongHashIndex, signCatalogForTest(t, key.ID, privateKey, wrongHashIndex), []CatalogKey{key})
	if err != nil {
		t.Fatal(err)
	}
	wrongAsset, err := wrongCatalog.SelectAsset("demo", "ipk", "dev", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetchVerifiedArtifactWithClient(context.Background(), wrongAsset, client); err == nil {
		t.Fatal("artifact with a mismatched SHA-256 was accepted")
	}

	wrongSizeIndex := catalogIndexForTest(t, testAssetURL, hex.EncodeToString(digest[:]), int64(len(body)+1), "", []string{"aarch64"})
	wrongSizeCatalog, err := VerifyCatalog(wrongSizeIndex, signCatalogForTest(t, key.ID, privateKey, wrongSizeIndex), []CatalogKey{key})
	if err != nil {
		t.Fatal(err)
	}
	wrongSizeAsset, err := wrongSizeCatalog.SelectAsset("demo", "ipk", "dev", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetchVerifiedArtifactWithClient(context.Background(), wrongSizeAsset, client); err == nil {
		t.Fatal("artifact with a mismatched size was accepted")
	}

	redirectRequests := 0
	redirectClient := &http.Client{Transport: testRoundTripper(func(req *http.Request) (*http.Response, error) {
		redirectRequests++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://example.invalid/payload"}}, Body: io.NopCloser(bytes.NewReader(nil)), ContentLength: 0, Request: req}, nil
	})}
	if _, err := fetchVerifiedArtifactWithClient(context.Background(), asset, redirectClient); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirect response was not rejected as HTTP 302: %v", err)
	}
	if redirectRequests != 1 {
		t.Fatalf("redirect was followed: requests=%d", redirectRequests)
	}
}

func TestFetchVerifiedArtifactRejectsForgedOrMutatedAsset(t *testing.T) {
	asset := CatalogAsset{URL: testAssetURL, Size: 10, SHA256: strings.Repeat("a", 64)}
	if _, err := FetchVerifiedArtifact(context.Background(), asset); err == nil {
		t.Fatal("asset not derived from a verified catalog was fetched")
	}

	body := []byte("verified payload")
	digest := sha256.Sum256(body)
	key, privateKey := catalogTestKeys(t)
	index := catalogIndexForTest(t, testAssetURL, hex.EncodeToString(digest[:]), int64(len(body)), "", []string{"aarch64"})
	catalog, err := VerifyCatalog(index, signCatalogForTest(t, key.ID, privateKey, index), []CatalogKey{key})
	if err != nil {
		t.Fatal(err)
	}
	verifiedAsset, err := catalog.SelectAsset("demo", "ipk", "dev", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	verifiedAsset.URL = "https://raw.githubusercontent.com/tommilevs/Mounvix/main/plugins/attacker/payload.ipk"
	requests := 0
	client := &http.Client{Transport: testRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
	})}
	if _, err := fetchVerifiedArtifactWithClient(context.Background(), verifiedAsset, client); err == nil {
		t.Fatal("mutated signed asset URL was accepted")
	}
	if requests != 0 {
		t.Fatal("HTTP request was sent for a mutated asset")
	}
}

func TestCatalogVerificationReadyRequiresDistinctCurrentAndNextKeys(t *testing.T) {
	current, _ := catalogTestKeys(t)
	next, _ := catalogTestKeys(t)
	next.ID = "test-next"
	next.Role = "next"
	if !CatalogVerificationReady([]CatalogKey{current, next}) {
		t.Fatal("distinct current and next public keys were not recognized")
	}
	next.PublicKey = current.PublicKey
	if CatalogVerificationReady([]CatalogKey{current, next}) {
		t.Fatal("same public key was accepted for current and next roles")
	}
}

func TestProductionCatalogKeysMatchPinnedFingerprints(t *testing.T) {
	keys := ProductionCatalogKeys()
	if len(keys) != 2 || !CatalogVerificationReady(keys) {
		t.Fatalf("production current/next keys are not ready: %+v", keys)
	}
	want := []struct {
		id          string
		role        string
		fingerprint string
	}{
		{CatalogCurrentKeyID, "current", catalogCurrentFingerprint},
		{CatalogNextKeyID, "next", catalogNextFingerprint},
	}
	for i, expected := range want {
		if keys[i].ID != expected.id || keys[i].Role != expected.role {
			t.Fatalf("production key %d identity=%+v", i, keys[i])
		}
		fingerprint := sha256.Sum256(keys[i].PublicKey)
		if got := hex.EncodeToString(fingerprint[:]); got != expected.fingerprint {
			t.Fatalf("production key %q fingerprint=%s, want %s", keys[i].ID, got, expected.fingerprint)
		}
	}
}

func TestProductionCatalogStatusRemainsReadOnlyUntilVerifiedFetch(t *testing.T) {
	if CatalogVerificationReady(nil) {
		t.Fatal("empty production key set reported ready")
	}
	if status := DefaultCatalogStatus(); !status.ReadOnly || status.IndexURL != CatalogIndexURL {
		t.Fatalf("catalog should remain read-only before a verified fetch: %+v", status)
	}
}

func TestSignCatalogUsesTheExactBytesAndOnlyPinnedPrivateKeys(t *testing.T) {
	current, currentPrivate := catalogTestKeys(t)
	next, _ := catalogTestKeys(t)
	next.ID = "test-next"
	next.Role = "next"
	keys := []CatalogKey{current, next}
	index := []byte("{ \n  \"plugins\" : [ ]\n}\n")
	signature, err := SignCatalog(index, current.ID, currentPrivate, keys)
	if err != nil {
		t.Fatalf("SignCatalog(): %v", err)
	}
	catalog, err := VerifyCatalog(index, signature, keys)
	if err != nil || !catalog.Verified() || catalog.SignerKeyID != current.ID {
		t.Fatalf("signed catalog verified=%v signer=%q err=%v", catalog.Verified(), catalog.SignerKeyID, err)
	}
	tampered := append([]byte(nil), index...)
	tampered[0] = 'x'
	if _, err := VerifyCatalog(tampered, signature, keys); err == nil {
		t.Fatal("signature accepted bytes different from the exact signed index")
	}
	_, wrongPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignCatalog(index, current.ID, wrongPrivate, keys); err == nil {
		t.Fatal("signing with a private key not matching the pinned key ID succeeded")
	}
	if _, err := SignCatalog(index, "unknown-key", currentPrivate, keys); err == nil {
		t.Fatal("signing with an unknown key ID succeeded")
	}
}

func TestCatalogFetcherVerifiesPinnedRawDocuments(t *testing.T) {
	current, currentPrivate := catalogTestKeys(t)
	next, _ := catalogTestKeys(t)
	next.ID = "test-next"
	next.Role = "next"
	index := []byte("{\"plugins\":[]}\n")
	signature := signCatalogForTest(t, current.ID, currentPrivate, index)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %q", r.Method)
		}
		switch r.URL.Path {
		case "/plugins/index.json":
			_, _ = w.Write(index)
		case "/plugins/index.json.sig":
			_, _ = w.Write(signature)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := newCatalogFetcher(server.Client(), server.URL+"/plugins/index.json", server.URL+"/plugins/index.json.sig", []CatalogKey{current, next})
	catalog, err := fetcher.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if !catalog.verified || catalog.SignerKeyID != current.ID || len(catalog.Plugins) != 0 {
		t.Fatalf("unexpected verified catalog: %+v", catalog)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetcher.Fetch(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch with a canceled context returned %v, want context.Canceled", err)
	}
	cached, err := fetcher.Fetch(context.Background())
	if err != nil || !cached.Verified() {
		t.Fatalf("cached Fetch() verified=%v err=%v", cached.Verified(), err)
	}
	if requests != 2 {
		t.Fatalf("expected one index/signature request pair while the verified cache is fresh, got %d requests", requests)
	}
}

func TestCatalogFetcherRejectsRedirectsAndOversizedDocuments(t *testing.T) {
	current, _ := catalogTestKeys(t)
	next, _ := catalogTestKeys(t)
	next.ID = "test-next"
	next.Role = "next"
	for _, test := range []struct {
		name      string
		body      []byte
		status    int
		wantError string
	}{
		{name: "redirect", status: http.StatusFound, wantError: "HTTP 302"},
		{name: "oversized index", body: bytes.Repeat([]byte("x"), maxCatalogIndexBytes+1), status: http.StatusOK, wantError: "size limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: testRoundTripper(func(req *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{
					StatusCode:    test.status,
					Header:        http.Header{"Location": []string{"https://attacker.example/index.json"}},
					Body:          io.NopCloser(bytes.NewReader(test.body)),
					ContentLength: int64(len(test.body)),
					Request:       req,
				}, nil
			})}
			fetcher := newCatalogFetcher(client, "https://catalog.example/plugins/index.json", "https://catalog.example/plugins/index.json.sig", []CatalogKey{current, next})
			if _, err := fetcher.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Fetch() error=%v, want containing %q", err, test.wantError)
			}
			if requests != 1 {
				t.Fatalf("expected one request and no redirect or follow-up, got %d", requests)
			}
		})
	}
}
