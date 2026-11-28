package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSealedReportOmitsPayload(t *testing.T) {
	a, db := newWalletTestApp(t)
	const camp = "sealed-report"
	const token = "sealed-report-token"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	cfg := map[string]any{
		"report_seal_pub":    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		"seed_byte_corpus":   []any{"73656372657473656564"},
		"hunt_git_url":       "https://secret.example/repo",
		"upstream_target_id": "tinyexr",
	}
	raw, _ := json.Marshal(cfg)
	if _, err := db.ExecContext(context.Background(), `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, string(raw), camp); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	detail := `{"input_hex":"5345435245542d4259544553","trap":"stack-buffer-overflow in parse_exr","box":"opaque-box"}`
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, 'native_crash', 'critical', ?, ?, ?, ?, ?, ?)`,
		"seal-f1", camp, "heap-overflow in parse_exr", "deadbeef", "/tmp/secret-input", "run ./harness secret", detail, now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := a.buildFuzzReport(context.Background(), camp, 20)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{
		"parse_exr", "5345435245542d4259544553", "deadbeef", "/tmp/secret-input",
		"secret-input", "73656372657473656564", "secret.example", "tinyexr",
		"heap-overflow",
	} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("report still contains %q", leak)
		}
	}
	if !strings.Contains(string(body), "opaque-box") {
		t.Fatal("customer box was dropped")
	}
	if !strings.Contains(string(body), "nacl-box-v1") {
		t.Fatal("seal marker missing")
	}
}
