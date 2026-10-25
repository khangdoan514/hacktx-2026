package toolpacks

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Credential is what a connected pack holds. It lives only in the store file
// (mode 0600, gitignored) and is never returned by the API or a tool result.
type Credential struct {
	Kind         string    `json:"kind"` // "token" or "oauth"
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Account      string    `json:"account,omitempty"`
	ConnectedAt  time.Time `json:"connected_at"`
	PublicClient bool      `json:"public_client,omitempty"`
}

// Store is a small JSON file shared by the API process and the MCP server,
// so a connection made in the console is visible to agents immediately.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore keeps credentials in dir/credentials.json.
func NewStore(dir string) *Store { return &Store{path: filepath.Join(dir, "credentials.json")} }

func (s *Store) Directory() string { return filepath.Dir(s.path) }

// ForOwner never falls back to the legacy shared credentials.
func (s *Store) ForOwner(owner string) *Store {
	sum := sha256.Sum256([]byte(owner))
	return NewStore(filepath.Join(filepath.Dir(s.path), "users", fmt.Sprintf("%x", sum)))
}

func (s *Store) load() (map[string]Credential, error) {
	items := map[string]Credential{}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) save(items map[string]Credential) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Get(pack string) (Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.load()
	if err != nil {
		return Credential{}, false
	}
	c, ok := items[pack]
	return c, ok && c.AccessToken != ""
}

func (s *Store) Put(pack string, c Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockFile()
	if err != nil {
		return err
	}
	defer unlock()
	items, err := s.load()
	if err != nil {
		return err
	}
	if c.ConnectedAt.IsZero() {
		c.ConnectedAt = time.Now().UTC()
	}
	items[pack] = c
	return s.save(items)
}

func (s *Store) Delete(pack string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockFile()
	if err != nil {
		return err
	}
	defer unlock()
	items, err := s.load()
	if err != nil {
		return err
	}
	delete(items, pack)
	return s.save(items)
}

// API and MCP processes may refresh or disconnect the same account concurrently.
func (s *Store) lockFile() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
