package fuzznative

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// ReproStatus is the native bridge outcome for a WASM finding.
type ReproStatus string

const (
	StatusPending   ReproStatus = "pending"
	StatusRunning   ReproStatus = "running"
	StatusConfirmed ReproStatus = "confirmed"
	StatusRejected  ReproStatus = "rejected"
	StatusSkipped   ReproStatus = "skipped"
)

// ReproResult is returned by the native bridge evaluator.
type ReproResult struct {
	Status         ReproStatus `json:"status"`
	UpstreamTarget string      `json:"upstream_target"`
	UpstreamCommit string      `json:"upstream_commit,omitempty"`
	Harness        string      `json:"harness,omitempty"`
	GuardSignal    bool        `json:"guard_signal"`
	NativeSignal   bool        `json:"native_signal"`
	Note           string      `json:"note"`
	InputHex       string      `json:"input_hex"`
}

// EvalRepro runs pinned upstream guard logic in-process (Go port) to confirm WASM signals.
func EvalRepro(upstreamTarget, guardName string, input []byte, pins *PinManifest) ReproResult {
	target := ResolveTarget(upstreamTarget, guardName)
	inHex := hex.EncodeToString(input)
	res := ReproResult{
		Status:         StatusSkipped,
		UpstreamTarget: target,
		InputHex:       inHex,
		Note:           "no upstream target",
	}
	if pins != nil && target != "" {
		if repo, ok := pins.Repos[target]; ok {
			res.UpstreamCommit = repo.Commit
			res.Harness = repo.FuzzHarness
		}
	}
	if target == "" {
		return res
	}
	guard := strings.TrimSpace(strings.ToLower(guardName))
	nativeHit := false
	note := ""
	switch {
	case strings.Contains(guard, "dup_input") || strings.Contains(guard, "tx_dup"):
		nativeHit = evalBitcoinDupInputs(input) != 0
	case strings.Contains(guard, "bip34") || strings.Contains(guard, "coinbase"):
		nativeHit = evalBitcoinBIP34Height(input) != 0
	case strings.Contains(guard, "block_weight") || strings.Contains(guard, "weight"):
		nativeHit = evalBitcoinBlockWeight(input) != 0
	case strings.Contains(guard, "getscriptop"):
		nativeHit = evalBitcoinGetScriptOp(input) != 0
	case strings.Contains(guard, "hasvalidops"):
		nativeHit = evalBitcoinHasValidOps(input) != 0
	case strings.Contains(guard, "evalscript_push") || strings.Contains(guard, "push"):
		nativeHit = evalBitcoinEvalScriptPush(input) != 0
	case strings.Contains(guard, "witness_stack") || strings.Contains(guard, "witness"):
		nativeHit = evalBitcoinWitnessStack(input) != 0
	case strings.Contains(guard, "evalscript_stack") || strings.Contains(guard, "stack"):
		nativeHit = evalBitcoinEvalScriptStack(input) != 0
	case strings.Contains(guard, "opcount") || strings.Contains(guard, "op_count"):
		nativeHit = evalBitcoinEvalScriptOpCount(input) != 0
	case strings.Contains(guard, "tx_check") || strings.Contains(guard, "moneyrange"):
		nativeHit = evalBitcoinTxCheckMoneyRange(input) != 0
	default:
		nativeHit = evalBitcoinDupInputs(input) != 0
		note = "generic dup-input native port"
	}
	res.NativeSignal = nativeHit
	res.GuardSignal = nativeHit
	if nativeHit {
		res.Status = StatusConfirmed
		if note == "" {
			res.Note = fmt.Sprintf("native guard confirmed on pinned %s (%s)", target, guardName)
		} else {
			res.Note = note
		}
	} else {
		res.Status = StatusRejected
		if note == "" {
			res.Note = fmt.Sprintf("wasm signal not reproduced on native %s port (%s)", target, guardName)
		} else {
			res.Note = note
		}
	}
	return res
}

// evalBitcoinDupInputs mirrors tasks/sources/security/upstream/bitcoin_tx_dup_inputs.c
func evalBitcoinDupInputs(input []byte) int {
	keys := make([]byte, 8)
	for i := 0; i < 8; i++ {
		if i < len(input) {
			keys[i] = input[i]
		}
	}
	for i := 0; i < 8; i++ {
		if keys[i] == 0 {
			return 1
		}
		for j := 0; j < i; j++ {
			if keys[i] == keys[j] {
				return 1
			}
		}
	}
	return 0
}

func evalBitcoinBIP34Height(input []byte) int {
	if len(input) < 4 {
		return 1
	}
	height := uint32(input[0]) | uint32(input[1])<<8 | uint32(input[2])<<16 | uint32(input[3])<<24
	if height == 0 || height > 1_000_000 {
		return 1
	}
	return 0
}

func evalBitcoinBlockWeight(input []byte) int {
	if len(input) < 4 {
		return 0
	}
	w := uint32(input[0]) | uint32(input[1])<<8 | uint32(input[2])<<16 | uint32(input[3])<<24
	const maxWU = 4_000_000
	if w > maxWU {
		return 1
	}
	return 0
}
