// Command fuzzweek-intake is the public intake for the Free Fuzz Week promo.
//
// It accepts repository submissions from the static hackme.tech form, records every
// one durably to JSONL *before* answering, and then best-effort notifies the operator
// over Telegram. Durability comes first on purpose: a submission is never lost because
// a bot token is missing, expired, or Telegram is down.
//
// The promo window is enforced server-side, so ending the week is a config flip and
// not a page edit made under time pressure.
//
// Env:
//
//	HACKME_FUZZWEEK_LISTEN      — listen address (default 127.0.0.1:18090; loopback only)
//	HACKME_FUZZWEEK_STORE       — JSONL path (default /var/lib/hackme/fuzzweek/submissions.jsonl)
//	HACKME_FUZZWEEK_OPEN_AT     — optional RFC3339 or YYYY-MM-DD; before it, intake is closed
//	HACKME_FUZZWEEK_DEADLINE    — optional RFC3339 or YYYY-MM-DD (end of that day UTC); after it, closed
//	HACKME_FUZZWEEK_MAX_PER_DAY — global accepted-per-day cap (default 200)
//	TELEGRAM_BOT_TOKEN or HACKME_TELEGRAM_BOT_TOKEN — operator notify token (optional)
//	HACKME_TELEGRAM_CHAT_ID     — operator chat id (optional; required for notifications)
//
// Submitted content is untrusted: it is length-capped, stripped of control characters,
// and sent to Telegram as plain text with no parse_mode so markup cannot be injected.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxBodyBytes   = 16 << 10
	maxRepoLen     = 300
	maxContactLen  = 200
	maxLangLen     = 40
	maxNoteLen     = 1000
	maxCampaignLen = 80
	maxWalletLen   = 72
	// The promo allows three repositories per person, so a limit of three left no room:
	// a corrected resubmission costs a token, and two participants behind one office NAT
	// or VPN would lock each other out. Abuse is still bounded by the daily cap and by a
	// human reading every submission.
	perIPPerHour  = 6
	notifyTimeout = 10 * time.Second
)

// knownForges are flagged in the operator notification as a quick legitimacy signal.
// Submissions from other hosts are still accepted; the operator vets every one anyway.
var knownForges = map[string]bool{
	"github.com":      true,
	"gitlab.com":      true,
	"codeberg.org":    true,
	"bitbucket.org":   true,
	"git.sr.ht":       true,
	"gitea.com":       true,
	"sourceforge.net": true,
}

// Submission is one intake record, as stored and as notified.
type Submission struct {
	TS         string `json:"ts"`
	Repo       string `json:"repo"`
	Contact    string `json:"contact"`
	Lang       string `json:"lang,omitempty"`
	CampaignID string `json:"campaign_id"`
	Wallet     string `json:"wallet"`
	Note       string `json:"note,omitempty"`
	KnownForge bool   `json:"known_forge"`
	IPHash     string `json:"ip_hash,omitempty"`
}

