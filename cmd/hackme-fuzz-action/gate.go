package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	modeGate = "gate"

	// reportTokenEnv carries the customer report token. Never a flag: flags
	// show up in `ps` and in workflow logs.
	reportTokenEnv = "HACKME_REPORT_TOKEN"

	maxAPIBody = 1 << 20
)

var campaignIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// GateOptions drive `-mode gate`: wait for a published campaign to finish and
// turn its CI gate into the job verdict.
type GateOptions struct {
	BaseURL     string
	CampaignID  string
	Token       string
	Timeout     time.Duration // overall wait for a terminal status
	Poll        time.Duration
	MaxCritical int
	MaxHigh     int
	MinRunsDone int // <0: require the full budget_runs reported by pulse
}

// GateResult is what the job reports.
type GateResult struct {
	CampaignID string   `json:"campaign_id"`
	Status     string   `json:"status"`
	Finished   bool     `json:"finished"`
	Pass       bool     `json:"pass"`
	Reasons    []string `json:"reasons"`
	RunsDone   int      `json:"runs_done"`
	BudgetRuns int      `json:"budget_runs"`
	Critical   int      `json:"critical_count"`
	High       int      `json:"high_count"`
	Crashes    int      `json:"crash_count"`
	Assurance  string   `json:"assurance_note,omitempty"`
}

// OK is true only for a finished campaign whose gate passed.
func (g *GateResult) OK() bool { return g != nil && g.Finished && g.Pass }

type gateClient struct {
	base  *url.URL
	token string
	http  *http.Client
	sleep func(context.Context, time.Duration) error
}

// parseBaseURL accepts https anywhere and plain http only for loopback, so a
// typo can't ship the report token over the internet in clear text.
func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, usageErr("hackme-url %q is not an absolute URL", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, usageErr("hackme-url must not carry credentials, query or fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, usageErr("hackme-url must be https (plain http only for loopback)")
		}
	default:
		return nil, usageErr("hackme-url must be https")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

// newAPIClient builds the hardened HTTP client for a node base URL.
func newAPIClient(baseURL string) (*gateClient, error) {
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	return &gateClient{
		base: base,
		http: &http.Client{
			Timeout: 30 * time.Second,
			// Never follow redirects: custom headers (the tokens) would ride along.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}, nil
}

// newGateClient is newAPIClient plus a validated campaign id and report token.
func newGateClient(o GateOptions) (*gateClient, error) {
	c, err := newAPIClient(o.BaseURL)
	if err != nil {
		return nil, err
	}
	if !campaignIDRE.MatchString(o.CampaignID) || strings.Contains(o.CampaignID, "..") {
		return nil, usageErr("campaign id %q: use [A-Za-z0-9._-], max 128", o.CampaignID)
	}
	tok := strings.TrimSpace(o.Token)
	if tok == "" {
		return nil, usageErr("report token missing: set %s", reportTokenEnv)
	}
	if strings.ContainsAny(tok, "\r\n") {
		return nil, usageErr("report token contains a line break")
	}
	c.token = tok
	return c, nil
}

// errTransient marks failures worth retrying on the next poll.
var errTransient = errors.New("transient")

func (c *gateClient) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	u := *c.base
	u.Path = c.base.Path + path
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Hackme-Report-Token", c.token)
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: GET %s: %v", errTransient, path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAPIBody+1))
	if err != nil {
		return fmt.Errorf("%w: GET %s: %v", errTransient, path, err)
	}
	if len(body) > maxAPIBody {
		return fmt.Errorf("GET %s: response larger than %d bytes", path, maxAPIBody)
	}
	switch {
	case res.StatusCode == http.StatusOK:
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
		return fmt.Errorf("%w: GET %s: HTTP %d", errTransient, path, res.StatusCode)
	case res.StatusCode >= 300 && res.StatusCode < 400:
		return fmt.Errorf("GET %s: HTTP %d redirect refused (token is never sent cross-origin)", path, res.StatusCode)
	default:
		// The body may echo request data; keep it out of logs beyond a short hint.
		return fmt.Errorf("GET %s: HTTP %d %s", path, res.StatusCode, shortText(body, 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: bad JSON: %v", path, err)
	}
	return nil
}

type pulseResp struct {
	Status   string `json:"status"`
	Progress struct {
		RunsDone   int `json:"runs_done"`
		BudgetRuns int `json:"budget_runs"`
	} `json:"progress"`
}

type gateResp struct {
	Pass      bool     `json:"pass"`
	Reasons   []string `json:"reasons"`
	Assurance string   `json:"assurance_note"`
	Observed  struct {
		Critical int `json:"critical_count"`
		High     int `json:"high_count"`
		Crashes  int `json:"crash_count"`
		RunsDone int `json:"runs_done"`
	} `json:"observed"`
}

func terminalStatus(s string) bool {
	switch s {
	case "completed", "cancelled", "failed":
		return true
	}
	return false
}

// WaitGate polls pulse until the campaign reaches a terminal status (or the
// timeout), then reads the gate. A gate read on a still-running campaign would
// trivially pass (no findings yet), so an unfinished campaign never passes.
func WaitGate(ctx context.Context, c *gateClient, o GateOptions) (*GateResult, error) {
	res := &GateResult{CampaignID: o.CampaignID}
	pulsePath := "/api/fuzz/campaigns/" + url.PathEscape(o.CampaignID) + "/pulse"
	gatePath := "/api/fuzz/campaigns/" + url.PathEscape(o.CampaignID) + "/gate"

	wctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	var lastErr error
	for {
		var p pulseResp
		err := c.getJSON(wctx, pulsePath, nil, &p)
		switch {
		case err == nil:
			lastErr = nil
			res.Status = p.Status
			res.RunsDone = p.Progress.RunsDone
			res.BudgetRuns = p.Progress.BudgetRuns
		case errors.Is(err, errTransient):
			lastErr = err
		default:
			return nil, err
		}
		if err == nil && terminalStatus(p.Status) {
			res.Finished = true
			break
		}
		if serr := c.sleep(wctx, o.Poll); serr != nil {
			res.Reasons = append(res.Reasons, fmt.Sprintf("campaign not finished after %s (status %q)", o.Timeout, res.Status))
			if lastErr != nil {
				res.Reasons = append(res.Reasons, "last error: "+lastErr.Error())
			}
			return res, nil
		}
	}

	if res.Status != "completed" {
		res.Reasons = append(res.Reasons, fmt.Sprintf("campaign ended as %q, not completed", res.Status))
		return res, nil
	}
	minRuns := o.MinRunsDone
	if minRuns < 0 {
		minRuns = res.BudgetRuns
	}
	q := url.Values{}
	q.Set("max_critical", strconv.Itoa(o.MaxCritical))
	q.Set("max_high", strconv.Itoa(o.MaxHigh))
	q.Set("min_runs_done", strconv.Itoa(minRuns))
	var g gateResp
	// The gate read gets its own budget: the campaign is done, don't let a
	// long wait starve it.
	gctx, gcancel := context.WithTimeout(ctx, 2*time.Minute)
	defer gcancel()
	if err := c.getJSON(gctx, gatePath, q, &g); err != nil {
		return nil, err
	}
	res.Pass = g.Pass
	res.Reasons = append(res.Reasons, g.Reasons...)
	res.Assurance = g.Assurance
	res.Critical = g.Observed.Critical
	res.High = g.Observed.High
	res.Crashes = g.Observed.Crashes
	if g.Observed.RunsDone > 0 {
		res.RunsDone = g.Observed.RunsDone
	}
	return res, nil
}

func shortText(b []byte, n int) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}
