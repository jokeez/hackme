// workerfuzz — pool distributed fuzz worker (claims WASM check work from coordinator).
//
//	COORD_URL=https://hackme.tech/pool/coordinator COORD_TOKEN=... WORKER_ID=rig-fuzz-01 \
//	HACKME_MINER_ED25519_SEED_HEX=... go run ./cmd/workerfuzz
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
	"hackme/internal/sandbox"
)

type claimResp struct {
	OK             bool    `json:"ok"`
	Reason         string  `json:"reason,omitempty"`
	WorkID         string  `json:"work_id,omitempty"`
	CampaignID     string  `json:"campaign_id,omitempty"`
	ItemID         int64   `json:"item_id,omitempty"`
	InputN         uint64  `json:"input_n,omitempty"`
	ActualInput    uint64  `json:"actual_input,omitempty"`
	InputMode      string  `json:"input_mode,omitempty"`
	InputBytesHex  string  `json:"input_bytes_hex,omitempty"`
	DepthTier      string  `json:"depth_tier,omitempty"`
	PerRunHMC      float64 `json:"per_run_hmc,omitempty"`
	WasmCheckHex   string  `json:"wasm_check_hex,omitempty"`
	CheckSemantics string  `json:"check_semantics,omitempty"`
}

func workerHTTPTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("WORKERFUZZ_HTTP_TIMEOUT_SEC")); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	if strings.Contains(strings.ToLower(os.Getenv("COORD_URL")), "hackme.tech") {
		return 120 * time.Second
	}
	return 45 * time.Second
}

func main() {
	coordURL := flag.String("coord", strings.TrimSpace(os.Getenv("COORD_URL")), "coordinator base URL")
	token := flag.String("token", strings.TrimSpace(os.Getenv("COORD_TOKEN")), "coordinator worker or admin token")
	workerID := flag.String("worker", strings.TrimSpace(os.Getenv("WORKER_ID")), "worker id")
	minerAddr := flag.String("miner", strings.TrimSpace(os.Getenv("MINER_ADDRESS")), "HMC payout address (optional with hybrid sig)")
	timeoutMS := flag.Int("timeout-ms", 500, "WASM check timeout ms")
	flag.Parse()
	if *coordURL == "" {
		*coordURL = "http://127.0.0.1:18081"
	}
	if *token == "" {
		if b, err := os.ReadFile(".secrets/hackme_coordinator_worker_token"); err == nil {
			*token = strings.TrimSpace(string(b))
		}
	}
	if *workerID == "" {
		*workerID = "workerfuzz-1"
	}
	priv, pubHex, derivedAddr, hybrid := loadHybridKey()
	if hybrid {
		if *minerAddr == "" {
			*minerAddr = derivedAddr
		} else if !strings.EqualFold(strings.TrimSpace(*minerAddr), derivedAddr) {
			fmt.Fprintf(os.Stderr, "workerfuzz: MINER_ADDRESS=%s ignored; hybrid signer binds payout=%s\n",
				strings.TrimSpace(*minerAddr), derivedAddr)
			*minerAddr = derivedAddr
		}
		fmt.Fprintf(os.Stderr, "workerfuzz: hybrid signer payout=%s\n", derivedAddr)
	}
	base := strings.TrimRight(*coordURL, "/")
	cl := &http.Client{Timeout: workerHTTPTimeout()}
	fmt.Fprintf(os.Stderr, "workerfuzz: coord=%s worker=%s\n", base, *workerID)
	for {
		cr, err := claim(cl, base, *token, *workerID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "claim:", err)
			// 429 / temporary ban: back off hard so idle canary does not extend bans
			sleep := 2 * time.Second
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "429") || strings.Contains(msg, "banned") || strings.Contains(msg, "no_fuzz_work") {
				sleep = 30 * time.Second
			}
			time.Sleep(sleep)
			continue
		}
		if !cr.OK {
			sleep := 2 * time.Second
			if strings.Contains(strings.ToLower(cr.Reason), "banned") || strings.Contains(strings.ToLower(cr.Reason), "no_fuzz_work") {
				sleep = 30 * time.Second
			}
			time.Sleep(sleep)
			continue
		}
		checkRet, durMS, trap := runCheck(cr, *timeoutMS)
		nonce := uint64(time.Now().UnixNano())
		if err := submit(cl, base, *token, *workerID, *minerAddr, priv, pubHex, hybrid, nonce, cr, checkRet, durMS, trap); err != nil {
			fmt.Fprintln(os.Stderr, "submit:", err)
		}
	}
}

