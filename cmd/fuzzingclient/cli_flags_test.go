package main

import "testing"

func TestReconcileCLIBaseAfterSubcommand(t *testing.T) {
	base, token, save, cmd, rest := reconcileCLI("http://127.0.0.1:8080", "", false, []string{
		"tasks", "--base", "http://127.0.0.1:18099",
	})
	if cmd != "tasks" {
		t.Fatalf("cmd=%q", cmd)
	}
	if base != "http://127.0.0.1:18099" {
		t.Fatalf("base=%q", base)
	}
	if len(rest) != 0 {
		t.Fatalf("rest=%v", rest)
	}
	if token != "" || save {
		t.Fatalf("token=%q save=%v", token, save)
	}
}

func TestReconcileCLIBaseBeforeSubcommand(t *testing.T) {
	base, _, save, cmd, _ := reconcileCLI("http://127.0.0.1:8080", "", false, []string{
		"register", "--save",
	})
	if cmd != "register" || !save {
		t.Fatalf("cmd=%q save=%v", cmd, save)
	}
	if base != "http://127.0.0.1:8080" {
		t.Fatalf("base=%q", base)
	}
}

func TestReconcileCLIBaseEqualsForm(t *testing.T) {
	base, _, _, cmd, _ := reconcileCLI("http://127.0.0.1:8080", "", false, []string{
		"wallet", "--base=http://127.0.0.1:19999",
	})
	if cmd != "wallet" || base != "http://127.0.0.1:19999" {
		t.Fatalf("cmd=%q base=%q", cmd, base)
	}
}
