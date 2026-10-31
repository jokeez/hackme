// Command checkpoint creates and verifies external chain checkpoints.
//
//	checkpoint create -db /opt/hackme/data/hackme.db -data-dir /opt/hackme/data -dir ./checkpoints
//	checkpoint verify -dir ./checkpoints [-api https://hackme.tech/pool/api] [-signer HMC-…]
//
// create opens the chain DB read-only (never migrates), reads tip + full state in one read
// transaction, links to the newest checkpoint in -dir and signs with the node key.
// verify needs no secrets: anyone can check signatures, the prev links and, with -api,
// that each checkpointed block hash is still the block served at that height.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"hackme/internal/checkpoint"
	"hackme/internal/nodecrypto"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "create":
		err = runCreate(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "checkpoint:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: checkpoint create|verify [flags] (see -h)")
	os.Exit(2)
}

func fileName(height uint64) string { return fmt.Sprintf("%010d.json", height) }

func loadDir(dir string) ([]checkpoint.Checkpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []checkpoint.Checkpoint
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var c checkpoint.Checkpoint
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if e.Name() != fileName(c.Height) {
			return nil, fmt.Errorf("%s: file name does not match height %d", e.Name(), c.Height)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Height < out[j].Height })
	return out, nil
}

func runCreate(args []string) error {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	dbPath := fs.String("db", "/opt/hackme/data/hackme.db", "chain SQLite DB (opened read-only)")
	dataDir := fs.String("data-dir", "/opt/hackme/data", "node data dir holding node_ed25519.seed")
	dir := fs.String("dir", "checkpoints", "checkpoint directory (git repo subdir)")
	minBlocks := fs.Uint64("min-blocks", 1, "refuse unless tip advanced at least this many blocks since the last checkpoint")
	_ = fs.Parse(args)

	signer, err := nodecrypto.Load(*dataDir)
	if err != nil {
		return fmt.Errorf("load node key: %w", err)
	}
	existing, err := loadDir(*dir)
	if err != nil {
		return err
	}
	var prev *checkpoint.Checkpoint
	if len(existing) > 0 {
		if err := checkpoint.VerifyChain(existing); err != nil {
			return fmt.Errorf("existing checkpoints do not verify, refusing to extend: %w", err)
		}
		prev = &existing[len(existing)-1]
		if prev.SignerPubKey != signer.PublicKeyHex() {
			return fmt.Errorf("node key %s differs from checkpoint signer %s", signer.Address(), prev.SignerAddress)
		}
	}

	dsn := "file:" + *dbPath + "?mode=ro&_pragma=busy_timeout(10000)&_pragma=query_only(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	snap, err := checkpoint.ReadSnapshot(ctx, db)
	if err != nil {
		return err
	}
	if prev != nil && snap.Height < prev.Height+*minBlocks {
		return fmt.Errorf("tip %d has not advanced %d block(s) past checkpoint %d (chain stalled?)", snap.Height, *minBlocks, prev.Height)
	}
	c, err := checkpoint.New(snap, prev, time.Now().Unix(), signer)
	if err != nil {
		return err
	}
	if err := c.VerifySignature(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	final := filepath.Join(*dir, fileName(c.Height))
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	fmt.Println(final)
	return nil
}

type blockLookup struct {
	Found  bool `json:"found"`
	Blocks []struct {
		Index uint64 `json:"index"`
		Hash  string `json:"hash"`
	} `json:"blocks"`
}

func fetchBlock(ctx context.Context, client *http.Client, api string, height uint64) (hash string, err error) {
	u := strings.TrimRight(api, "/") + "/reports/blocks?q=" + url.QueryEscape(fmt.Sprint(height))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %d", u, res.StatusCode)
	}
	var lk blockLookup
	if err := json.Unmarshal(body, &lk); err != nil {
		return "", fmt.Errorf("GET %s: %w", u, err)
	}
	if !lk.Found || len(lk.Blocks) == 0 || lk.Blocks[0].Index != height {
		return "", fmt.Errorf("block %d not served by %s", height, api)
	}
	return lk.Blocks[0].Hash, nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dir := fs.String("dir", "checkpoints", "checkpoint directory")
	api := fs.String("api", "", "optional public chain API base (e.g. https://hackme.tech/pool/api) to compare block hashes")
	signerAddr := fs.String("signer", "", "optional expected signer address (HMC-…)")
	last := fs.Int("last", 0, "with -api, only compare the newest N checkpoints (0 = all)")
	_ = fs.Parse(args)

	cps, err := loadDir(*dir)
	if err != nil {
		return err
	}
	if len(cps) == 0 {
		return fmt.Errorf("no checkpoints in %s", *dir)
	}
	if err := checkpoint.VerifyChain(cps); err != nil {
		return err
	}
	if *signerAddr != "" && cps[0].SignerAddress != *signerAddr {
		return fmt.Errorf("signer %s, expected %s", cps[0].SignerAddress, *signerAddr)
	}
	fmt.Printf("ok: %d checkpoints, heights %d..%d, signer %s, chain %s\n",
		len(cps), cps[0].Height, cps[len(cps)-1].Height, cps[0].SignerAddress, cps[0].ChainID)
	if *api == "" {
		return nil
	}
	start := 0
	if *last > 0 && *last < len(cps) {
		start = len(cps) - *last
	}
	client := &http.Client{Timeout: 20 * time.Second}
	ctx := context.Background()
	for _, c := range cps[start:] {
		// Compare block hash only. Under D1 baseline relay the header miner_address is
		// the pool worker that found the empty block; the authority (checkpoint signer)
		// attests tip+ledger state and need not equal miner_address.
		hash, err := fetchBlock(ctx, client, *api, c.Height)
		if err != nil {
			return err
		}
		if !strings.EqualFold(hash, c.BlockHash) {
			return fmt.Errorf("HISTORY CHANGED at height %d: checkpoint %s, API now serves %s", c.Height, c.BlockHash, hash)
		}
	}
	fmt.Printf("ok: %d block hash(es) match %s\n", len(cps)-start, *api)
	return nil
}
