package poolsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/fuzzengine"
)

func TestUploadCorpusNamespaceRetriesTransient(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if r.Header.Get("X-Hackme-Admin-Token") != "tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if n == 1 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	err := UploadCorpusNamespace(context.Background(), srv.URL, "tok", "research:jsmn", []fuzzengine.PoolCorpusSeed{{
		Input: 1, InputBytes: []byte("unit"), Energy: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("attempts=%d", n)
	}
}

func TestUploadCorpusNamespaceDoesNotRetryAuth(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	err := UploadCorpusNamespace(context.Background(), srv.URL, "tok", "research:jsmn", []fuzzengine.PoolCorpusSeed{{
		InputBytes: []byte("unit"),
	}})
	if err == nil || n != 1 {
		t.Fatalf("err=%v attempts=%d", err, n)
	}
}
