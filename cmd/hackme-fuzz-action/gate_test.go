package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "tok-123"

type fakeHub struct {
	pulses     []string // statuses returned in order; last one repeats
	calls      atomic.Int32
	gateQuery  atomic.Value
	gatePass   bool
	pulseCode  int
	gateHits   atomic.Int32
	budgetRuns int
}

func (f *fakeHub) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/fuzz/campaigns/camp-1/pulse", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Report-Token") != testToken {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		if f.pulseCode != 0 {
			http.Error(w, "boom", f.pulseCode)
			return
		}
		i := int(f.calls.Add(1)) - 1
		if i >= len(f.pulses) {
			i = len(f.pulses) - 1
		}
		w.Write([]byte(`{"ok":true,"status":"` + f.pulses[i] + `","progress":{"runs_done":48,"budget_runs":` +
			strconv.Itoa(f.budgetRuns) + `}}`))
	})
	mux.HandleFunc("/api/fuzz/campaigns/camp-1/gate", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Report-Token") != testToken {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		f.gateHits.Add(1)
		f.gateQuery.Store(r.URL.RawQuery)
		pass, reason := "true", "all thresholds satisfied"
		if !f.gatePass {
			pass, reason = "false", "crash critical_count exceeds threshold"
		}
		w.Write([]byte(`{"pass":` + pass + `,"reasons":["` + reason + `"],"assurance_note":"pass ≠ proven secure",` +
			`"observed":{"critical_count":1,"high_count":0,"crash_count":1,"runs_done":48}}`))
	})
	return mux
}

func gateFor(t *testing.T, srv *httptest.Server, timeout time.Duration) (*gateClient, GateOptions) {
	t.Helper()
	o := GateOptions{BaseURL: srv.URL, CampaignID: "camp-1", Token: testToken,
		Timeout: timeout, Poll: time.Millisecond, MinRunsDone: -1}
	c, err := newGateClient(o)
	if err != nil {
		t.Fatal(err)
	}
	return c, o
}

func TestWaitGatePassAfterCompletion(t *testing.T) {
	hub := &fakeHub{pulses: []string{"planned", "running", "running", "completed"}, gatePass: true, budgetRuns: 48}
	srv := httptest.NewServer(hub.handler(t))
	defer srv.Close()
	c, o := gateFor(t, srv, 5*time.Second)
	res, err := WaitGate(context.Background(), c, o)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || res.Status != "completed" || hub.calls.Load() != 4 {
		t.Fatalf("res=%+v calls=%d", res, hub.calls.Load())
	}
	q, _ := hub.gateQuery.Load().(string)
	if !strings.Contains(q, "min_runs_done=48") || !strings.Contains(q, "max_critical=0") {
		t.Fatalf("gate query %q should demand the full budget and zero crashes", q)
	}
}

func TestWaitGateFail(t *testing.T) {
	hub := &fakeHub{pulses: []string{"completed"}, gatePass: false, budgetRuns: 48}
	srv := httptest.NewServer(hub.handler(t))
	defer srv.Close()
	c, o := gateFor(t, srv, 5*time.Second)
	res, err := WaitGate(context.Background(), c, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() || res.Critical != 1 || !strings.Contains(strings.Join(res.Reasons, ";"), "critical") {
		t.Fatalf("res=%+v", res)
	}
}

func TestWaitGateNeverPassesUnfinished(t *testing.T) {
	hub := &fakeHub{pulses: []string{"running"}, gatePass: true, budgetRuns: 48}
	srv := httptest.NewServer(hub.handler(t))
	defer srv.Close()
	c, o := gateFor(t, srv, 50*time.Millisecond)
	res, err := WaitGate(context.Background(), c, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() || res.Finished || hub.gateHits.Load() != 0 {
		t.Fatalf("running campaign must not pass or even read the gate: %+v", res)
	}
}

func TestWaitGateCancelledIsNotPass(t *testing.T) {
	hub := &fakeHub{pulses: []string{"cancelled"}, gatePass: true}
	srv := httptest.NewServer(hub.handler(t))
	defer srv.Close()
	c, o := gateFor(t, srv, time.Second)
	res, err := WaitGate(context.Background(), c, o)
	if err != nil || res.OK() || hub.gateHits.Load() != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestWaitGateRetriesTransient(t *testing.T) {
	var n atomic.Int32
	mux := http.NewServeMux()
	hub := &fakeHub{pulses: []string{"completed"}, gatePass: true}
	inner := hub.handler(t)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pulse") && n.Add(1) <= 2 {
			http.Error(w, "busy", http.StatusBadGateway)
			return
		}
		inner.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, o := gateFor(t, srv, 5*time.Second)
	res, err := WaitGate(context.Background(), c, o)
	if err != nil || !res.OK() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestWaitGateAuthErrorIsFatal(t *testing.T) {
	hub := &fakeHub{pulses: []string{"completed"}, pulseCode: http.StatusUnauthorized}
	srv := httptest.NewServer(hub.handler(t))
	defer srv.Close()
	c, o := gateFor(t, srv, 5*time.Second)
	if _, err := WaitGate(context.Background(), c, o); err == nil || errors.Is(err, errTransient) {
		t.Fatalf("401 should stop immediately, got %v", err)
	}
}

func TestGateRefusesRedirect(t *testing.T) {
	var leaked atomic.Bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Report-Token") != "" {
			leaked.Store(true)
		}
	}))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	c, o := gateFor(t, srv, time.Second)
	_, err := WaitGate(context.Background(), c, o)
	if err == nil || !strings.Contains(err.Error(), "redirect") || leaked.Load() {
		t.Fatalf("err=%v leaked=%v", err, leaked.Load())
	}
}

func TestGateOptionValidation(t *testing.T) {
	ok := GateOptions{BaseURL: "https://hackme.tech", CampaignID: "campaign-audit-1", Token: "t"}
	if _, err := newGateClient(ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*GateOptions){
		"http remote":   func(o *GateOptions) { o.BaseURL = "http://hackme.tech" },
		"ftp":           func(o *GateOptions) { o.BaseURL = "ftp://hackme.tech" },
		"relative":      func(o *GateOptions) { o.BaseURL = "hackme.tech" },
		"userinfo":      func(o *GateOptions) { o.BaseURL = "https://u:p@hackme.tech" },
		"query":         func(o *GateOptions) { o.BaseURL = "https://hackme.tech/?x=1" },
		"id traversal":  func(o *GateOptions) { o.CampaignID = "../admin" },
		"id slash":      func(o *GateOptions) { o.CampaignID = "a/b" },
		"id dotdot":     func(o *GateOptions) { o.CampaignID = "a..b" },
		"no token":      func(o *GateOptions) { o.Token = " " },
		"token newline": func(o *GateOptions) { o.Token = "a\nX-Evil: 1" },
	}
	for name, mut := range bad {
		o := ok
		mut(&o)
		if _, err := newGateClient(o); !errors.Is(err, errUsage) {
			t.Errorf("%s: want usage error, got %v", name, err)
		}
	}
	for _, u := range []string{"http://127.0.0.1:8080", "http://localhost:1", "http://[::1]:2"} {
		o := ok
		o.BaseURL = u
		if _, err := newGateClient(o); err != nil {
			t.Errorf("%s should be allowed: %v", u, err)
		}
	}
}
