package fuzzupstream

import (
	"testing"
	"time"
)

func TestBuildTimeoutGivesSlowCatalogTargetsRoom(t *testing.T) {
	for _, id := range []string{"pcre2", "quickjs", "duktape", "libxml2", "nghttp2"} {
		if got := buildTimeout(Target{ID: id}); got != 600*time.Second {
			t.Fatalf("%s build wall %s, want 10m", id, got)
		}
	}
	if got := buildTimeout(Target{ID: "jsmn"}); got != 120*time.Second {
		t.Fatalf("jsmn build wall %s, want 2m", got)
	}
}