// scrub removes control characters and collapses runaway whitespace, then caps length.
// Untrusted text reaches an operator's Telegram client, so newline floods and terminal
// control sequences are stripped rather than escaped.
func scrub(s string, max int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if r == '\n' || r == '\t' {
			r = ' '
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		if r == ' ' {
			if lastSpace {
				continue
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if max > 0 {
		r := []rune(out)
		if len(r) > max {
			out = strings.TrimSpace(string(r[:max])) + "…"
		}
	}
	return out
}

// normalizeRepo validates a submitted repository URL and returns its canonical form
// plus whether the host is a known forge. It requires https and rejects embedded
// credentials, so a submission can never carry a secret into the operator's chat.
func normalizeRepo(raw string) (string, bool, error) {
	raw = scrub(raw, maxRepoLen)
	if raw == "" {
		return "", false, errors.New("repository link is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, errors.New("repository link is not a valid URL")
	}
	if u.Scheme != "https" {
		return "", false, errors.New("repository link must start with https://")
	}
	if u.User != nil {
		return "", false, errors.New("remove credentials from the repository link")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || !strings.Contains(host, ".") {
		return "", false, errors.New("repository link needs a valid host")
	}
	u.Fragment = ""
	u.Host = host
	if u.Path == "/" {
		u.Path = ""
	}
	if u.Path == "" {
		return "", false, errors.New("link to a repository, not just a host")
	}
	return u.String(), knownForges[host], nil
}

// validateContact accepts an email, an @handle, or a t.me link — anything the operator
// can actually reply to.
func validateContact(raw string) (string, error) {
	c := scrub(raw, maxContactLen)
	if len([]rune(c)) < 3 {
		return "", errors.New("contact is required so we can reach you")
	}
	lower := strings.ToLower(c)
	switch {
	case strings.Contains(c, "@") && !strings.HasPrefix(c, "@") && strings.Contains(c, "."):
		return c, nil // email
	case strings.HasPrefix(c, "@") && len([]rune(c)) >= 4:
		return c, nil // telegram handle
	case strings.HasPrefix(lower, "https://t.me/"), strings.HasPrefix(lower, "t.me/"):
		return c, nil
	}
	return "", errors.New("use an email, an @telegram handle, or a t.me link")
}

// validateCampaignID checks the id of the campaign the submitter already placed.
// Free Fuzz Week ranks the run, so a submission without a campaign cannot be judged.
func validateCampaignID(raw string) (string, error) {
	c := scrub(raw, maxCampaignLen)
	if c == "" {
		return "", errors.New("campaign id is required — place the campaign first, then register it here")
	}
	if len([]rune(c)) < 6 {
		return "", errors.New("that campaign id looks too short")
	}
	for _, r := range c {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return "", errors.New("campaign id may only contain letters, digits, dot, dash and underscore")
		}
	}
	return c, nil
}

// validateWallet checks the HMC address a prize would be paid to. Only the shape is
// checked here; the chain remains the authority at payout time.
func validateWallet(raw string) (string, error) {
	w := scrub(raw, maxWalletLen)
	if w == "" {
		return "", errors.New("HMC wallet is required so a prize can be paid on-chain")
	}
	if !strings.HasPrefix(w, "HMC-") {
		return "", errors.New("HMC wallet must start with HMC-")
	}
	hex := strings.TrimPrefix(w, "HMC-")
	if len(hex) < 8 {
		return "", errors.New("that HMC wallet looks too short")
	}
	for _, r := range hex {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return "", errors.New("HMC wallet must be HMC- followed by hex characters")
		}
	}
	return w, nil
}

// parseWindowBound accepts RFC3339 or YYYY-MM-DD. A bare date means the end of that
// day in UTC for a deadline and the start of it for an opening, which is what an
// operator writing "2026-12-07" in a unit file means.
func parseWindowBound(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("want RFC3339 or YYYY-MM-DD, got %q", raw)
	}
	if endOfDay {
		return d.UTC().Add(24*time.Hour - time.Second), nil
	}
	return d.UTC(), nil
}

// windowOpen reports whether intake accepts submissions at now.
func windowOpen(now, openAt, deadline time.Time) (bool, string) {
	if !openAt.IsZero() && now.Before(openAt) {
		return false, "not_open_yet"
	}
	if !deadline.IsZero() && now.After(deadline) {
		return false, "closed"
	}
	return true, "open"
}

// ipLimiter is a small fixed-window limiter keyed by client IP.
type ipLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

func newIPLimiter(max int, window time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, window: window, max: max}
}

// allow records a hit and reports whether it is within the limit.
func (l *ipLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	// Opportunistic sweep so the map cannot grow without bound.
	if len(l.hits) > 4096 {
		for k, v := range l.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.hits, k)
			}
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// store appends submissions and remembers which repositories were already seen.
type store struct {
	mu     sync.Mutex
	path   string
	seen   map[string]bool
	day    string
	dayN   int
	maxDay int
}

func newStore(path string, maxDay int) (*store, error) {
	s := &store{path: path, seen: map[string]bool{}, maxDay: maxDay}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	// Rebuild dedup and today's count from the existing log so a restart mid-promo
	// does not re-notify or reset the daily cap.
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	s.day = today
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec Submission
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Repo != "" {
			s.seen[rec.Repo] = true
		}
		if strings.HasPrefix(rec.TS, today) {
			s.dayN++
		}
	}
	return s, nil
}

var errDuplicate = errors.New("duplicate")
var errDayFull = errors.New("day_full")

