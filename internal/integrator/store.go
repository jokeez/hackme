// Package integrator manages scoped B2B API tokens (tasks read/create only).
package integrator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is one integrator credential (token stored as SHA-256 hex only).
type Record struct {
	ID          string `json:"id"`
	Label       string `json:"label,omitempty"`
	TokenSHA256 string `json:"token_sha256"`
	CreatedAt   int64  `json:"created_at"`
	RotatedAt   int64  `json:"rotated_at,omitempty"`
	Revoked     bool   `json:"revoked,omitempty"`
	ClientIP    string `json:"client_ip,omitempty"`
}

type fileData struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// Store persists integrator tokens under the node data directory.
type Store struct {
	path string
	mu   sync.RWMutex
	data fileData
}

// Open loads or creates integrator_tokens.json in dataDir.
func Open(dataDir string) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("integrator: dataDir required")
	}
	path := filepath.Join(dataDir, "integrator_tokens.json")
	s := &Store{path: path, data: fileData{Version: 1}}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var d fileData
	if err := json.Unmarshal(b, &d); err != nil {
		return fmt.Errorf("integrator: decode: %w", err)
	}
	if d.Version == 0 {
		d.Version = 1
	}
	s.data = d
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "hmdev_" + hex.EncodeToString(b), nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "int-" + hex.EncodeToString(b)
}

// ActiveCount returns non-revoked records.
func (s *Store) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, r := range s.data.Records {
		if !r.Revoked {
			n++
		}
	}
	return n
}

// Validate reports whether plainToken matches an active record or legacy env token hash is checked externally.
func (s *Store) Validate(plainToken string) bool {
	plainToken = strings.TrimSpace(plainToken)
	if plainToken == "" {
		return false
	}
	h := hashToken(plainToken)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.data.Records {
		if r.Revoked {
			continue
		}
		if r.TokenSHA256 == h {
			return true
		}
	}
	return false
}

// Register creates a new integrator and returns id + plaintext token (shown once).
func (s *Store) Register(label, clientIP string, maxActive int) (id, token string, err error) {
	if maxActive <= 0 {
		maxActive = 200
	}
	label = strings.TrimSpace(label)
	if len(label) > 120 {
		label = label[:120]
	}
	token, err = newToken()
	if err != nil {
		return "", "", err
	}
	id = newID()
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	active := 0
	for _, r := range s.data.Records {
		if !r.Revoked {
			active++
		}
	}
	if active >= maxActive {
		return "", "", errors.New("integrator: max active tokens reached")
	}
	s.data.Records = append(s.data.Records, Record{
		ID:          id,
		Label:       label,
		TokenSHA256: hashToken(token),
		CreatedAt:   now,
		ClientIP:    strings.TrimSpace(clientIP),
	})
	if err := s.saveLocked(); err != nil {
		return "", "", err
	}
	return id, token, nil
}

// Rotate invalidates the old token and issues a new one for the same record id.
func (s *Store) Rotate(plainOld string) (id, newPlain string, err error) {
	plainOld = strings.TrimSpace(plainOld)
	if plainOld == "" {
		return "", "", errors.New("integrator: token required")
	}
	h := hashToken(plainOld)
	newPlain, err = newToken()
	if err != nil {
		return "", "", err
	}
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range s.data.Records {
		if s.data.Records[i].Revoked {
			continue
		}
		if s.data.Records[i].TokenSHA256 != h {
			continue
		}
		s.data.Records[i].TokenSHA256 = hashToken(newPlain)
		s.data.Records[i].RotatedAt = now
		id = s.data.Records[i].ID
		found = true
		break
	}
	if !found {
		return "", "", errors.New("integrator: unknown or revoked token")
	}
	if err := s.saveLocked(); err != nil {
		return "", "", err
	}
	return id, newPlain, nil
}

// RevokeByID marks a record revoked (admin tooling).
func (s *Store) RevokeByID(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Records {
		if s.data.Records[i].ID == id {
			s.data.Records[i].Revoked = true
			return s.saveLocked()
		}
	}
	return errors.New("integrator: id not found")
}
