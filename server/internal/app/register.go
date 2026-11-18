package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/version"
	"golang.org/x/crypto/bcrypt"
)

// PendingReg — заявка на регистрацию (до подтверждения админом).
type PendingReg struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PassHash  string    `json:"pass_hash"`
	CreatedAt time.Time `json:"created_at"`
	Note      string    `json:"note,omitempty"`
}

// RegStore — хранилище заявок (data/registrations.json).
type RegStore struct {
	mu    sync.Mutex
	path  string
	items []*PendingReg
}

func NewRegStore(dir string) (*RegStore, error) {
	r := &RegStore{path: filepath.Join(dir, "registrations.json")}
	if b, err := os.ReadFile(r.path); err == nil {
		if err := json.Unmarshal(b, &r.items); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return r, nil
}

func (r *RegStore) List() []*PendingReg {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*PendingReg, len(r.items))
	copy(out, r.items)
	return out
}

func (r *RegStore) Add(name, pass, note string) (*PendingReg, error) {
	name = strings.TrimSpace(name)
	if len(name) < 3 || len(name) > 32 {
		return nil, errors.New("логин: 3–32 символа")
	}
	if len(pass) < 8 {
		return nil, errors.New("пароль не короче 8 символов")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range r.items {
		if it.Name == name {
			return nil, errors.New("заявка с этим логином уже есть")
		}
	}
	p := &PendingReg{ID: hex.EncodeToString(raw), Name: name, PassHash: string(hash), CreatedAt: time.Now(), Note: note}
	r.items = append(r.items, p)
	if err := r.saveLocked(); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *RegStore) Approve(id string) (*PendingReg, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, it := range r.items {
		if it.ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			if err := r.saveLocked(); err != nil {
				return nil, err
			}
			return it, nil
		}
	}
	return nil, errors.New("заявка не найдена")
}

func (r *RegStore) Reject(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, it := range r.items {
		if it.ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			_ = r.saveLocked()
			return true
		}
	}
	return false
}

func (r *RegStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.items, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

type registerView struct {
	Title   string
	Lang    string
	Version string
	User    *User
	Msg     string
	Err     string
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	v := &registerView{Title: "Регистрация", Lang: a.configSnapshot().Lang, Version: versionOf()}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		name := strings.TrimSpace(r.PostFormValue("username"))
		pass := r.PostFormValue("password")
		if a.store.Find(name) != nil {
			v.Err = "Пользователь с таким логином уже существует"
		} else if _, err := a.regs.Add(name, pass, r.RemoteAddr); err != nil {
			v.Err = err.Error()
		} else {
			v.Msg = "Заявка отправлена. После подтверждения администратором вы сможете войти."
			a.store.Audit(name, "register_request", r.RemoteAddr)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "register.html", v)
}

type regsView struct {
	Title   string
	Lang    string
	Version string
	User    *User
	Pending []*PendingReg
	Msg     string
}

func (a *App) handleRegistrations(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	v := &regsView{Title: "Заявки", Lang: a.configSnapshot().Lang, Version: versionOf(), User: u}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		id := r.PostFormValue("id")
		switch r.PostFormValue("action") {
		case "approve":
			p, err := a.regs.Approve(id)
			if err != nil {
				v.Msg = "Ошибка: " + err.Error()
			} else if a.store.Find(p.Name) != nil {
				v.Msg = "Пользователь уже существует"
			} else if err := a.store.AddUserHash(p.Name, p.PassHash, "user"); err != nil {
				v.Msg = "Ошибка: " + err.Error()
			} else {
				v.Msg = "Пользователь " + p.Name + " принят"
				a.store.Audit(u.Name, "user_approved", p.Name)
			}
		case "reject":
			if a.regs.Reject(id) {
				v.Msg = "Заявка отклонена"
				a.store.Audit(u.Name, "user_rejected", id)
			}
		}
	}
	v.Pending = a.regs.List()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "registrations.html", v)
}

// versionOf — версия сервера для шаблонов.
func versionOf() string { return version.Version }
