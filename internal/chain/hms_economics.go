package chain

import (
	"fmt"
	"strings"
)

const (
	// MaxSupplyHMS is the hard cap for lane-settlement mint (21M, same unit scale as HMC/SUP).
	MaxSupplyHMS = 21_000_000.0
	// HMSNetworkFeeBurnShare is the burned share of transfer fee_units on the HMS ledger.
	HMSNetworkFeeBurnShare = NetworkFeeBurnShare
	// HMSNetworkFeeTreasuryShare is credited to the HMS treasury address (not HMC DevFeeAddress).
	HMSNetworkFeeTreasuryShare = NetworkFeeDevShare
	// HMSTreasuryGenesisFloatPct is minted to treasury at genesis (ops float, not counted as miner emission).
	HMSTreasuryGenesisFloatPct = 0.005
)

const metaHMSTreasuryAddress = "hms_treasury_address"

func validateHMSTreasuryAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	// Accept legacy (16 hex) and long (64 hex) so genesis stays ready past Economics v2.
	if !IsValidAddress(addr) {
		return fmt.Errorf("hms treasury address must be HMC- + 16 or 64 lowercase hex")
	}
	return nil
}
