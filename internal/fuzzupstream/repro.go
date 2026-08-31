package fuzzupstream

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// ReproCmdHuntNative returns a copy-paste repro for ASAN inventory/catalog harnesses.
func ReproCmdHuntNative(input []byte) string {
	hexIn := hex.EncodeToString(input)
	return fmt.Sprintf(
		"printf '%%s' '%s' | xxd -r -p > crash.bin && ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 ./harness.bin < crash.bin",
		hexIn,
	)
}

// ReproCmdHuntNativeWithPath uses an explicit harness path in the repro line.
func ReproCmdHuntNativeWithPath(harnessPath string, input []byte) string {
	hp := strings.TrimSpace(harnessPath)
	if hp == "" {
		return ReproCmdHuntNative(input)
	}
	hexIn := hex.EncodeToString(input)
	return fmt.Sprintf(
		"printf '%%s' '%s' | xxd -r -p > crash.bin && ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 %s < crash.bin",
		hexIn, hp,
	)
}
