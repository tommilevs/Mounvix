package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	bootstrapAdminName         = "admin"
	legacyDefaultAdminPassword = "1234567890"
	adminBootstrapPasswordFile = "admin-bootstrap-password"
)

// User — учётная запись. ACL: имя корня -> "r" | "rw".
type User struct {
	Name     string            `json:"name"`
	PassHash string            `json:"pass_hash"`
	Role     string            `json:"role"`
	Disabled bool              `json:"disabled"`
	ACL      map[string]string `json:"acl,omitempty"`
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

type AuditEntry struct {
	At   time.Time `json:"at"`
	User string    `json:"user"`
	Act  string    `json:"act"`
	Info string    `json:"info"`
}

type session struct {
	User string
	Exp  time.Time
}

// Store — JSON-хранилище пользователей/сессий и аудит-лог.
type Store struct {
	mu       sync.Mutex
	Dir      string
	users    []*User
	sessions map[string]session
	audit    []AuditEntry
	auditF   *os.File
}

func NewStore(dir string) (*Store, error) {
	s := &Store{Dir: dir, sessions: map[string]session{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create user store directory: %w", err)
	}
	s.auditF, _ = os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	usersPath := filepath.Join(dir, "users.json")
	if _, err := os.Stat(usersPath); err == nil {
		if err := os.Chmod(usersPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure users.json permissions: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect users.json permissions: %w", err)
	}

	if len(s.users) == 0 {
		if err := s.seedInitialAdmin(); err != nil {
			return nil, err
		}
		return s, nil
	}

	admin := s.findAdmin()
	if admin != nil && isLegacyDefaultAdmin(admin) {
		if err := s.rotateLegacyDefaultAdmin(admin); err != nil {
			return nil, err
		}
	} else if err := s.removeStaleBootstrapPassword(admin); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(filepath.Join(s.Dir, "users.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.users)
}

func (s *Store) seedInitialAdmin() error {
	password, err := s.ensureBootstrapPassword()
	if err != nil {
		return fmt.Errorf("create initial admin password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash initial admin password: %w", err)
	}
	s.users = []*User{{Name: bootstrapAdminName, PassHash: string(hash), Role: "admin"}}
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("save initial admin user: %w", err)
	}
	s.logBootstrapPasswordPath()
	return nil
}

func (s *Store) findAdmin() *User {
	for _, user := range s.users {
		if user.Name == bootstrapAdminName && user.IsAdmin() {
			return user
		}
	}
	return nil
}

func isLegacyDefaultAdmin(user *User) bool {
	return user != nil && user.IsAdmin() && user.Name == bootstrapAdminName &&
		bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(legacyDefaultAdminPassword)) == nil
}

func (s *Store) rotateLegacyDefaultAdmin(admin *User) error {
	password, err := s.ensureBootstrapPassword()
	if err != nil {
		return fmt.Errorf("rotate legacy default admin password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash replacement admin password: %w", err)
	}
	admin.PassHash = string(hash)
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("save rotated admin password: %w", err)
	}
	s.logBootstrapPasswordPath()
	return nil
}

func (s *Store) ensureBootstrapPassword() (string, error) {
	path := filepath.Join(s.Dir, adminBootstrapPasswordFile)
	data, err := os.ReadFile(path)
	if err == nil {
		password := strings.TrimSpace(string(data))
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(password)
		if decodeErr != nil || len(decoded) != 32 {
			return "", errors.New("admin bootstrap password file is invalid")
		}
		return password, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read admin bootstrap password: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate admin bootstrap password: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	file, err := os.CreateTemp(s.Dir, ".admin-bootstrap-*")
	if err != nil {
		return "", fmt.Errorf("create temporary admin bootstrap file: %w", err)
	}
	tempPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); closeErr != nil {
				log.Printf("mounvix: close temporary admin bootstrap file %s: %v", tempPath, closeErr)
			}
		}
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Printf("mounvix: remove temporary admin bootstrap file %s: %v", tempPath, removeErr)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure temporary admin bootstrap file: %w", err)
	}
	if _, err := file.WriteString(password + "\n"); err != nil {
		return "", fmt.Errorf("write temporary admin bootstrap file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary admin bootstrap file: %w", err)
	}
	if err := file.Close(); err != nil {
		closed = true
		return "", fmt.Errorf("close temporary admin bootstrap file: %w", err)
	}
	closed = true
	if err := os.Rename(tempPath, path); err != nil {
		return "", fmt.Errorf("install admin bootstrap password file: %w", err)
	}
	return password, nil
}

