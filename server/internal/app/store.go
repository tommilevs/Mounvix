package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
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
		return nil, err
	}
	s.auditF, _ = os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(s.users) == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("1234567890"), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		s.users = []*User{{Name: "admin", PassHash: string(hash), Role: "admin"}}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(filepath.Join(s.Dir, "users.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.users)
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(s.Dir, "users.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
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
			return s.saveLocked()
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
