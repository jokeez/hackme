package block

import (
	"encoding/json"
	"fmt"
	"time"
)

const PoHBlockKind = "poh_wasm_v1"

// PoHTask describes the WASM eval PoH baked into mined blocks.
// If orderTaskID is non-empty, it is stored in payload for progress tracking on orders.
func PoHTask(nonce, eval, mod uint64, orderTaskID, formula string) Task {
	if formula == "" {
		formula = "eval_v1(n)=n*7+13"
	}
	m := map[string]any{
		"poh":     "wasm_eval",
		"nonce":   nonce,
		"eval":    eval,
		"mod":     mod,
		"formula": formula,
	}
	if orderTaskID != "" {
		m["order_task_id"] = orderTaskID
	}
	payload, _ := json.Marshal(m)
	return Task{
		ID:         "task-poh-" + randomID(),
		Kind:       PoHBlockKind,
		Payload:    payload,
		TargetHint: fmt.Sprintf("eval(n)%%%d==0", mod),
	}
}

// NewPoHBlock builds a mined block (header + hash) after a valid PoH nonce.
// orderTaskID links the block to a row in SQLite tasks when mining a paid order.
func NewPoHBlock(index uint64, prevHash, minerAddress string, nonce, eval, mod uint64, orderTaskID, formula string) *Block {
	b := &Block{
		Index:        index,
		Timestamp:    time.Now().Unix(),
		PrevHash:     prevHash,
		Nonce:        nonce,
		MinerAddress: minerAddress,
		Task:         PoHTask(nonce, eval, mod, orderTaskID, formula),
	}
	b.SetHash()
	return b
}
