// minersign prints Ed25519 signature fields for coordinator hybrid signer submits.
//
// Usage:
//
//	HACKME_MINER_ED25519_SEED_HEX=<64 hex chars = 32 bytes> minersign --nonce-file ./nonce.seq < payload.json
//
// payload.json fields (subset): worker_id, base_nonce, batch_size, work_id, attempts,
// found, found_nonce, result_hash, proof_hash
//
// Flags:
//
//	-gen-seed            print a fresh seed + pubkey + derived payout address
//	-nonce-file PATH     persistent monotonic submit_nonce store (required for stdin mode)
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hackme/internal/worksubmit"

	"github.com/tyler-smith/go-bip39"
)

func deriveAddress(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

func main() {
	genSeed := flag.Bool("gen-seed", false, "generate ed25519 seed (32 bytes hex) and derived miner address")
	noncePath := flag.String("nonce-file", "", "persistent monotonic submit_nonce store (required)")
	flag.Parse()

	if *genSeed {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "minersign: generate: %v\n", err)
			os.Exit(1)
		}
		seed := priv.Seed()
		recoveryPhrase := ""
		if phrase, err := bip39.NewMnemonic(seed); err == nil {
			recoveryPhrase = phrase
		}
		out := map[string]string{
			"miner_seed_hex":                hex.EncodeToString(seed),
			"miner_pubkey_ed25519":          hex.EncodeToString(pub),
			"miner_address_from_pubkey":     deriveAddress(pub),
			"HACKME_MINER_ED25519_SEED_HEX": hex.EncodeToString(seed),
			"recovery_phrase":               recoveryPhrase,
			"recovery_phrase_note":          "24-word phrase encodes your 32-byte HackMe mining key (HMC payout). Write it down offline to claim mined HMC — not a Bitcoin/Ethereum wallet seed.",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.Encode(out)
		return
	}

	if strings.TrimSpace(*noncePath) == "" {
		fmt.Fprintf(os.Stderr, "usage: minersign -nonce-file PATH < payload.json\n       minersign -gen-seed\n")
		os.Exit(2)
	}

	seedHex := strings.TrimSpace(os.Getenv("HACKME_MINER_ED25519_SEED_HEX"))
	if seedHex == "" {
		fmt.Fprintf(os.Stderr, "minersign: HACKME_MINER_ED25519_SEED_HEX required (32-byte seed as 64 hex chars)\n")
		os.Exit(1)
	}
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintf(os.Stderr, "minersign: invalid seed hex (want %d bytes)\n", ed25519.SeedSize)
		os.Exit(1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	var partial struct {
		WorkerID   string `json:"worker_id"`
		BaseNonce  uint64 `json:"base_nonce"`
		BatchSize  uint64 `json:"batch_size"`
		WorkID     string `json:"work_id"`
		Attempts   uint64 `json:"attempts"`
		Found      bool   `json:"found"`
		FoundNonce uint64 `json:"found_nonce"`
		ResultHash string `json:"result_hash"`
		ProofHash  string `json:"proof_hash"`
	}
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(&partial); err != nil {
		fmt.Fprintf(os.Stderr, "minersign: json: %v\n", err)
		os.Exit(1)
	}

	submitNonce, err := bumpNonceFile(strings.TrimSpace(*noncePath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "minersign: nonce file: %v\n", err)
		os.Exit(1)
	}

	rh, ph := worksubmit.NormalizeHashes(partial.ResultHash, partial.ProofHash)
	p := worksubmit.SignPayload{
		WorkerID:    strings.TrimSpace(partial.WorkerID),
		BaseNonce:   partial.BaseNonce,
		BatchSize:   partial.BatchSize,
		WorkID:      strings.TrimSpace(partial.WorkID),
		Attempts:    partial.Attempts,
		Found:       partial.Found,
		FoundNonce:  partial.FoundNonce,
		ResultHash:  rh,
		ProofHash:   ph,
		SubmitNonce: submitNonce,
	}

	msg := p.CanonicalJSON()
	sig := ed25519.Sign(priv, msg)

	out := map[string]any{
		"miner_pubkey_ed25519": hex.EncodeToString(pub),
		"miner_sig_ed25519":    hex.EncodeToString(sig),
		"miner_sig_alg":        "ed25519",
		"submit_nonce":         submitNonce,
		"miner_address":        deriveAddress(pub),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "minersign: encode: %v\n", err)
		os.Exit(1)
	}
}

func bumpNonceFile(path string) (uint64, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
	}
	var next uint64 = 1
	if b, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "" {
			v, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return 0, err
			}
			if v == ^uint64(0) {
				return 0, fmt.Errorf("nonce overflow")
			}
			next = v + 1
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	data := []byte(strconv.FormatUint(next, 10) + "\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		if werr := os.WriteFile(path, data, 0o644); werr != nil {
			_ = os.Remove(tmp)
			return 0, fmt.Errorf("rename: %w; write: %v", err, werr)
		}
		_ = os.Remove(tmp)
	}
	return next, nil
}