func loadHybridKey() (ed25519.PrivateKey, string, string, bool) {
	seedHex := strings.TrimSpace(os.Getenv("HACKME_MINER_ED25519_SEED_HEX"))
	if seedHex == "" {
		fmt.Fprintln(os.Stderr, "workerfuzz: HACKME_MINER_ED25519_SEED_HEX required (treasury/dev-fee key is not allowed)")
		return nil, "", "", false
	}
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "workerfuzz: invalid HACKME_MINER_ED25519_SEED_HEX")
		return nil, "", "", false
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	addr := "HMC-" + hex.EncodeToString(sum[:])[:16]
	if payoutIsTreasury(addr) {
		fmt.Fprintf(os.Stderr, "workerfuzz: payout address %s is treasury/dev-fee — generate a dedicated worker key (minersign -gen-seed)\n", addr)
		os.Exit(1)
	}
	return priv, hex.EncodeToString(pub), addr, true
}

func payoutIsTreasury(addr string) bool {
	return strings.EqualFold(strings.TrimSpace(addr), chain.DevFeeAddress)
}

func claim(cl *http.Client, base, token, workerID string) (claimResp, error) {
	var out claimResp
	body, _ := json.Marshal(map[string]any{"worker_id": workerID})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/fuzz/work/claim", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := cl.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = json.Unmarshal(b, &out)
	if res.StatusCode != 200 {
		return out, fmt.Errorf("HTTP %d %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return out, nil
}

func runCheck(cr claimResp, timeoutMS int) (checkResult int32, durationMS int, trap string) {
	start := time.Now()
	wasm, err := hex.DecodeString(strings.TrimSpace(cr.WasmCheckHex))
	if err != nil || len(wasm) == 0 {
		return 0, 0, "missing wasm"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	ok, execErr := sandbox.InvokeCheckInput(ctx, wasm, inputForCheck(cr))
	durationMS = int(time.Since(start).Milliseconds())
	if execErr != nil {
		return 0, durationMS, execErr.Error()
	}
	if ok {
		return 1, durationMS, ""
	}
	return 0, durationMS, ""
}

func inputForCheck(cr claimResp) []byte {
	if h := strings.TrimSpace(cr.InputBytesHex); h != "" {
		if b, err := hex.DecodeString(h); err == nil && len(b) > 0 {
			return b
		}
	}
	return nil
}

func submit(cl *http.Client, base, token, workerID, minerAddress string, priv ed25519.PrivateKey, pubHex string, hybrid bool, nonce uint64, cr claimResp, checkResult int32, durationMS int, trap string) error {
	payload := map[string]any{
		"worker_id":    workerID,
		"work_id":      cr.WorkID,
		"campaign_id":  cr.CampaignID,
		"item_id":      cr.ItemID,
		"input_n":      cr.InputN,
		"actual_input": cr.ActualInput,
		"check_result": checkResult,
		"duration_ms":  durationMS,
		"trap":         trap,
		"submit_nonce": nonce,
	}
	if hybrid {
		if priv == nil {
			return errors.New("hybrid signer required but no key loaded")
		}
		signPayload := poolfuzz.SubmitSignPayload{
			WorkerID: workerID, CampaignID: cr.CampaignID, ItemID: cr.ItemID,
			InputN: cr.InputN, ActualInput: cr.ActualInput, CheckResult: checkResult, SubmitNonce: nonce,
		}
		sig := ed25519.Sign(priv, poolfuzz.CanonicalSubmitBytes(signPayload))
		payload["miner_pubkey"] = pubHex
		payload["miner_sig"] = hex.EncodeToString(sig)
		payload["miner_sig_alg"] = "ed25519"
		payload["miner_address"] = minerAddress
	} else if strings.TrimSpace(minerAddress) != "" {
		payload["miner_address"] = strings.TrimSpace(minerAddress)
	}
	if strings.TrimSpace(cr.InputBytesHex) != "" {
		payload["input_bytes_hex"] = strings.TrimSpace(cr.InputBytesHex)
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, base+"/api/fuzz/work/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d %s", res.StatusCode, string(b))
	}
	sem := fuzzengine.ParseCheckSemantics(map[string]any{"check_semantics": cr.CheckSemantics})
	pass, finding := fuzzengine.EvalCheck(sem, checkResult, nil)
	if finding && trap == "" {
		fmt.Fprintf(os.Stderr, "workerfuzz: FINDING campaign=%s input=0x%x semantics=%s\n", cr.CampaignID, cr.ActualInput, sem)
	} else if pass {
		fmt.Fprintf(os.Stderr, "workerfuzz: ok campaign=%s input=0x%x\n", cr.CampaignID, cr.ActualInput)
	}
	return nil
}
