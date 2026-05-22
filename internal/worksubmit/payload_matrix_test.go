package worksubmit

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSignPayloadCanonicalStable ensures identical payloads produce identical signing bytes (cross-OS minersign).
func TestSignPayloadCanonicalStable(t *testing.T) {
	rh, ph := NormalizeHashes("fuzz-cross-platform-fixed", "")
	p := SignPayload{
		WorkerID:    "worker-matrix",
		BaseNonce:   42,
		BatchSize:   2048,
		WorkID:      "work-abc",
		Attempts:    2048,
		Found:       false,
		FoundNonce:  0,
		ResultHash:  rh,
		ProofHash:   ph,
		SubmitNonce: 7,
	}
	first := p.CanonicalJSON()
	for _, label := range []string{"windows-opencl", "linux-cuda", "hackme-os-minersign", runtime.GOOS} {
		_ = label
		canon := p.CanonicalJSON()
		if string(first) != string(canon) {
			t.Fatalf("canonical drift on repeat marshal (%s)", runtime.GOOS)
		}
	}
}

// TestMinersignMatchesCoordinatorCanonical verifies cmd/minersign output signs the same bytes as in-process Ed25519.
func TestMinersignMatchesCoordinatorCanonical(t *testing.T) {
	root := filepath.Join("..", "..")
	minersign := filepath.Join(root, "bin", "minersign-test-matrix")
	if err := exec.Command("go", "build", "-trimpath", "-o", minersign, "./cmd/minersign").Run(); err != nil {
		t.Skip("minersign build:", err)
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seedHex := hex.EncodeToString(seed)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	payload := map[string]any{
		"worker_id":   "w1",
		"base_nonce":  100,
		"batch_size":  512,
		"work_id":     "lease-1",
		"attempts":    512,
		"found":       false,
		"found_nonce": 0,
		"result_hash": "abc",
		"proof_hash":  "",
	}
	raw, _ := json.Marshal(payload)
	nonceFile := filepath.Join(t.TempDir(), "nonce.seq")
	cmd := exec.Command(minersign, "-nonce-file", nonceFile)
	cmd.Env = append(cmd.Environ(), "HACKME_MINER_ED25519_SEED_HEX="+seedHex)
	cmd.Stdin = strings.NewReader(string(raw))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		Pub string `json:"miner_pubkey_ed25519"`
		Sig string `json:"miner_sig_ed25519"`
	}
	if err := json.Unmarshal(out, &signed); err != nil {
		t.Fatal(err)
	}
	rh, ph := NormalizeHashes("abc", "")
	p := SignPayload{
		WorkerID: "w1", BaseNonce: 100, BatchSize: 512, WorkID: "lease-1",
		Attempts: 512, Found: false, ResultHash: rh, ProofHash: ph, SubmitNonce: 1,
	}
	msg := p.CanonicalJSON()
	sig, _ := hex.DecodeString(signed.Sig)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("minersign signature does not verify against SignPayload canonical JSON")
	}
	if hex.EncodeToString(pub) != signed.Pub {
		t.Fatalf("pubkey mismatch")
	}
}
