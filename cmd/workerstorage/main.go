// HMS storage worker — Proof-of-Storage challenges against hmscoordinator.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	coord := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_HMS_COORDINATOR_URL")), "/")
	if coord == "" {
		coord = "http://127.0.0.1:18082"
	}
	workerID := strings.TrimSpace(os.Getenv("HACKME_WORKER_ID"))
	if workerID == "" {
		workerID = "worker-hms-storage"
	}
	dir := strings.TrimSpace(os.Getenv("HACKME_STORAGE_DIR"))
	if dir == "" {
		dir = filepath.Join("data", "hms-storage", workerID)
	}
	quotaGB, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HACKME_STORAGE_QUOTA_GB")))
	if quotaGB < 50 {
		quotaGB = 200
	}
	token := strings.TrimSpace(os.Getenv("HACKME_HMS_WORKER_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORKER_TOKEN"))
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatal(err)
	}
	pubHex := hex.EncodeToString(pub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := postJSON(coord+"/api/storage/register", token, map[string]any{
		"worker_id": workerID, "pubkey_hex": pubHex, "quota_gb": quotaGB,
	}, nil); err != nil {
		log.Fatalf("register: %v", err)
	}
	log.Printf("storage worker %s registered quota=%dGB dir=%s", workerID, quotaGB, dir)

	// Demo chunk if empty
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		seedChunk(coord, token, workerID, dir, "chunk-demo-1")
	}

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		runChallenge(coord, token, workerID, dir, priv, pubHex)
		<-tick.C
	}
}

func seedChunk(coord, token, workerID, dir, chunkID string) {
	data := make([]byte, 4096)
	_, _ = randRead(data)
	path := filepath.Join(dir, chunkID+".dat")
	_ = os.WriteFile(path, data, 0o600)
	sum := sha256.Sum256(data)
	_ = postJSON(coord+"/api/storage/chunk", token, map[string]any{
		"chunk_id": chunkID, "worker_id": workerID,
		"ciphertext_sha256": hex.EncodeToString(sum[:]),
		"size":              len(data),
	}, nil)
	log.Printf("seeded demo chunk %s", chunkID)
}

func runChallenge(coord, token, workerID, dir string, priv ed25519.PrivateKey, pubHex string) {
	var ch map[string]any
	if err := postJSON(coord+"/api/storage/challenge", token, map[string]any{"worker_id": workerID}, &ch); err != nil {
		log.Printf("challenge: %v", err)
		return
	}
	chunkID, _ := ch["chunk_id"].(string)
	offsetF, _ := ch["sector_offset"].(float64)
	offset := uint64(offsetF)
	path := filepath.Join(dir, chunkID+".dat")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("read chunk: %v", err)
		return
	}
	sector := sectorProof(data, offset)
	payload := map[string]any{
		"worker_id":    workerID,
		"challenge_id": ch["challenge_id"],
		"epoch_id":     ch["epoch_id"],
		"proof_hex":    hex.EncodeToString(sector[:]),
	}
	body, _ := json.Marshal(payload)
	sig := ed25519.Sign(priv, body)
	reqBody := map[string]any{}
	_ = json.Unmarshal(body, &reqBody)
	reqBody["pubkey_hex"] = pubHex
	reqBody["sig_hex"] = hex.EncodeToString(sig)
	if err := postJSON(coord+"/api/storage/submit", token, reqBody, nil); err != nil {
		log.Printf("submit: %v", err)
		return
	}
	log.Printf("proof ok chunk=%s offset=%d", chunkID, offset)
}

func sectorProof(ciphertext []byte, offset uint64) [32]byte {
	start := int(offset)
	if start >= len(ciphertext) {
		start = 0
	}
	end := start + 32
	if end > len(ciphertext) {
		end = len(ciphertext)
	}
	seg := make([]byte, 32)
	copy(seg, ciphertext[start:end])
	return sha256.Sum256(seg)
}

func postJSON(url, token string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func randRead(b []byte) (int, error) {
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Read(b)
}
