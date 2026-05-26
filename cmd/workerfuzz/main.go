// workerfuzz — pool distributed fuzz worker (claims WASM check work from coordinator).
//
//   COORD_URL=https://hackme.tech/pool/coordinator COORD_TOKEN=... WORKER_ID=rig-fuzz-01 go run ./cmd/workerfuzz
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
)

type claimResp struct {
	OK              bool   `json:"ok"`
	Reason          string `json:"reason,omitempty"`
	WorkID          string `json:"work_id,omitempty"`
	CampaignID      string `json:"campaign_id,omitempty"`
	ItemID          int64  `json:"item_id,omitempty"`
	InputN          uint64 `json:"input_n,omitempty"`
	ActualInput     uint64 `json:"actual_input,omitempty"`
	WasmCheckHex    string `json:"wasm_check_hex,omitempty"`
	CheckSemantics  string `json:"check_semantics,omitempty"`
}

func main() {
	coordURL := flag.String("coord", strings.TrimSpace(os.Getenv("COORD_URL")), "coordinator base URL")
	token := flag.String("token", strings.TrimSpace(os.Getenv("COORD_TOKEN")), "coordinator worker or admin token")
	workerID := flag.String("worker", strings.TrimSpace(os.Getenv("WORKER_ID")), "worker id")
	minerAddr := flag.String("miner", strings.TrimSpace(os.Getenv("MINER_ADDRESS")), "HMC payout address (20/80 escrow)")
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
	base := strings.TrimRight(*coordURL, "/")
	cl := &http.Client{Timeout: 45 * time.Second}
	fmt.Fprintf(os.Stderr, "workerfuzz: coord=%s worker=%s miner=%s\n", base, *workerID, *minerAddr)
	for {
		cr, err := claim(cl, base, *token, *workerID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "claim:", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if !cr.OK {
			time.Sleep(2 * time.Second)
			continue
		}
		checkRet, durMS, trap := runCheck(cr, *timeoutMS)
		if err := submit(cl, base, *token, *workerID, *minerAddr, cr, checkRet, durMS, trap); err != nil {
			fmt.Fprintln(os.Stderr, "submit:", err)
		}
	}
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
	ok, execErr := sandbox.InvokeCheck(ctx, wasm, cr.ActualInput)
	durationMS = int(time.Since(start).Milliseconds())
	if execErr != nil {
		return 0, durationMS, execErr.Error()
	}
	if ok {
		return 1, durationMS, ""
	}
	return 0, durationMS, ""
}

func submit(cl *http.Client, base, token, workerID, minerAddress string, cr claimResp, checkResult int32, durationMS int, trap string) error {
	body, _ := json.Marshal(map[string]any{
		"worker_id":     workerID,
		"miner_address": strings.TrimSpace(minerAddress),
		"work_id":      cr.WorkID,
		"campaign_id":  cr.CampaignID,
		"item_id":      cr.ItemID,
		"input_n":      cr.InputN,
		"actual_input": cr.ActualInput,
		"check_result": checkResult,
		"duration_ms":  durationMS,
		"trap":         trap,
	})
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
		fmt.Fprintf(os.Stderr, "workerfuzz: finding campaign=%s input=0x%x semantics=%s\n", cr.CampaignID, cr.ActualInput, sem)
	} else if pass {
		fmt.Fprintf(os.Stderr, "workerfuzz: ok campaign=%s input=0x%x\n", cr.CampaignID, cr.ActualInput)
	}
	return nil
}