func (s *Store) removeStaleBootstrapPassword(admin *User) error {
	path := filepath.Join(s.Dir, adminBootstrapPasswordFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read admin bootstrap password file: %w", err)
	}
	password := strings.TrimSpace(string(data))
	if admin != nil && password != "" && bcrypt.CompareHashAndPassword([]byte(admin.PassHash), []byte(password)) == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale admin bootstrap password file: %w", err)
	}
	return nil
}

func (s *Store) logBootstrapPasswordPath() {
	log.Printf("mounvix: initial admin password is stored in %s; read it locally and change it in the panel", filepath.Join(s.Dir, adminBootstrapPasswordFile))
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(s.Dir, "users.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write temporary users.json: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("secure temporary users.json: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("replace users.json: %w", err)
	}
	return nil
}

func (s *Store) Users() []*User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*User, len(s.users))
	copy(out, s.users)
	return out
}

func (s *Store) Find(name string) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Name == name {
			return u
		}
	}
	return nil
}

func (s *Store) Verify(name, pass string) (*User, bool) {
	u := s.Find(name)
	if u == nil || u.Disabled {
		return nil, false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte(pass)) != nil {
		return nil, false
	}
	return u, true
}

func (s *Store) AddUser(name, pass, role string) error {
	if name == "" || len(pass) < 4 {
		return errors.New("bad name or password")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Name == name {
			return errors.New("user exists")
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if role != "admin" {
		role = "user"
	}
	s.users = append(s.users, &User{Name: name, PassHash: string(hash), Role: role, ACL: map[string]string{}})
	return s.saveLocked()
}

func (s *Store) SetPassword(name, pass string) error {
	if len(pass) < 4 {
		return errors.New("password too short")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Name == name {
			hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			u.PassHash = string(hash)
			if err := s.saveLocked(); err != nil {
				return err
			}
			if name == bootstrapAdminName {
				path := filepath.Join(s.Dir, adminBootstrapPasswordFile)
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("remove initial admin password file: %w", err)
				}
			}
			return nil
		}
	}
	return errors.New("no such user")
}

func (s *Store) ToggleUser(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Name == name {
			u.Disabled = !u.Disabled
			_ = s.saveLocked()
			return
		}
	}
}

func (s *Store) SetACL(name string, acl map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Name == name {
			u.ACL = acl
			_ = s.saveLocked()
			return
		}
	}
}

// AddUserHash добавляет пользователя с уже готовым bcrypt-хешем (из заявки).
func (s *Store) AddUserHash(name, hash, role string) error {
	if name == "" || hash == "" {
		return errors.New("bad name or hash")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Name == name {
			return errors.New("user exists")
		}
	}
	if role != "admin" {
		role = "user"
	}
	s.users = append(s.users, &User{Name: name, PassHash: hash, Role: role, ACL: map[string]string{}})
	return s.saveLocked()
}

func (s *Store) CreateSession(name string) string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[tok] = session{User: name, Exp: time.Now().Add(12 * time.Hour)}
	s.mu.Unlock()
	return tok
}

func (s *Store) SessionUser(tok string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[tok]
	if !ok || time.Now().After(sess.Exp) {
		delete(s.sessions, tok)
		return "", false
	}
	return sess.User, true
}

func (s *Store) DropSession(tok string) {
	s.mu.Lock()
	delete(s.sessions, tok)
	s.mu.Unlock()
}

func (s *Store) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	now := time.Now()
	for _, v := range s.sessions {
		if now.Before(v.Exp) {
			n++
		}
	}
	return n
}

func (s *Store) Audit(who, act, info string) {
	e := AuditEntry{At: time.Now(), User: who, Act: act, Info: info}
	s.mu.Lock()
	s.audit = append(s.audit, e)
	if len(s.audit) > 500 {
		s.audit = s.audit[len(s.audit)-500:]
	}
	if s.auditF != nil {
		if b, err := json.Marshal(e); err == nil {
			_, _ = s.auditF.Write(append(b, '\n'))
		}
	}
	s.mu.Unlock()
}

func (s *Store) AuditTail(n int) []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.audit) {
		n = len(s.audit)
	}
	out := make([]AuditEntry, n)
	copy(out, s.audit[len(s.audit)-n:])
	return out
}

func (s *Store) AuditCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.audit)
}
