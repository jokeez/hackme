package main

import (
	"testing"
)

func TestAllowedFailClosedEmpty(t *testing.T) {
	b := &bot{cfg: config{allowedUsers: map[int64]struct{}{}}}
	if b.allowed(1) {
		t.Fatal("empty allowlist must deny")
	}
	b.cfg.allowedUsers[42] = struct{}{}
	if !b.allowed(42) {
		t.Fatal("listed user must be allowed")
	}
	if b.allowed(99) {
		t.Fatal("unlisted user must be denied")
	}
}

func TestLoadConfigEmptyAllowlist(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("HACKME_TELEGRAM_ALLOWED_USER_IDS", "")
	t.Setenv("HACKME_TELEGRAM_NODE_URL", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.allowedUsers) != 0 {
		t.Fatalf("want empty map, got %#v", cfg.allowedUsers)
	}
	b := newBot(cfg)
	if b.allowed(1) {
		t.Fatal("empty allowlist must deny all")
	}
}
