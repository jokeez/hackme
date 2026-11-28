package fuzzreport

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestSealRoundTripHidesPlaintext(t *testing.T) {
	t.Setenv("HACKME_REPORT_SEAL_PEPPER", "test-pepper")
	pub, priv, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"title":     "heap-overflow in parse_exr",
		"input_hex": "414243",
		"trap":      "stack-buffer-overflow in parse_exr",
	}
	raw, err := Envelope(pub, payload, "dedup-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"parse_exr", "414243", "heap-overflow", "stack-buffer"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("sealed envelope contains %q", leak)
		}
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(pub, priv, env["box"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), "parse_exr") || !strings.Contains(string(opened), "414243") {
		t.Fatalf("opened box lost the payload: %s", opened)
	}
	otherPub, otherPriv, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(otherPub, otherPriv, env["box"].(string)); err == nil {
		t.Fatal("other key opened the box")
	}
	fp := InputFingerprint("camp", "aa")
	if fp == "" || fp == "aa" || len(fp) != 64 {
		t.Fatalf("fingerprint %q", fp)
	}
	if _, err := hex.DecodeString(fp); err != nil {
		t.Fatal(err)
	}
	if InputFingerprint("camp", "aa") != fp {
		t.Fatal("fingerprint not stable")
	}
	if tok := BucketToken("camp", "crash|stack_oob|parse_exr"); tok == "" || strings.Contains(tok, "parse_exr") {
		t.Fatalf("bucket token echoes the bug: %q", tok)
	}
}
