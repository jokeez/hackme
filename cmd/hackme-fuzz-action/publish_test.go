package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"hackme/internal/fuzzingcli"
)

const (
	testCIToken     = "ci-0123456789abcdef0123456789abcdef"
	testReportToken = "report-tok-secret-999"
)

func TestCheckPublishAllowed(t *testing.T) {
	same := func(g PublishGuard) PublishGuard { g.HeadRepo, g.BaseRepo = "acme/lib", "acme/lib"; return g }
	ok := []PublishGuard{
		{Event: "workflow_dispatch"},
		{Event: "push", RefProtected: "true"},
		same(PublishGuard{Event: "pull_request", PublishLabel: "hackme-dig", PRLabels: `["bug","hackme-dig"]`}),
	}
	for _, g := range ok {
		if err := checkPublishAllowed(g); err != nil {
			t.Errorf("%+v: %v", g, err)
		}
	}
	bad := map[string]PublishGuard{
		"push unprotected":    {Event: "push", RefProtected: "false"},
		"pr without label":    same(PublishGuard{Event: "pull_request", PublishLabel: "hackme-dig", PRLabels: `["bug"]`}),
		"pr label not set up": same(PublishGuard{Event: "pull_request", PRLabels: `["hackme-dig"]`}),
		"pr from fork": {Event: "pull_request", PublishLabel: "hackme-dig", PRLabels: `["hackme-dig"]`,
			HeadRepo: "evil/lib", BaseRepo: "acme/lib"},
		"pr labels garbage":   same(PublishGuard{Event: "pull_request", PublishLabel: "x", PRLabels: `nope`}),
		"label substring":     same(PublishGuard{Event: "pull_request", PublishLabel: "dig", PRLabels: `["hackme-dig"]`}),
		"pull_request_target": {Event: "pull_request_target"},
		"schedule":            {Event: "schedule"},
		"no event":            {},
	}
	for name, g := range bad {
		if err := checkPublishAllowed(g); !errors.Is(err, errUsage) {
			t.Errorf("%s: want refusal, got %v", name, err)
		}
	}
}

func TestCILabel(t *testing.T) {
	cases := map[string]string{
		"acme/lib#12":            "acme/lib#12",
		"acme/lib@main":          "acme/lib@main",
		"--x":                    "x",
		"a b%c\n":                "a-b-c-",
		"..":                     "ci",
		"a..b":                   "a-b",
		"":                       "ci",
		strings.Repeat("a", 300): strings.Repeat("a", maxLabelLen),
	}
	for in, want := range cases {
		if got := ciLabel(in); got != want {
			t.Errorf("ciLabel(%q)=%q want %q", in, got, want)
		}
	}
}

type fakeOrders struct {
	digCalls  atomic.Int32
	lastBody  atomic.Value
	digStatus int
}

func (f *fakeOrders) server(t *testing.T) *httptest.Server {
	hub := &fakeHub{pulses: []string{"running", "completed"}, gatePass: true, budgetRuns: 48}
	inner := hub.handler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ci/dig", func(w http.ResponseWriter, r *http.Request) {
		f.digCalls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("X-Hackme-CI-Token") != testCIToken {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.lastBody.Store(string(b))
		if f.digStatus != 0 {
			http.Error(w, `{"error":"ci_cap_exceeded"}`, f.digStatus)
			return
		}
		w.Write([]byte(`{"ok":true,"campaign_id":"camp-1","customer_report_token":"` + testReportToken + `"}`))
	})
	// Gate/pulse use the per-campaign report token from the create response.
	mux.HandleFunc("/api/fuzz/campaigns/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Report-Token") == testReportToken {
			r.Header.Set("X-Hackme-Report-Token", testToken)
		}
		inner.ServeHTTP(w, r)
	})
	return httptest.NewServer(mux)
}

func publishArgs(srvURL, ws, dir string, extra ...string) []string {
	return append([]string{"-mode", "publish", "-hackme-url", srvURL, "-workspace", ws,
		"-hackme-root", fuzzingcli.DiscoverRepoRoot(), "-gate-poll", "1ms", "-gate-timeout", "5s",
		"-label", "acme/lib@main", "-markdown-out", filepath.Join(dir, "c.md"),
		"-github-output", filepath.Join(dir, "out")}, extra...)
}

