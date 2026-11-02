package poolfuzz

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

const (
	defaultSeedCorpusWireMaxItems = 48
	defaultSeedCorpusWireMaxBytes = 256 << 10 // 256 KiB JSON-ish payload budget
)

// seedCorpusWireCaps returns max items / total decoded bytes for claim JSON.
// Override: HACKME_POOL_SEED_CORPUS_MAX_ITEMS, HACKME_POOL_SEED_CORPUS_MAX_BYTES.
func seedCorpusWireCaps() (maxItems, maxBytes int) {
	maxItems = defaultSeedCorpusWireMaxItems
	maxBytes = defaultSeedCorpusWireMaxBytes
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_SEED_CORPUS_MAX_ITEMS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 512 {
			maxItems = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_SEED_CORPUS_MAX_BYTES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 64 && n <= 4<<20 {
			maxBytes = n
		}
	}
	return maxItems, maxBytes
}

// applySeedByteCorpusWireTrim mutates cfg["seed_byte_corpus"] in place so Dig
// claim and submit/replay use the same capped list (byteAnchorBase parity).
func applySeedByteCorpusWireTrim(cfg map[string]any) {
	if cfg == nil {
		return
	}
	raw, ok := cfg["seed_byte_corpus"]
	if !ok || raw == nil {
		return
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return
	}
	cfg["seed_byte_corpus"] = TrimSeedByteCorpusForWire(list)
}

// TrimSeedByteCorpusForWire caps seed_byte_corpus for claim/replay wire parity.
// Call after guided L2 merge on both claim and async replay so workers and
// verifiers see the same truncated list (order preserved, first-fit).
func TrimSeedByteCorpusForWire(list []any) []any {
	if len(list) == 0 {
		return list
	}
	maxItems, maxBytes := seedCorpusWireCaps()
	out := make([]any, 0, min(len(list), maxItems))
	total := 0
	for _, item := range list {
		if len(out) >= maxItems {
			break
		}
		n := seedCorpusItemWireBytes(item)
		if n <= 0 {
			continue
		}
		if total+n > maxBytes && len(out) > 0 {
			break
		}
		if n > maxBytes && len(out) == 0 {
			// Single oversized seed: keep it only if under 1MiB hard ceiling
			// already enforced elsewhere; otherwise skip.
			if n > 1<<20 {
				continue
			}
			out = append(out, item)
			break
		}
		out = append(out, item)
		total += n
	}
	return out
}

func seedCorpusItemWireBytes(item any) int {
	switch x := item.(type) {
	case []byte:
		return len(x)
	case string:
		return len(x)
	default:
		b, err := json.Marshal(item)
		if err != nil {
			return 0
		}
		return len(b)
	}
}
