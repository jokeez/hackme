package fuzzengine

import "testing"

func TestCorpusPersistNamespacePack(t *testing.T) {
	ns := CorpusPersistNamespace(map[string]any{"guard_pack": "filter_utf8"})
	if ns != "pack:filter_utf8" {
		t.Fatalf("ns=%q", ns)
	}
}

func TestCorpusPersistNamespaceOverride(t *testing.T) {
	ns := CorpusPersistNamespace(map[string]any{
		"guard_pack":          "secrets",
		"corpus_persist_key":  "acme:prod",
	})
	if ns != "acme:prod" {
		t.Fatalf("override ns=%q", ns)
	}
}

func TestCorpusPersistEnabledDefaultGuided(t *testing.T) {
	if !CorpusPersistEnabled(map[string]any{"guard_pack": "secrets", "guided_scheduling": true}) {
		t.Fatal("guided should enable persist")
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