// append writes the record durably. It reports errDuplicate for a repository already
// on file and errDayFull once the global daily cap is reached, so neither case turns
// into operator spam.
func (s *store) append(rec Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[rec.Repo] {
		return errDuplicate
	}
	today := time.Now().UTC().Format("2006-01-02")
	if today != s.day {
		s.day, s.dayN = today, 0
	}
	if s.maxDay > 0 && s.dayN >= s.maxDay {
		return errDayFull
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	s.seen[rec.Repo] = true
	s.dayN++
	return nil
}

// telegramText renders one submission as plain text. No parse_mode is used when
// sending, so nothing here needs escaping and no markup can be injected.
func telegramText(rec Submission) string {
	forge := "unknown host — vet carefully"
	if rec.KnownForge {
		forge = "known forge"
	}
	var b strings.Builder
	b.WriteString("Free Fuzz Week — new submission\n\n")
	fmt.Fprintf(&b, "Repo: %s\n(%s)\n", rec.Repo, forge)
	fmt.Fprintf(&b, "Contact: %s\n", rec.Contact)
	fmt.Fprintf(&b, "Campaign: %s\n", rec.CampaignID)
	fmt.Fprintf(&b, "Wallet: %s\n", rec.Wallet)
	if rec.Lang != "" {
		fmt.Fprintf(&b, "Language: %s\n", rec.Lang)
	}
	if rec.Note != "" {
		fmt.Fprintf(&b, "\nNote: %s\n", rec.Note)
	}
	fmt.Fprintf(&b, "\nReceived: %s", rec.TS)
	return b.String()
}

// notifier sends operator notifications; a zero token or chat disables it.
type notifier struct {
	token  string
	chatID string
	client *http.Client
}

func (n *notifier) enabled() bool { return n.token != "" && n.chatID != "" }

// send posts one plain-text message. Errors are logged without the token.
func (n *notifier) send(text string) error {
	if !n.enabled() {
		return nil
	}
	form := url.Values{
		"chat_id":                  {n.chatID},
		"text":                     {text},
		"disable_web_page_preview": {"true"},
	}
	endpoint := "https://api.telegram.org/bot" + n.token + "/sendMessage"
	resp, err := n.client.PostForm(endpoint, form)
	if err != nil {
		// url.Error embeds the request URL, which carries the token: report only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("telegram post failed: %v", ue.Err)
		}
		return errors.New("telegram post failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("telegram replied %d", resp.StatusCode)
	}
	return nil
}

// clientIP trusts X-Real-IP because the service listens on loopback and nginx, the
// only possible client, sets it.
func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type server struct {
	store    *store
	limiter  *ipLimiter
	notify   *notifier
	openAt   time.Time
	deadline time.Time
	now      func() time.Time
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

// handleStatus lets the page render its own closed state from the server's clock
// rather than the visitor's.
func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	open, state := windowOpen(s.now(), s.openAt, s.deadline)
	out := map[string]any{"ok": true, "open": open, "state": state}
	if !s.deadline.IsZero() {
		out["deadline"] = s.deadline.UTC().Format(time.RFC3339)
	}
	if !s.openAt.IsZero() {
		out["opens_at"] = s.openAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "POST only"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "could not read the form"})
		return
	}
	now := s.now()
	if open, state := windowOpen(now, s.openAt, s.deadline); !open {
		msg := "Free Fuzz Week submissions are closed."
		if state == "not_open_yet" {
			msg = "Free Fuzz Week has not opened yet."
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": state, "message": msg})
		return
	}

	// Honeypot: a real browser leaves this hidden field empty. Answer normally so a
	// bot learns nothing, but record it as spam and never notify.
	honeypot := strings.TrimSpace(r.PostFormValue("website"))

	repo, knownForge, err := normalizeRepo(r.PostFormValue("repo"))
	if err != nil && honeypot == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	contact, cerr := validateContact(r.PostFormValue("contact"))
	if cerr != nil && honeypot == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": cerr.Error()})
		return
	}
	campaignID, iderr := validateCampaignID(r.PostFormValue("campaign_id"))
	if iderr != nil && honeypot == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": iderr.Error()})
		return
	}
	wallet, werr := validateWallet(r.PostFormValue("wallet"))
	if werr != nil && honeypot == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": werr.Error()})
		return
	}

	// Every request costs a token, honeypot hits included: a bot that fills the trap must
	// not get an unmetered path. Only a real submission is told it was limited, so the
	// trap stays invisible to whatever filled it.
	ip := clientIP(r)
	withinLimit := s.limiter.allow(ip, now)
	if honeypot == "" && !withinLimit {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"ok": false, "error": "too many submissions from this address — try again later",
		})
		return
	}

	if honeypot != "" {
		// Answer exactly as a success looks, but write nothing: these records are never
		// notified and never read, so storing attacker-controlled lines would only hand
		// out an unbounded disk write per request.
		log.Printf("[fuzzweek] honeypot submission dropped ip=%s", ip)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "received"})
		return
	}

	rec := Submission{
		TS:         now.UTC().Format(time.RFC3339),
		Repo:       repo,
		Contact:    contact,
		Lang:       scrub(r.PostFormValue("lang"), maxLangLen),
		CampaignID: campaignID,
		Wallet:     wallet,
		Note:       scrub(r.PostFormValue("note"), maxNoteLen),
		KnownForge: knownForge,
	}

	switch err := s.store.append(rec); {
	case errors.Is(err, errDuplicate):
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "status": "duplicate",
			"message": "That repository is already on the list — we will get back to you.",
		})
		return
	case errors.Is(err, errDayFull):
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "status": "full",
			"message": "Today's intake is full. Please try again tomorrow.",
		})
		return
	case err != nil:
		log.Printf("[fuzzweek] store append failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok": false, "error": "could not record the submission — please retry",
		})
		return
	}

	// The submission is already durable; notify out of band so Telegram latency or an
	// outage never delays or fails the visitor's response.
	go func() {
		if err := s.notify.send(telegramText(rec)); err != nil {
			log.Printf("[fuzzweek] operator notify failed (submission is stored): %v", err)
		}
	}()

	log.Printf("[fuzzweek] accepted repo=%s known_forge=%v", rec.Repo, rec.KnownForge)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "status": "received",
		"message": "Got it. We review every submission by hand and will reply to your contact.",
	})
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "optional env file to load before starting")
	flag.Parse()
	if configPath != "" {
		if err := loadEnvFile(configPath); err != nil {
			log.Fatalf("[fuzzweek] config %s: %v", configPath, err)
		}
	}

	listen := envOr("HACKME_FUZZWEEK_LISTEN", "127.0.0.1:18090")
	storePath := envOr("HACKME_FUZZWEEK_STORE", "/var/lib/hackme/fuzzweek/submissions.jsonl")
	maxDay := 200
	if v := strings.TrimSpace(os.Getenv("HACKME_FUZZWEEK_MAX_PER_DAY")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			log.Fatalf("[fuzzweek] HACKME_FUZZWEEK_MAX_PER_DAY must be a non-negative integer, got %q", v)
		}
		maxDay = n
	}
	openAt, err := parseWindowBound(os.Getenv("HACKME_FUZZWEEK_OPEN_AT"), false)
	if err != nil {
		log.Fatalf("[fuzzweek] HACKME_FUZZWEEK_OPEN_AT: %v", err)
	}
	deadline, err := parseWindowBound(os.Getenv("HACKME_FUZZWEEK_DEADLINE"), true)
	if err != nil {
		log.Fatalf("[fuzzweek] HACKME_FUZZWEEK_DEADLINE: %v", err)
	}

	st, err := newStore(storePath, maxDay)
	if err != nil {
		log.Fatalf("[fuzzweek] store %s: %v", storePath, err)
	}
	token := envOr("TELEGRAM_BOT_TOKEN", os.Getenv("HACKME_TELEGRAM_BOT_TOKEN"))
	chatID := strings.TrimSpace(os.Getenv("HACKME_TELEGRAM_CHAT_ID"))
	n := &notifier{token: strings.TrimSpace(token), chatID: chatID, client: &http.Client{Timeout: notifyTimeout}}

	srv := &server{
		store:    st,
		limiter:  newIPLimiter(perIPPerHour, time.Hour),
		notify:   n,
		openAt:   openAt,
		deadline: deadline,
		now:      time.Now,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/fuzz-week/submit", srv.handleSubmit)
	mux.HandleFunc("/api/fuzz-week/status", srv.handleStatus)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})

	if !n.enabled() {
		log.Printf("[fuzzweek] operator notifications DISABLED (set a bot token and HACKME_TELEGRAM_CHAT_ID); submissions are still recorded to %s", storePath)
	} else {
		log.Printf("[fuzzweek] operator notifications enabled")
	}
	window := "always open"
	if !openAt.IsZero() || !deadline.IsZero() {
		window = fmt.Sprintf("open_at=%s deadline=%s", openAt.Format(time.RFC3339), deadline.Format(time.RFC3339))
	}
	log.Printf("[fuzzweek] listening on %s store=%s %s max_per_day=%d", listen, storePath, window, maxDay)

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("[fuzzweek] serve: %v", err)
	}
}
