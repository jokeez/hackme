package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// mintIdempotencyMetaKey binds asset+to+amount+memo.
// Empty memo or prefix yields "" — callers must reject empty memo (never disable dedup).
func mintIdempotencyMetaKey(prefix, to string, amountUnits uint64, memo string) string {
	memo = strings.TrimSpace(memo)
	if memo == "" || strings.TrimSpace(prefix) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%d\n%s", prefix, strings.TrimSpace(to), amountUnits, memo)))
	return prefix + hex.EncodeToString(sum[:16])
}
