package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
)

func TestRunWithKeysSignsAndVerifiesExactRawBytes(t *testing.T) {
	currentPublic, currentPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nextPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := []plugins.CatalogKey{
		{ID: "test-current", PublicKey: currentPublic, Role: "current"},
		{ID: "test-next", PublicKey: nextPublic, Role: "next"},
	}
	index := []byte("{ \"plugins\" : [] }\n")
	keyDER, err := x509.MarshalPKCS8PrivateKey(currentPrivate)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.json")
	keyPath := filepath.Join(dir, "current-private.pem")
	signaturePath := filepath.Join(dir, "index.json.sig")
	if err := os.WriteFile(indexPath, index, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"--key-id", keys[0].ID, "--key-file", keyPath, "--index", indexPath, "--signature", signaturePath}
	var stdout, stderr bytes.Buffer
	if err := runWithKeys(args, keys, &stdout, &stderr); err != nil {
		t.Fatalf("sign catalog: %v; stderr=%s", err, stderr.String())
	}
	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := plugins.VerifyCatalog(index, signature, keys)
	if err != nil || !catalog.Verified() || catalog.SignerKeyID != keys[0].ID {
		t.Fatalf("signed raw catalog did not verify: catalog=%+v err=%v", catalog, err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runWithKeys([]string{"--verify", "--index", indexPath, "--signature", signaturePath}, keys, &stdout, &stderr); err != nil {
		t.Fatalf("verify signed catalog: %v; stderr=%s", err, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("verified catalog signed by test-current")) {
		t.Fatalf("verification output=%q", stdout.String())
	}

	if err := os.WriteFile(indexPath, []byte("{\"plugins\":[]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWithKeys([]string{"--verify", "--index", indexPath, "--signature", signaturePath}, keys, &stdout, &stderr); err == nil {
		t.Fatal("verification accepted bytes different from the exact signed catalog")
	}
}

func TestRunWithKeysRejectsPEMWithUnexpectedPrefix(t *testing.T) {
	currentPublic, currentPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nextPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := []plugins.CatalogKey{
		{ID: "test-current", PublicKey: currentPublic, Role: "current"},
		{ID: "test-next", PublicKey: nextPublic, Role: "next"},
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(currentPrivate)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.json")
	keyPath := filepath.Join(dir, "current-private.pem")
	signaturePath := filepath.Join(dir, "index.json.sig")
	if err := os.WriteFile(indexPath, []byte(`{"plugins":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, append([]byte("unexpected prefix\n"), keyPEM...), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--key-id", keys[0].ID, "--key-file", keyPath, "--index", indexPath, "--signature", signaturePath}
	var stdout, stderr bytes.Buffer
	if err := runWithKeys(args, keys, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "exactly one PEM block") {
		t.Fatalf("unexpected PEM prefix was accepted or returned the wrong error: %v", err)
	}
}
