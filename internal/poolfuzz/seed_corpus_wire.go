package poolfuzz

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"hackme/internal/fuzzengine"
)

const (
	// Matches hunt.rankedLibFuzzerSeedCap. First-fit keeps the head, so freshly
	// accepted coverage units are placed ahead of generic boot starters.
	defaultSeedCorpusWireMaxItems = 96
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

// genericBootSeed is a libFuzzer starter ("{}", "[]", "null", or a one-byte
// 0x00/0x80). Those must not occupy claim slots ahead of a unit a miner just found.
func genericBootSeed(b []byte) bool {
	switch string(b) {
	case "{}", "[]", "null":
		return true
	}
	return len(b) == 1 && (b[0] == 0x00 || b[0] == 0x80)
}

// claimSeedCorpusNeedsL2Merge is true only when the campaign config has no
// coverage unit yet. Delta accept already pinned fresh units at the front of
// seed_byte_corpus (96 items / 256KiB). Re-reading the L2 directory on every
// claim and replay re-hashes that cache on the request path. Empty or
// boot-only corpora still merge once so the first claim is not seedless.
func claimSeedCorpusNeedsL2Merge(cfg map[string]any) bool {
	for _, b := range fuzzengine.ParseByteCorpus(cfg) {
		if len(b) > 0 && !genericBootSeed(b) {
			return false
		}
	}
	return true
}

// capAcceptedSeedBytes bounds a delta's fresh units to the claim wire budget
// before they are hex-copied into campaign JSON.
func capAcceptedSeedBytes(in [][]byte) [][]byte {
	maxItems, maxBytes := seedCorpusWireCaps()
	out := make([][]byte, 0, min(len(in), maxItems))
	total := 0
	for _, b := range in {
		if len(b) == 0 {
			continue
		}
		if len(out) >= maxItems {
			break
		}
		if total+len(b) > maxBytes && len(out) > 0 {
			break
		}
		if len(b) > maxBytes && len(out) == 0 {
			if len(b) > 1<<20 {
				continue
			}
			out = append(out, b)
			break
		}
		out = append(out, b)
		total += len(b)
	}
	return out
}

func seedItemBytes(item any) ([]byte, bool) {
	parsed := fuzzengine.ParseByteCorpus(map[string]any{"seed_byte_corpus": []any{item}})
	if len(parsed) == 0 || len(parsed[0]) == 0 {
		return nil, false
	}
	return parsed[0], true
}

// MergeAcceptedSeedByteCorpus places just-accepted coverage units ahead of the
// stored corpus and demotes generic boot starters, then applies the claim wire
// cap. First-fit trim keeps this head, so a new unit is not evicted by boot
// junk that already filled the 96-item / 256KiB budget.
func MergeAcceptedSeedByteCorpus(accepted [][]byte, existing []any) []any {
	seen := map[string]struct{}{}
	var head, mid, tail []any
	add := func(dst *[]any, item any, b []byte) {
		if len(b) == 0 {
			return
		}
		key := hex.EncodeToString(b)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		*dst = append(*dst, item)
	}
	for _, b := range accepted {
		if len(b) == 0 || genericBootSeed(b) {
			continue
		}
		add(&head, hex.EncodeToString(b), b)
	}
	maxItems, _ := seedCorpusWireCaps()
	examined := 0
	for _, item := range existing {
		// First-fit trim keeps head+mid+tail up to maxItems. Stop once that
		// prefix is full, and cap how many legacy entries we decode per request.
		// Tail boots sit after real units. Counting them toward the cap would
		// stop before mid filled and let a "{}" survive first-fit trim.
		if len(head)+len(mid) >= maxItems || examined >= maxItems*8 {
			break
		}
		examined++
		b, ok := seedItemBytes(item)
		if !ok {
			// Opaque entry: keep it with the non-boot body so a trim cannot
			// silently delete a seed we failed to decode.
			key := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(fmtItem(item), "0x")))
			if key == "" {
				continue
			}
			if _, dup := seen["raw:"+key]; dup {
				continue
			}
			seen["raw:"+key] = struct{}{}
			mid = append(mid, item)
			continue
		}
		if genericBootSeed(b) {
			add(&tail, item, b)
			continue
		}
		add(&mid, item, b)
	}
	merged := make([]any, 0, len(head)+len(mid)+len(tail))
	merged = append(merged, head...)
	merged = append(merged, mid...)
	merged = append(merged, tail...)
	return TrimSeedByteCorpusForWire(merged)
}

func fmtItem(item any) string {
	switch x := item.(type) {
	case string:
		return x
	case []byte:
		return hex.EncodeToString(x)
	default:
		b, err := json.Marshal(item)
		if err != nil {
			return ""
		}
		return string(b)
	}
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
