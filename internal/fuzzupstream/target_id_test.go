package fuzzupstream

import "testing"

func TestSanitizeCatalogID(t *testing.T) {
	ok, _ := SanitizeCatalogID("mpack")
	if ok != "mpack" {
		t.Fatalf("got %q", ok)
	}
	if _, good := SanitizeCatalogID("json-c"); !good {
		t.Fatal("json-c should be allowed")
	}
	for _, bad := range []string{"", ".", "..", "../etc", "a/b", "a\\b", "with space", string([]byte{0})} {
		if _, good := SanitizeCatalogID(bad); good {
			t.Fatalf("expected reject %q", bad)
		}
	}
}