func TestPublishEndToEnd(t *testing.T) {
	f := &fakeOrders{}
	srv := f.server(t)
	defer srv.Close()
	ws, dir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")
	t.Setenv(ciTokenEnv, testCIToken)

	var out, errb strings.Builder
	code := run(publishArgs(srv.URL, ws, dir, "-wasm", "g.wasm", "-guard-pack", "overflow_smoke"), &out, &errb)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb.String())
	}
	var body ciDigBody
	_ = json.Unmarshal([]byte(f.lastBody.Load().(string)), &body)
	if body.WasmCheckHex != "0061736d01000000"+strings.TrimPrefix(body.WasmCheckHex, "0061736d01000000") ||
		len(body.WasmCheckHex) != 2*len(minimalCheckWasm) || body.GuardPack != "overflow_smoke" ||
		body.DigPackage != "scan" || body.Label != "acme/lib@main" || body.DigPreset != "" {
		t.Fatalf("body sent: %+v", body)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "c.md"))
	outs, _ := os.ReadFile(filepath.Join(dir, "out"))
	if !strings.Contains(string(outs), "campaign-id=camp-1\n") || !strings.Contains(string(outs), "gate-pass=true\n") {
		t.Fatalf("outputs:\n%s", outs)
	}
	if !strings.HasPrefix(string(md), gateMarker) {
		t.Fatalf("publish should leave the gate verdict as markdown:\n%s", md)
	}
	all := out.String() + errb.String() + string(md) + string(outs)
	for _, secret := range []string{testReportToken, testCIToken} {
		if strings.Contains(all, secret) {
			t.Fatalf("secret %q leaked into stdout/stderr/markdown/outputs", secret)
		}
	}
}

func TestPublishPresetSendsPresetOnly(t *testing.T) {
	requireWasmTarget(t, "rustc")
	f := &fakeOrders{}
	srv := f.server(t)
	defer srv.Close()
	t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")
	t.Setenv(ciTokenEnv, testCIToken)
	var out, errb strings.Builder
	if code := run(publishArgs(srv.URL, t.TempDir(), t.TempDir(), "-dig-preset", "scan_smoke"), &out, &errb); code != 0 {
		t.Fatalf("exit=%d %s", code, errb.String())
	}
	var body ciDigBody
	_ = json.Unmarshal([]byte(f.lastBody.Load().(string)), &body)
	if body.DigPreset != "scan_smoke" || body.WasmCheckHex != "" {
		t.Fatalf("preset publish must let the node resolve its shipped guard: %+v", body)
	}
}

func TestPublishRefusalsSendNothing(t *testing.T) {
	f := &fakeOrders{}
	srv := f.server(t)
	defer srv.Close()
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	writeFile(t, filepath.Join(ws, "bad.wasm"), []byte("\x00asm\x01\x00\x00\x00garbage"))

	type tc struct {
		event, token string
		args         []string
		want         int
	}
	cases := map[string]tc{
		"plain PR event":      {"pull_request", testCIToken, []string{"-wasm", "g.wasm"}, 2},
		"no ci token":         {"workflow_dispatch", "", []string{"-wasm", "g.wasm"}, 2},
		"http remote url":     {"workflow_dispatch", testCIToken, []string{"-wasm", "g.wasm", "-hackme-url", "http://example.com"}, 2},
		"gate wasm rejected":  {"workflow_dispatch", testCIToken, []string{"-wasm", "bad.wasm"}, 1},
		"bad gate timing":     {"workflow_dispatch", testCIToken, []string{"-wasm", "g.wasm", "-gate-timeout", "9h"}, 2},
		"pull_request_target": {"pull_request_target", testCIToken, []string{"-wasm", "g.wasm", "-publish-label", "x"}, 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GITHUB_EVENT_NAME", c.event)
			t.Setenv(ciTokenEnv, c.token)
			before := f.digCalls.Load()
			var out, errb strings.Builder
			if code := run(publishArgs(srv.URL, ws, t.TempDir(), c.args...), &out, &errb); code != c.want {
				t.Fatalf("exit=%d want %d: %s", code, c.want, errb.String())
			}
			if f.digCalls.Load() != before {
				t.Fatal("an order was sent although publish should have been refused")
			}
		})
	}
}

func TestPublishServerErrorStopsBeforeGate(t *testing.T) {
	f := &fakeOrders{digStatus: http.StatusTooManyRequests}
	srv := f.server(t)
	defer srv.Close()
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")
	t.Setenv(ciTokenEnv, testCIToken)
	var out, errb strings.Builder
	if code := run(publishArgs(srv.URL, ws, t.TempDir(), "-wasm", "g.wasm"), &out, &errb); code != 1 {
		t.Fatalf("exit=%d", code)
	}
	if f.digCalls.Load() != 1 || !strings.Contains(errb.String(), "429") {
		t.Fatalf("one attempt, no retry; stderr=%s calls=%d", errb.String(), f.digCalls.Load())
	}
}
