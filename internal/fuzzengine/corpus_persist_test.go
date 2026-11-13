package fuzzengine

import "testing"

func TestCorpusPersistNamespacePackRequiresOwner(t *testing.T) {
	ns := CorpusPersistNamespace(map[string]any{"guard_pack": "filter_utf8"})
	if ns != "" {
		t.Fatalf("pack without owner must refuse shared ns, got %q", ns)
	}
	ns = CorpusPersistNamespace(map[string]any{
		"guard_pack": "filter_utf8",
		"owner_ref":  "customer:acme",
	})
	if ns != "owner:customer:acme:pack:filter_utf8" {
		t.Fatalf("ns=%q", ns)
	}
}

func TestCorpusPersistNamespaceOverrideTenantScoped(t *testing.T) {
	ns := CorpusPersistNamespace(map[string]any{
		"guard_pack":         "secrets",
		"corpus_persist_key": "acme:prod",
		"owner_ref":          "customer:acme",
	})
	if ns != "owner:customer:acme:key:acme:prod" {
		t.Fatalf("override ns=%q", ns)
	}
	if CorpusPersistNamespace(map[string]any{"corpus_persist_key": "global"}) != "" {
		t.Fatal("bare persist key without owner must be empty")
	}
}

func TestCorpusNamespaceOwnedBy(t *testing.T) {
	if !CorpusNamespaceOwnedBy("owner:customer:a:pack:secrets", "customer:a") {
		t.Fatal("owned")
	}
	if CorpusNamespaceOwnedBy("owner:customer:a:pack:secrets", "customer:b") {
		t.Fatal("cross-tenant")
	}
	if CorpusNamespaceOwnedBy("pack:secrets", "customer:a") {
		t.Fatal("legacy pack ns must not match")
	}
}

func TestCorpusPersistEnabledDefaultGuided(t *testing.T) {
	if CorpusPersistEnabled(map[string]any{"guard_pack": "secrets", "guided_scheduling": true}) {
		t.Fatal("pack without owner must not enable persist")
	}
	if !CorpusPersistEnabled(map[string]any{
		"guard_pack":        "secrets",
		"owner_ref":         "customer:x",
		"guided_scheduling": true,
		"corpus_persist":    true,
	}) {
		t.Fatal("tenant-scoped guided persist should enable")
	}
}

func TestCorpusPersistMaxBounds(t *testing.T) {
	max := CorpusPersistMax(map[string]any{
		"pool_corpus_max":    512,
		"corpus_persist_max": 200,
	})
	if max != 200 {
		t.Fatalf("max=%d want explicit 200", max)
	}
	def := CorpusPersistMax(map[string]any{"pool_corpus_max": 512})
	if def != 128 {
		t.Fatalf("default max=%d want 128 cap", def)
	}
}
