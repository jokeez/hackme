// sendtransfer signs transfer_v1 from node seed and POSTs to /api/tx/send.
//
// Usage:
//
//	HACKME_DATA_DIR=logs/desktop/data go run ./cmd/sendtransfer \
//	  -to HMC-381c0c5e2cfcc714 -amount-hmc 1.0 -base https://hackme.tech
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/nodecrypto"
)

func main() {
	to := flag.String("to", "", "recipient HMC address")
	amountHMC := flag.Float64("amount-hmc", 0, "amount in HMC")
	base := flag.String("base", "https://hackme.tech", "chain node base URL")
	dataDir := flag.String("data-dir", "", "directory with node_ed25519.seed")
	memo := flag.String("memo", "transfer", "memo")
	flag.Parse()

	if *to == "" || *amountHMC <= 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *dataDir == "" {
		*dataDir = strings.TrimSpace(os.Getenv("HACKME_DATA_DIR"))
	}
	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "set -data-dir or HACKME_DATA_DIR")
		os.Exit(2)
	}

	signer, err := nodecrypto.LoadOrCreate(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "signer:", err)
		os.Exit(1)
	}
	from := signer.Address()
	nonce, err := fetchNonce(strings.TrimRight(*base, "/"), from)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nonce:", err)
		os.Exit(1)
	}

	tx := chain.TransferTx{
		TxType:        "transfer_v1",
		From:          from,
		To:            strings.TrimSpace(*to),
		AmountUnits:   uint64(*amountHMC * 1e8),
		FeeUnits:      chain.DefaultTransferMinFee,
		Nonce:         nonce,
		TimestampUnix: time.Now().Unix(),
		Memo:          *memo,
		PubKeyEd25519: signer.PublicKeyHex(),
	}
	canon, err := tx.CanonicalBytes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "canonical:", err)
		os.Exit(1)
	}
	tx.SigEd25519 = signer.SignHex(canon)

	raw, _ := json.Marshal(tx)
	url := strings.TrimRight(*base, "/") + "/api/tx/send"
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "http:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("http=%d\n%s\n", resp.StatusCode, string(body))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		os.Exit(1)
	}
}

func fetchNonce(base, addr string) (uint64, error) {
	resp, err := http.Get(base + "/api/address/" + addr)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var st struct {
		NextNonce uint64 `json:"next_nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return 0, err
	}
	return st.NextNonce, nil
}
