package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (m *workManager) relayFuzzSettle(ctx context.Context, kind, campaignID, minerAddress, severity string) error {
	base := strings.TrimRight(strings.TrimSpace(m.ordersProbeURL), "/")
	if base == "" {
		return nil
	}
	tok := m.ordersAdminToken()
	if tok == "" {
		return fmt.Errorf("fuzz settle: orders admin token missing")
	}
	body, _ := json.Marshal(map[string]any{
		"kind":          kind,
		"campaign_id":   campaignID,
		"miner_address": minerAddress,
		"severity":      severity,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/fuzz/pool/settle", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	cl := &http.Client{Timeout: 12 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fuzz settle HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

type wmFuzzSettler struct{ m *workManager }

func (w wmFuzzSettler) PayRun(ctx context.Context, campaignID, minerAddress string) error {
	if w.m == nil {
		return nil
	}
	return w.m.relayFuzzSettle(ctx, "run", campaignID, minerAddress, "")
}

func (w wmFuzzSettler) PayFinding(ctx context.Context, campaignID, minerAddress, severity string) error {
	if w.m == nil {
		return nil
	}
	return w.m.relayFuzzSettle(ctx, "finding", campaignID, minerAddress, severity)
}

func (w wmFuzzSettler) Finalize(ctx context.Context, campaignID string) error {
	if w.m == nil {
		return nil
	}
	return w.m.relayFuzzSettle(ctx, "finalize", campaignID, "", "")
}
