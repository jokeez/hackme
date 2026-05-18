package block

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

const (
	// GenesisRewardHMC is minted once at InitGenesis to DevFeeAddress (treasury), not to the node primary wallet.
	GenesisRewardHMC = 50_000.0
	GenesisKind      = "genesis_v1"
	ChainID          = "hackme-dev-mainnet"
)

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// GenesisTask builds the first-block challenge description.
func GenesisTask() Task {
	return Task{
		ID:         "task-genesis-" + randomID(),
		Kind:       GenesisKind,
		Payload:    []byte(`{"message":"HackMe genesis — WASM lock: find nonce where eval(nonce) mod M == 0 (M from meta poh_target_mod after genesis)"}`),
		TargetHint: "eval(nonce)%M==0",
	}
}

// NewGenesisBlock returns block #0 with PrevHash = ZeroPrevHash and Hash unset until SetHash is called.
func NewGenesisBlock(minerAddress string) *Block {
	b := &Block{
		Index:        0,
		Timestamp:    time.Now().Unix(),
		PrevHash:     ZeroPrevHash,
		Nonce:        0,
		MinerAddress: minerAddress,
		Task:         GenesisTask(),
	}
	b.SetHash()
	return b
}
