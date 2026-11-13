package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	modePublish = "publish"

	// ciTokenEnv is the scoped Orders CI token (POST /api/ci/dig only).
	ciTokenEnv = "HACKME_CI_TOKEN"

	maxLabelLen = 100
)

// PublishGuard is the GitHub context that decides whether a real (paid)
// publish may run. Dry-run stays the default on every PR; publish only on
// workflow_dispatch, a push to a protected ref, or a same-repo PR carrying
// the opt-in label (agreed in #86).
type PublishGuard struct {
	Event        string // GITHUB_EVENT_NAME
	RefProtected string // github.ref_protected ("true"/"false")
	PRLabels     string // JSON array of label names on the PR
	PublishLabel string // label that opts a PR in; empty = PRs never publish
	HeadRepo     string // PR head repo full name
	BaseRepo     string // PR base repo full name
}

func checkPublishAllowed(g PublishGuard) error {
	switch g.Event {
	case "workflow_dispatch":
		return nil
	case "push":
		if g.RefProtected == "true" {
			return nil
		}
		return usageErr("publish on push needs a protected branch")
	case "pull_request":
		if strings.TrimSpace(g.PublishLabel) == "" {
			return usageErr("publish on pull_request needs publish-label (and the PR must carry it)")
		}
		if g.HeadRepo == "" || g.HeadRepo != g.BaseRepo {
			return usageErr("publish never runs for PRs from forks")
		}
		var labels []string
		if err := json.Unmarshal([]byte(g.PRLabels), &labels); err != nil {
			return usageErr("could not read PR labels")
		}
		for _, l := range labels {
			if l == g.PublishLabel {
				return nil
			}
		}
		return usageErr("PR does not carry the %q label", g.PublishLabel)
	default:
		// pull_request_target, schedule, issue_comment, …: refuse rather than guess.
		return usageErr("publish is not allowed on %q events", g.Event)
	}
}

// ciLabel turns repo/ref context into the server's label charset.
func ciLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			strings.ContainsRune("._/#:@-", r):
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
		if b.Len() >= maxLabelLen {
			break
		}
	}
	out := strings.ReplaceAll(b.String(), "..", "-")
	out = strings.TrimLeft(out, "._/#:@-")
	if out == "" {
		return "ci"
	}
	return out
}

type ciDigBody struct {
	DigPreset    string `json:"dig_preset,omitempty"`
	GuardPack    string `json:"guard_pack,omitempty"`
	WasmCheckHex string `json:"wasm_check_hex,omitempty"`
	DigPackage   string `json:"dig_package"`
	Title        string `json:"title,omitempty"`
	Label        string `json:"label"`
}

func publishBody(p *Plan, title, label string) (ciDigBody, error) {
	b := ciDigBody{DigPackage: p.DigPackage, Title: title, Label: ciLabel(label)}
	switch p.Source {
	case sourcePreset:
		if p.DigPreset != "" {
			b.DigPreset = p.DigPreset
		} else {
			b.GuardPack = p.GuardPack
		}
	case sourceWasm:
		if len(p.raw) == 0 {
			return b, fmt.Errorf("no wasm bytes to publish")
		}
		b.WasmCheckHex = hex.EncodeToString(p.raw)
		b.GuardPack = p.GuardPack
	default:
		return b, fmt.Errorf("unknown plan source %q", p.Source)
	}
	return b, nil
}

type ciDigResp struct {
	CampaignID  string `json:"campaign_id"`
	ReportToken string `json:"customer_report_token"`
}

// publishDig creates the order. One attempt only: the call spends escrow, so
// a blind retry after a timeout could pay twice.
func publishDig(ctx context.Context, c *gateClient, ciToken string, body ciDigBody) (*ciDigResp, error) {
	tok := strings.TrimSpace(ciToken)
	if tok == "" {
		return nil, usageErr("CI token missing: set %s", ciTokenEnv)
	}
	if strings.ContainsAny(tok, "\r\n") {
		return nil, usageErr("CI token contains a line break")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	u := *c.base
	u.Path = c.base.Path + "/api/ci/dig"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-CI-Token", tok)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /api/ci/dig: %v (not retried: the order may or may not exist, check the node)", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxAPIBody+1))
	if err != nil {
		return nil, fmt.Errorf("POST /api/ci/dig: %v", err)
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, fmt.Errorf("POST /api/ci/dig: HTTP %d redirect refused (token is never sent cross-origin)", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /api/ci/dig: HTTP %d %s", res.StatusCode, shortText(b, 300))
	}
	if len(b) > maxAPIBody {
		return nil, fmt.Errorf("POST /api/ci/dig: response larger than %d bytes", maxAPIBody)
	}
	var out ciDigResp
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("POST /api/ci/dig: bad JSON: %v", err)
	}
	if !campaignIDRE.MatchString(out.CampaignID) || strings.Contains(out.CampaignID, "..") {
		return nil, fmt.Errorf("POST /api/ci/dig: unexpected campaign_id %q", shortText([]byte(out.CampaignID), 80))
	}
	if strings.TrimSpace(out.ReportToken) == "" || strings.ContainsAny(out.ReportToken, "\r\n") {
		return nil, fmt.Errorf("POST /api/ci/dig: no usable report token in the response")
	}
	return &out, nil
}
