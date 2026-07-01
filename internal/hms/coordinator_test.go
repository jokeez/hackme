package hms

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordinatorStorageRoundtrip(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{MinQuotaGB: 10, MaxQuotaGB: 1000, EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)
	if err := coord.RegisterStorageWorker("w1", pubHex, 100); err != nil {
		t.Fatal(err)
	}
	ct := make([]byte, 32)
	for i := range ct {
		ct[i] = byte(i)
	}
	if err := coord.AssignChunk("chunk1", "w1", ct, 512, nil); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 512)
	copy(data, ct)
	if err := coord.writeMarketChunkFile("w1", "chunk1", data); err != nil {
		t.Fatal(err)
	}
	ch, err := coord.IssueChallenge("w1")
	if err != nil {
		t.Fatal(err)
	}
	offset := uint64FromAny(ch["sector_offset"])
	sector := SectorProofFromCiphertext(data, offset)
	p := StorageSubmitPayload{
		WorkerID:    "w1",
		ChallengeID: ch["challenge_id"].(string),
		EpochID:     int64FromAny(ch["epoch_id"]),
		ProofHex:    hex.EncodeToString(sector[:]),
	}
	body, _ := json.Marshal(p)
	sig := ed25519.Sign(priv, body)
	if err := coord.SubmitStorageProof(p, pubHex, hex.EncodeToString(sig), sector[:]); err != nil {
		t.Fatal(err)
	}
	st := coord.PoolStats()
	if st["storage_workers"].(int) != 1 {
		t.Fatalf("stats: %+v", st)
	}
}

func int64FromAny(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case int:
		return int64(x)
	default:
		return 0
	}
}

func uint64FromAny(v any) uint64 {
	switch x := v.(type) {
	case uint64:
		return x
	case float64:
		return uint64(x)
	case int64:
		return uint64(x)
	default:
		return 0
	}
}

func TestOpenDB(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "hms.db")
	db, err := OpenDB(p)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
