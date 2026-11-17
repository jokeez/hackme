// rc20_pool_client — signed hub pool Orders calls for live E2E (no secrets printed).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
	"hackme/internal/nodecrypto"
	"hackme/internal/poolsync"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rc20_pool_client fund-confirm|dig|hunt|cancel|prepaid …")
		os.Exit(2)
	}
	cmd := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)

	dataDir := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR"))
	hub := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_ORDERS_HUB_URL")), "/")
	if hub == "" {
		hub = "https://hackme.tech"
	}
	if dataDir == "" {
		fatal("HACKME_DATA_DIR required")
	}
	signer, err := nodecrypto.Load(dataDir)
	if err != nil {
		fatal("signer: %v", err)
	}
	owner := chain.OrdersPrepaidNormalizeAddr(signer.Address())
	tok := poolsync.OrdersPublishToken()
	if tok == "" {
		fatal("publish token env missing")
	}

	switch cmd {
	case "fund-confirm":
		tx := flag.String("tx", "", "tx hash")
		amt := flag.Float64("amount-hmc", 0, "amount hmc")
		flag.Parse()
		if *tx == "" || *amt <= 0 {
			fatal("need -tx and -amount-hmc")
		}
		body, _ := json.Marshal(map[string]any{
			"tx_hash":      strings.TrimSpace(*tx),
			"amount_units": chain.HMCToUnits(*amt),
		})
		call(hub+"/api/orders/pool/fund/confirm", tok, owner, signer, body)
	case "dig":
		budget := flag.Float64("budget-hmc", 1, "budget")
		title := flag.String("title", "rc20-e2e", "title")
		pkg := flag.String("package", "scan", "dig package: scan|audit|deep")
		flag.Parse()
		digPkg := strings.ToLower(strings.TrimSpace(*pkg))
		if digPkg == "" {
			digPkg = "scan"
		}
		preset := "scan_smoke"
		runs := 64
		depth := "wasm_only"
		switch digPkg {
		case "audit", "pro":
			digPkg = "audit"
			preset = "audit_secrets"
			runs = 256
			depth = "wasm_native"
			if *budget < 5 {
				*budget = 5
			}
		case "deep", "enterprise":
			digPkg = "deep"
			preset = ""
			runs = 2048
			depth = "bytes_corpus"
			if *budget < 40 {
				*budget = 40
			}
		default:
			digPkg = "scan"
		}
		payload := map[string]any{
			"title":            *title,
			"dig_package":      digPkg,
			"budget_hmc":       *budget,
			"budget_runs":      runs,
			"pool_distributed": true,
			"create_poh_order": false,
			"payer_ref":        owner,
			"depth_tier":       depth,
		}
		if preset != "" {
			payload["dig_preset"] = preset
		}
		body, _ := json.Marshal(payload)
		call(hub+"/api/orders/pool/dig", tok, owner, signer, body)
	case "hunt":
		budget := flag.Float64("budget-hmc", 20, "budget")
		title := flag.String("title", "rc20-hunt-e2e", "title")
		target := flag.String("target", "jsmn", "catalog target_id")
		shards := flag.Int("shards", 0, "budget shards (0 = package default)")
		engine := flag.String("engine", "libfuzzer", "hunt_engine")
		pkg := flag.String("package", "hunt_lite", "hunt_lite|hunt_standard|hunt_heavy")
		lease := flag.Int("engine-lease-sec", 0, "override engine lease (0 = server package default)")
		flag.Parse()
		huntPkg := strings.ToLower(strings.TrimSpace(*pkg))
		if huntPkg == "" {
			huntPkg = "hunt_lite"
		}
		payload := map[string]any{
			"title":            *title,
			"package":          huntPkg,
			"catalog":          true,
			"target_id":        strings.TrimSpace(*target),
			"budget_hmc":       *budget,
			"pool_distributed": true,
			"status":           "running",
			"hunt_engine":      strings.TrimSpace(*engine),
			"owner_ref":        owner,
		}
		if *shards > 0 {
			payload["budget_shards"] = *shards
		}
		// Do not force a shallow 60s lease — hub package defaults are deeper.
		if *lease > 0 {
			payload["engine_lease_sec"] = *lease
		}
		body, _ := json.Marshal(payload)
		call(hub+"/api/orders/pool/hunt", tok, owner, signer, body)
	case "cancel":
		id := flag.String("campaign-id", "", "campaign id")
		flag.Parse()
		if strings.TrimSpace(*id) == "" {
			fatal("need -campaign-id")
		}
		body, _ := json.Marshal(map[string]any{"campaign_id": strings.TrimSpace(*id)})
		call(hub+"/api/orders/pool/cancel", tok, owner, signer, body)
	case "prepaid":
		flag.Parse()
		get(hub+"/api/orders/pool/prepaid", tok, owner, signer)
	default:
		fatal("unknown cmd %s", cmd)
	}
}

func ownerProof(signer *nodecrypto.Signer, owner string) (pub, sig string, ts int64) {
	ts = time.Now().Unix()
	msg := []byte(fmt.Sprintf("hackme-orders-pool-owner-v1\n%s\n%d", owner, ts))
	return strings.TrimSpace(signer.PublicKeyHex()), strings.TrimSpace(signer.SignHex(msg)), ts
}

func call(url, tok, owner string, signer *nodecrypto.Signer, body []byte) {
	pub, sig, ts := ownerProof(signer, owner)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fatal("%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	req.Header.Set("X-Hackme-Orders-Owner-Ref", owner)
	req.Header.Set("X-Hackme-Orders-Owner-PubKey", pub)
	req.Header.Set("X-Hackme-Orders-Owner-Sig", sig)
	req.Header.Set("X-Hackme-Orders-Owner-Ts", strconv.FormatInt(ts, 10))
	resp, err := netutil.HTTPClientNoRedirect(120 * time.Second).Do(req)
	if err != nil {
		fatal("%v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	os.Stdout.Write(raw)
	os.Stdout.Write([]byte("\n"))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		os.Exit(1)
	}
}

func get(url, tok, owner string, signer *nodecrypto.Signer) {
	pub, sig, ts := ownerProof(signer, owner)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		fatal("%v", err)
	}
	req.Header.Set("X-Hackme-Admin-Token", tok)
	req.Header.Set("X-Hackme-Orders-Owner-Ref", owner)
	req.Header.Set("X-Hackme-Orders-Owner-PubKey", pub)
	req.Header.Set("X-Hackme-Orders-Owner-Sig", sig)
	req.Header.Set("X-Hackme-Orders-Owner-Ts", strconv.FormatInt(ts, 10))
	resp, err := netutil.HTTPClientNoRedirect(30 * time.Second).Do(req)
	if err != nil {
		fatal("%v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	os.Stdout.Write(raw)
	os.Stdout.Write([]byte("\n"))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		os.Exit(1)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
