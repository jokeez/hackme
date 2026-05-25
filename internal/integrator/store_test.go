package integrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterValidateRotate(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, tok, err := s.Register("acme", "127.0.0.1", 10)
	if err != nil || id == "" || tok == "" {
		t.Fatalf("register: id=%q err=%v", id, err)
	}
	if !s.Validate(tok) {
		t.Fatal("validate new token")
	}
	id2, tok2, err := s.Rotate(tok)
	if err != nil || id2 != id || tok2 == "" {
		t.Fatalf("rotate: %v", err)
	}
	if s.Validate(tok) {
		t.Fatal("old token must be invalid after rotate")
	}
	if !s.Validate(tok2) {
		t.Fatal("new token must work")
	}
	if _, err := os.ReadFile(filepath.Join(dir, "integrator_tokens.json")); err != nil {
		t.Fatal(err)
	}
}
