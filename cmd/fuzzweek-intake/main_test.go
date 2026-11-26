package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRepoAcceptsAndCanonicalizes(t *testing.T) {
	got, known, err := normalizeRepo("  github.com/jokeez/hackme#readme ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://github.com/jokeez/hackme" {
		t.Fatalf("want scheme added and fragment dropped, got %q", got)
	}
	if !known {
		t.Fatal("github.com should be flagged as a known forge")
	}
}

func TestNormalizeRepoRejects(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"not https":   "http://github.com/a/b",
		"credentials": "https://user:pw@github.com/a/b",
		"host only":   "https://github.com",
		"no dot":      "https://localhost/a",
		"bare word":   "notaurl",
	}
	for name, in := range cases {
		if _, _, err := normalizeRepo(in); err == nil {
			t.Errorf("%s: want rejection for %q", name, in)
		}
	}
}

func TestNormalizeRepoUnknownHostStillAccepted(t *testing.T) {
	got, known, err := normalizeRepo("https://git.example.org/team/proj")
	if err != nil {
		t.Fatalf("self-hosted forges must be accepted: %v", err)
	}
	if known {
		t.Fatal("example.org is not a known forge")
	}
	if got == "" {
		t.Fatal("expected a canonical URL")
	}
}

func TestValidateContact(t *testing.T) {
	for _, ok := range []string{"a@b.co", "@handle", "https://t.me/someone"} {
		if _, err := validateContact(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ab", "nocontact", "@a"} {
		if _, err := validateContact(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestScrubStripsControlCharsAndCaps(t *testing.T) {
	got := scrub("a\x1b[31mb\n\n\nc\x00d", 0)
	if strings.ContainsAny(got, "\x1b\x00\n") {
		t.Fatalf("control characters survived: %q", got)
	}
	if got != "a[31mb cd" { // newlines collapse to one space; NUL is dropped outright
		t.Fatalf("unexpected scrub result: %q", got)
	}
	if capped := scrub(strings.Repeat("x", 50), 10); len([]rune(capped)) != 11 { // 10 + ellipsis
		t.Fatalf("cap not applied: %q", capped)
	}
}

func TestParseWindowBoundAndOpenState(t *testing.T) {
	deadline, err := parseWindowBound("2026-12-07", true)
	if err != nil {
		t.Fatalf("date should parse: %v", err)
	}
	if deadline.Hour() != 23 {
		t.Fatalf("a bare deadline date must mean end of day, got %s", deadline)
	}
	openAt, _ := parseWindowBound("2026-12-01", false)
	if openAt.Hour() != 0 {
		t.Fatalf("a bare open date must mean start of day, got %s", openAt)
	}
	if _, err := parseWindowBound("not-a-date", true); err == nil {
		t.Fatal("garbage must be rejected rather than silently ignored")
	}

	before := time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)
	during := time.Date(2026, 12, 3, 12, 0, 0, 0, time.UTC)
	after := time.Date(2026, 12, 9, 12, 0, 0, 0, time.UTC)
	if open, state := windowOpen(before, openAt, deadline); open || state != "not_open_yet" {
		t.Fatalf("before the window: open=%v state=%s", open, state)
	}
	if open, _ := windowOpen(during, openAt, deadline); !open {
		t.Fatal("inside the window intake must be open")
	}
	if open, state := windowOpen(after, openAt, deadline); open || state != "closed" {
		t.Fatalf("after the window: open=%v state=%s", open, state)
	}
}

// The promo permits three repositories per person, so the per-IP budget has to leave
// room for a correction or a shared address rather than sitting exactly on three.
func TestPerIPBudgetLeavesRoomForThreeRepos(t *testing.T) {
	if perIPPerHour <= 3 {
		t.Fatalf("perIPPerHour=%d leaves no headroom for the three repos the rules allow", perIPPerHour)
	}
	s, _ := newTestServer(t, time.Now())
	for i := 1; i <= 3; i++ {
		rec := post(s, url.Values{
			"repo": {fmt.Sprintf("https://github.com/person/repo%d", i)}, "contact": {"me@example.com"},
			"campaign_id": {fmt.Sprintf("hunt-2026112500%d", i)}, "wallet": {"HMC-73bdd7dd362e93d1"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("repo %d of the three allowed was refused: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	// A correction after the third must still get through.
	rec := post(s, url.Values{
		"repo": {"https://github.com/person/repo3-corrected"}, "contact": {"me@example.com"},
		"campaign_id": {"hunt-20261125004"}, "wallet": {"HMC-73bdd7dd362e93d1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a correction right after three submissions was refused: %d", rec.Code)
	}
}

func TestIPLimiter(t *testing.T) {
	l := newIPLimiter(2, time.Hour)
	now := time.Now()
	if !l.allow("ip", now) || !l.allow("ip", now) {
		t.Fatal("first two must be allowed")
	}
	if l.allow("ip", now) {
		t.Fatal("third within the window must be refused")
	}
	if !l.allow("other", now) {
		t.Fatal("a different address must not be affected")
	}
	if !l.allow("ip", now.Add(2*time.Hour)) {
		t.Fatal("the window must expire")
	}
}

func newTestServer(t *testing.T, now time.Time) (*server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "subs.jsonl")
	st, err := newStore(path, 3)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &server{
		store:   st,
		limiter: newIPLimiter(perIPPerHour, time.Hour),
		notify:  &notifier{client: &http.Client{}},
		now:     func() time.Time { return now },
	}, path
}

func post(s *server, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz-week/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSubmit(rec, req)
	return rec
}

func TestSubmitStoresAndDeduplicates(t *testing.T) {
	s, path := newTestServer(t, time.Now())
	form := url.Values{"repo": {"https://github.com/a/b"}, "contact": {"me@example.com"}, "note": {"fuzz the parser"},
		"campaign_id": {"hunt-20261125t065107"}, "wallet": {"HMC-73bdd7dd362e93d1"}}

	if rec := post(s, form); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "received") {
		t.Fatalf("first submission: %d %s", rec.Code, rec.Body.String())
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), "github.com/a/b") {
		t.Fatalf("submission was not recorded durably: %v %s", err, body)
	}

	if rec := post(s, form); !strings.Contains(rec.Body.String(), "duplicate") {
		t.Fatalf("resubmitting the same repo must be reported as duplicate: %s", rec.Body.String())
	}
}

func TestSubmitHoneypotLooksNormalButIsNeverStored(t *testing.T) {
	s, path := newTestServer(t, time.Now())
	rec := post(s, url.Values{
		"repo": {"https://github.com/spam/bot"}, "contact": {"x@y.zz"}, "website": {"http://spam"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a honeypot hit must look normal to the bot, got %d", rec.Code)
	}
	if body, _ := os.ReadFile(path); len(body) != 0 {
		t.Fatalf("a honeypot hit must not be written to disk, got %q", body)
	}
}

// A filled honeypot used to skip the rate limiter and the daily cap while still being
// fsynced to disk, which made the trap an unmetered write path on a public endpoint.
func TestHoneypotFloodCannotGrowTheStoreOrEscapeTheLimiter(t *testing.T) {
	s, path := newTestServer(t, time.Now())
	for i := 0; i < 50; i++ {
		if code := post(s, url.Values{
			"repo": {"https://github.com/spam/x"}, "contact": {"x@y.zz"}, "website": {"bot"},
		}).Code; code != http.StatusOK {
			t.Fatalf("submission %d: honeypot must stay opaque, got %d", i, code)
		}
	}
	if body, _ := os.ReadFile(path); len(body) != 0 {
		t.Fatalf("50 honeypot hits wrote %d bytes; want none", len(body))
	}
	// Those hits consumed the per-IP budget, so a real submission now has to wait.
	real := post(s, url.Values{
		"repo": {"https://github.com/real/project"}, "contact": {"me@example.com"},
		"campaign_id": {"hunt-20261125t065107"}, "wallet": {"HMC-73bdd7dd362e93d1"},
	})
	if real.Code != http.StatusTooManyRequests {
		t.Fatalf("honeypot hits must cost rate-limit tokens, got %d", real.Code)
	}
}

func TestSubmitRejectsBadInputAndClosedWindow(t *testing.T) {
	s, _ := newTestServer(t, time.Now())
	if rec := post(s, url.Values{"repo": {"nope"}, "contact": {"me@example.com"}, "campaign_id": {"hunt-20261125t065107"}, "wallet": {"HMC-73bdd7dd362e93d1"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad repo must be rejected, got %d", rec.Code)
	}
	if rec := post(s, url.Values{"repo": {"https://github.com/a/b"}, "contact": {"nope"}, "campaign_id": {"hunt-20261125t065107"}, "wallet": {"HMC-73bdd7dd362e93d1"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad contact must be rejected, got %d", rec.Code)
	}

	closed, _ := newTestServer(t, time.Date(2026, 12, 9, 0, 0, 0, 0, time.UTC))
	closed.deadline = time.Date(2026, 12, 7, 23, 59, 59, 0, time.UTC)
	rec := post(closed, url.Values{"repo": {"https://github.com/a/b"}, "contact": {"me@example.com"}, "campaign_id": {"hunt-20261125t065107"}, "wallet": {"HMC-73bdd7dd362e93d1"}})
	if !strings.Contains(rec.Body.String(), "closed") {
		t.Fatalf("a past deadline must close intake server-side: %s", rec.Body.String())
	}
}

func TestSubmitDailyCap(t *testing.T) {
	s, _ := newTestServer(t, time.Now()) // cap is 3
	s.limiter = newIPLimiter(100, time.Hour)
	for i, repo := range []string{"a", "b", "c"} {
		if rec := post(s, url.Values{"repo": {"https://github.com/x/" + repo}, "contact": {"me@example.com"}, "campaign_id": {"hunt-2026112" + repo}, "wallet": {"HMC-73bdd7dd362e93d1"}}); rec.Code != http.StatusOK {
			t.Fatalf("submission %d: %d", i, rec.Code)
		}
	}
	rec := post(s, url.Values{"repo": {"https://github.com/x/d"}, "contact": {"me@example.com"}, "campaign_id": {"hunt-20261125zz"}, "wallet": {"HMC-73bdd7dd362e93d1"}})
	if !strings.Contains(rec.Body.String(), "full") {
		t.Fatalf("the daily cap must stop the fourth: %s", rec.Body.String())
	}
}

func TestStoreReloadsDedupAndCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subs.jsonl")
	st, err := newStore(path, 10)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	rec := Submission{TS: time.Now().UTC().Format(time.RFC3339), Repo: "https://github.com/a/b", Contact: "x@y.zz"}
	if err := st.append(rec); err != nil {
		t.Fatalf("append: %v", err)
	}
	reopened, err := newStore(path, 10)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := reopened.append(rec); !errors.Is(err, errDuplicate) {
		t.Fatalf("a restart must not forget what was already submitted, got %v", err)
	}
	if reopened.dayN != 1 {
		t.Fatalf("today's count must survive a restart, got %d", reopened.dayN)
	}
}

func TestTelegramTextIsPlainAndFlagsUnknownHost(t *testing.T) {
	txt := telegramText(Submission{
		TS: "2026-11-25T10:00:00Z", Repo: "https://git.example.org/a/b", Contact: "@someone", Note: "*bold* _x_",
		CampaignID: "hunt-20261125t065107", Wallet: "HMC-73bdd7dd362e93d1",
	})
	if !strings.Contains(txt, "hunt-20261125t065107") || !strings.Contains(txt, "HMC-73bdd7dd362e93d1") {
		t.Fatal("the operator needs the campaign id and wallet to rank and pay")
	}
	if !strings.Contains(txt, "unknown host") {
		t.Fatal("an unknown host must be flagged for the operator")
	}
	if !strings.Contains(txt, "*bold* _x_") {
		t.Fatal("notes are sent as plain text, so markup stays literal")
	}
}

func TestStatusEndpointReportsWindow(t *testing.T) {
	s, _ := newTestServer(t, time.Date(2026, 12, 9, 0, 0, 0, 0, time.UTC))
	s.deadline = time.Date(2026, 12, 7, 23, 59, 59, 0, time.UTC)
	rec := httptest.NewRecorder()
	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/fuzz-week/status", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"open":false`) || !strings.Contains(body, `"state":"closed"`) {
		t.Fatalf("status must report the closed window: %s", body)
	}
}

func TestNotifierDisabledWithoutTokenOrChat(t *testing.T) {
	n := &notifier{client: &http.Client{}}
	if n.enabled() {
		t.Fatal("no token and no chat must mean disabled")
	}
	if err := n.send("x"); err != nil {
		t.Fatalf("a disabled notifier must be a no-op, got %v", err)
	}
	if (&notifier{token: "t", client: &http.Client{}}).enabled() {
		t.Fatal("a token without a chat id must stay disabled")
	}
}

func TestClientIPPrefersRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Real-IP", "203.0.113.7")
	if got := clientIP(req); got != "203.0.113.7" {
		t.Fatalf("want the nginx-provided address, got %q", got)
	}
	plain := httptest.NewRequest(http.MethodPost, "/", nil)
	plain.RemoteAddr = "198.51.100.4:1234"
	if got := clientIP(plain); got != "198.51.100.4" {
		t.Fatalf("want the port stripped, got %q", got)
	}
}

func TestValidateCampaignID(t *testing.T) {
	if _, err := validateCampaignID("hunt-20261125t065107"); err != nil {
		t.Fatalf("a real campaign id must be accepted: %v", err)
	}
	for _, bad := range []string{"", "short", "has space", "semi;colon"} {
		if _, err := validateCampaignID(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestValidateWallet(t *testing.T) {
	if _, err := validateWallet("HMC-73bdd7dd362e93d1"); err != nil {
		t.Fatalf("a real HMC address must be accepted: %v", err)
	}
	for _, bad := range []string{"", "73bdd7dd362e93d1", "HMC-", "HMC-xyz", "HMC-zz11", "BTC-73bdd7dd"} {
		if _, err := validateWallet(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestSubmitRequiresCampaignAndWallet(t *testing.T) {
	s, _ := newTestServer(t, time.Now())
	rec := post(s, url.Values{"repo": {"https://github.com/a/b"}, "contact": {"me@example.com"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "campaign id") {
		t.Fatalf("a submission with no campaign cannot be ranked: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(s, url.Values{"repo": {"https://github.com/a/b"}, "contact": {"me@example.com"}, "campaign_id": {"hunt-20261125t065107"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "wallet") {
		t.Fatalf("a prize needs a wallet: %d %s", rec.Code, rec.Body.String())
	}
}
