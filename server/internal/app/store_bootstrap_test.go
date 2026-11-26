package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestNewStoreCreatesRandomAdminBootstrapPasswordAndRemovesItAfterReset(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	bootstrapPath := filepath.Join(dir, "admin-bootstrap-password")
	initialPassword := readBootstrapPassword(t, bootstrapPath)
	if initialPassword == "1234567890" || len(initialPassword) < 40 {
		t.Fatalf("initial password is not a unique high-entropy credential: %q", initialPassword)
	}
	if _, ok := store.Verify("admin", initialPassword); !ok {
		t.Fatal("bootstrap password does not authenticate the seeded admin")
	}
	if _, ok := store.Verify("admin", "1234567890"); ok {
		t.Fatal("legacy default password still authenticates the seeded admin")
	}

	info, err := os.Stat(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("bootstrap password permissions=%#o, want 0600", info.Mode().Perm())
	}

	restarted, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := readBootstrapPassword(t, bootstrapPath); got != initialPassword {
		t.Fatal("restart changed the existing bootstrap password")
	}
	if _, ok := restarted.Verify("admin", initialPassword); !ok {
		t.Fatal("restart invalidated the bootstrap password")
	}

	const replacementPassword = "test-replacement-password-2026"
	if err := restarted.SetPassword("admin", replacementPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bootstrapPath); !os.IsNotExist(err) {
		t.Fatalf("bootstrap password file remains after admin reset: stat err=%v", err)
	}
	if _, ok := restarted.Verify("admin", initialPassword); ok {
		t.Fatal("old bootstrap password still authenticates after reset")
	}
	if _, ok := restarted.Verify("admin", replacementPassword); !ok {
		t.Fatal("replacement admin password does not authenticate")
	}
}

func TestNewStoreRotatesLegacyDefaultWithoutChangingOtherUsers(t *testing.T) {
	dir := t.TempDir()
	adminHash, err := bcrypt.GenerateFromPassword([]byte("1234567890"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	readerHash, err := bcrypt.GenerateFromPassword([]byte("reader-secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	writeStoreUsersFixture(t, dir, []*User{
		{Name: "admin", PassHash: string(adminHash), Role: "admin"},
		{Name: "reader", PassHash: string(readerHash), Role: "user", ACL: map[string]string{"share": "r"}},
	})

	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Verify("admin", "1234567890"); ok {
		t.Fatal("legacy default password remains valid after upgrade")
	}
	if _, ok := store.Verify("reader", "reader-secret"); !ok {
		t.Fatal("existing non-admin credentials were changed")
	}
	reader := store.Find("reader")
	if reader == nil || reader.Role != "user" || reader.ACL["share"] != "r" {
		t.Fatalf("existing reader account or ACL changed: %#v", reader)
	}
	if _, ok := store.Verify("admin", readBootstrapPassword(t, filepath.Join(dir, "admin-bootstrap-password"))); !ok {
		t.Fatal("rotated admin bootstrap password does not authenticate")
	}

	usersInfo, err := os.Stat(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if usersInfo.Mode().Perm() != 0o600 {
		t.Fatalf("users.json permissions=%#o, want 0600", usersInfo.Mode().Perm())
	}
}

func TestNewStorePreservesCustomAdminPasswordWithoutBootstrapFile(t *testing.T) {
	dir := t.TempDir()
	const customPassword = "custom-admin-password"
	adminHash, err := bcrypt.GenerateFromPassword([]byte(customPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	writeStoreUsersFixture(t, dir, []*User{{Name: "admin", PassHash: string(adminHash), Role: "admin"}})

	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Verify("admin", customPassword); !ok {
		t.Fatal("existing custom admin password was changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "admin-bootstrap-password")); !os.IsNotExist(err) {
		t.Fatalf("custom-password installation unexpectedly has a bootstrap file: %v", err)
	}
}

func TestNewStoreFailsClosedWhenBootstrapPathIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "admin-bootstrap-password"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir); err == nil {
		t.Fatal("store started despite an unreadable bootstrap-password path")
	}
}

func writeStoreUsersFixture(t *testing.T, dir string, users []*User) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readBootstrapPassword(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bootstrap password: %v", err)
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		t.Fatal("bootstrap password file is empty")
	}
	return password
}
