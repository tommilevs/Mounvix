// Package share хранит ссылки «поделиться» (токены, срок, пароль).
package share

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Share struct {
	Token        string    `json:"token"`
	Root         string    `json:"root"`
	Path         string    `json:"path"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	PasswordHash string    `json:"password_hash,omitempty"`
	AllowUpload  bool      `json:"allow_upload"`
}

type Manager struct {
	mu    sync.Mutex
	path  string
	items map[string]*Share
	key   []byte
}

func New(dir string) (*Manager, error) {
	m := &Manager{path: filepath.Join(dir, "shares.json"), items: map[string]*Share{}}
	m.key = make([]byte, 32)
	if _, err := rand.Read(m.key); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(m.path); err == nil {
		var list []*Share
		if json.Unmarshal(b, &list) == nil {
			for _, s := range list {
				m.items[s.Token] = s
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Create(root, path, by string, ttl time.Duration, password string, allowUpload bool) (*Share, error) {
	if root == "" {
		return nil, errors.New("root required")
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	s := &Share{
		Token:       hex.EncodeToString(tok),
		Root:        root,
		Path:        filepath.ToSlash(path),
		CreatedBy:   by,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(ttl),
		AllowUpload: allowUpload,
	}
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		s.PasswordHash = string(h)
	}
	m.mu.Lock()
	m.items[s.Token] = s
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (m *Manager) Get(token string) *Share {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.items[token]
	if !ok {
		return nil
	}
	if time.Now().After(s.ExpiresAt) {
		delete(m.items, token)
		_ = m.saveLocked()
		return nil
	}
	return s
}

func (m *Manager) List() []*Share {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Share, 0, len(m.items))
	for _, s := range m.items {
		if time.Now().After(s.ExpiresAt) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Manager) Revoke(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[token]; !ok {
		return false
	}
	delete(m.items, token)
	_ = m.saveLocked()
	return true
}

func (m *Manager) CookieName(token string) string { return "mvx_share_" + token }

func (m *Manager) Unlock(token, password string) (string, bool) {
	s := m.Get(token)
	if s == nil || s.PasswordHash == "" {
		return "", false
	}
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(password)) != nil {
		return "", false
	}
	return m.cookieValue(s), true
}

func (m *Manager) CheckCookie(token, cookie string) bool {
	s := m.Get(token)
	if s == nil || cookie == "" {
		return false
	}
	return hmac.Equal([]byte(cookie), []byte(m.cookieValue(s)))
}

func (m *Manager) cookieValue(s *Share) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(s.Token + "|" + s.ExpiresAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *Manager) saveLocked() error {
	list := make([]*Share, 0, len(m.items))
	for _, s := range m.items {
		list = append(list, s)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
