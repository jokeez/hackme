package main

import (
	"os"
	"strconv"
	"strings"
)

// Pseudo-share target: shareMod ≈ max(M/divisor, batch/targetSharesPerLease).
// A share is a nonce in the lease where PoH work % shareMod == 0.
//
// Soft mode (!SHARE_STRICT): attempt-pay; shares are stats/rate only.
// Strict mode (Blake3 default): PPS on verified shares; GH/s and pool M from
// shares + lease wall — never from client-reported hashrate_gh_s.

const (
	defaultShareDivisor = uint64(1024)
	maxSharesPerSubmit  = 128
	// targetSharesPerLease: lease-scaled share difficulty so a full honest lease
	// yields ~this many shares (fair vs RTT-bound tiny leases that always hit the cap).
	targetSharesPerLease = uint64(32)
	// shareHarvestScanCap: soft-mode expectedShares window.
	shareHarvestScanCap = uint64(256 * 1024)
	// shareHashBudget: max PoH hashes a worker spends stride-harvesting shares per submit.
	shareHashBudget = uint64(4 << 20)
	// defaultShareRateMin: after enough expected shares, accepted/expected below this
	// flags share_rate_low.
	defaultShareRateMin         = 0.30
	defaultShareRateMinExpected = 8.0
)

// softShareScanAttempts caps expected-share window to the worker harvest budget.
func softShareScanAttempts(attempts uint64, shareStrict bool) uint64 {
	capN := shareHarvestScanCap
	if shareStrict {
		capN = shareHashBudget
	}
	if attempts <= capN {
		return attempts
	}
	return capN
}

func envShareDivisor() uint64 {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_DIVISOR"))
	if v == "" {
		return defaultShareDivisor
	}
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
		return 0
	}
	x, err := strconv.ParseUint(v, 10, 64)
	if err != nil || x < 2 {
		return defaultShareDivisor
	}
	if x > 1_000_000 {
		return 1_000_000
	}
	return x
}

func envShareStrict() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_STRICT"))
	if v == "" {
		// Blake3 era default: PPS on verified shares (set =0 to keep soft attempt-pay).
		return true
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func envShareRateMin() float64 {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_RATE_MIN"))
	if v == "" {
		return defaultShareRateMin
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil || x < 0 {
		return defaultShareRateMin
	}
	if x > 1 {
		return 1
	}
	return x
}

func envShareRateMinExpected() float64 {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_RATE_MIN_EXPECTED"))
	if v == "" {
		return defaultShareRateMinExpected
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil || x < 1 {
		return defaultShareRateMinExpected
	}
	return x
}

func envShareRateReject() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_RATE_REJECT"))
	if v == "" {
		return false
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func shareModFor(targetMod, divisor uint64) uint64 {
	if divisor < 2 || targetMod == 0 {
		return 0
	}
	sm := targetMod / divisor
	if sm < 1 {
		sm = 1
	}
	return sm
}

// shareModForLease hardens share difficulty on large Blake3 leases so expected
// shares stay near targetSharesPerLease (not maxSharesPerSubmit on every submit).
// When pool M is solvency-scale (Hs×block_sec), M/divisor can exceed the whole
// lease — that would starve PPS. Cap so a full lease still expects ≥1 share.
func shareModForLease(targetMod, divisor, batch uint64) uint64 {
	sm := shareModFor(targetMod, divisor)
	if batch == 0 {
		return sm
	}
	leaseSM := batch / targetSharesPerLease
	if leaseSM < 1 {
		leaseSM = 1
	}
	if sm == 0 || leaseSM > sm {
		return leaseSM
	}
	// Starve guard: shareMod ≥ batch ⇒ expected shares < 1 for an honest full lease.
	if sm >= batch {
		return leaseSM
	}
	return sm
}

// expectedShares returns attempts * divisor / M (Poisson mean when shareMod = M/div).
func expectedShares(attempts, targetMod, divisor uint64) float64 {
	if attempts == 0 || targetMod == 0 || divisor < 2 {
		return 0
	}
	return float64(attempts) * float64(divisor) / float64(targetMod)
}

// expectedSharesForMod is attempts / shareMod (works for lease-scaled share mods).
func expectedSharesForMod(attempts, shareMod uint64) float64 {
	if attempts == 0 || shareMod == 0 {
		return 0
	}
	return float64(attempts) / float64(shareMod)
}

// shareRejectKind counts why submitted share_nonces failed validation.
type shareRejectKind struct {
	Dup        int
	OutOfRange int
	BadPoH     int
}

func (s shareRejectKind) Total() int { return s.Dup + s.OutOfRange + s.BadPoH }

// validateShareNonces checks each nonce is in [base, base+batch) and satisfies
// PoH work % shareMod == 0. Dedupes, caps at maxSharesPerSubmit.
func validateShareNonces(shares []uint64, base, batch, shareMod uint64, formulaVersion int, prevHash, minerAddress string) (accepted []uint64, rej shareRejectKind) {
	if shareMod == 0 || batch == 0 || len(shares) == 0 {
		return nil, shareRejectKind{}
	}
	seen := make(map[uint64]struct{}, len(shares))
	end := base + batch
	if end < base {
		end = ^uint64(0)
	}
	for _, n := range shares {
		if len(accepted) >= maxSharesPerSubmit {
			// Drop extras without counting them as PoH rejects (cap is not a bad share).
			break
		}
		if _, ok := seen[n]; ok {
			rej.Dup++
			continue
		}
		seen[n] = struct{}{}
		if n < base || n >= end {
			rej.OutOfRange++
			continue
		}
		if !validFoundNoncePoH(n, shareMod, formulaVersion, prevHash, minerAddress) {
			rej.BadPoH++
			continue
		}
		accepted = append(accepted, n)
	}
	return accepted, rej
}

func rewardPerShare(baseRewardHMC float64, divisor uint64) float64 {
	if divisor < 2 || baseRewardHMC <= 0 {
		return 0
	}
	return baseRewardHMC / float64(divisor)
}

// rewardPerShareAt pays baseReward * shareMod / M (equals rewardPerShare when shareMod = M/div).
func rewardPerShareAt(baseRewardHMC float64, shareMod, targetMod uint64) float64 {
	if shareMod == 0 || targetMod == 0 || baseRewardHMC <= 0 {
		return 0
	}
	return baseRewardHMC * float64(shareMod) / float64(targetMod)
}

// shareImpliedGHS estimates device GH/s from accepted shares and claim→submit wall.
// Very short walls (<50ms) are RTT noise; 50–500ms still credits GH so fast GPUs
// can grow adaptive leases past bootstrap (finding: stuck at 512M).
func shareImpliedGHS(nShares int, shareMod uint64, wallSec float64) float64 {
	if nShares <= 0 || shareMod == 0 || wallSec < 0.05 {
		return 0
	}
	// One share on a multi-GB shareMod is Poisson noise (old CPU 4M stride harvest).
	// Ignore for pool M / GH until we have a stable multi-share sample.
	if nShares < 2 && shareMod >= (1<<28) {
		return 0
	}
	hashes := float64(nShares) * float64(shareMod)
	return clampWorkerHashrateGHS(hashes / wallSec / 1e9)
}

// warmLowShareImpliedGHS spreads Blake3 SHARE_STRICT micro-samples into a visible
// board band without flattening everyone onto 0.1. Preserves rank order of raw
// share/wall and lifts multi-share submits. Only touches raw ∈ (0, 0.05); real
// GPU rates pass through. Cap 2 GH/s.
func warmLowShareImpliedGHS(raw float64, nShares int) float64 {
	if nShares < 1 || raw <= 0 || raw >= 0.05 {
		return raw
	}
	// raw 0.001→~0.14, 0.01→~0.55, 0.04→~1.65; +shares stretches further.
	bumped := 0.08 + raw*38
	if nShares > 1 {
		extra := nShares - 1
		if extra > 8 {
			extra = 8
		}
		bumped += 0.07 * float64(extra)
	}
	if bumped < 0.1 {
		bumped = 0.1
	}
	if bumped > 2 {
		bumped = 2
	}
	return clampWorkerHashrateGHS(bumped)
}

// pacedBoardGHS prefers a client-declared pace in [0.1, 2] when shares already
// proved work (diverse board targets), else the warmed share-implied sample.
// Declared values outside the band are ignored so FORCE spam cannot mint GH.
func pacedBoardGHS(implied float64, reported float64, nShares int) float64 {
	warmed := warmLowShareImpliedGHS(implied, nShares)
	if nShares < 1 {
		return warmed
	}
	if reported < 0.1 || reported > 2 {
		return warmed
	}
	// Declared pace wins for board diversity; never below 85% of warmed floor
	// so a weak harvest cannot advertise a full 2 GH/s.
	floor := warmed * 0.85
	if floor < 0.1 {
		floor = 0.1
	}
	out := reported
	if out < floor {
		out = floor
	}
	if out > 2 {
		out = 2
	}
	return clampWorkerHashrateGHS(out)
}

// honestWallHashrateGHS converts lease wall into GH/s without crediting a full
// early-found batch as if the GPU burned every nonce (64G / 2s → fantasy 30 GH/s).
// Credited hashes are capped to what prevGH (or a mid-GPU bootstrap) could do.
func honestWallHashrateGHS(batch uint64, wallSec, prevGH float64) float64 {
	if batch == 0 || wallSec < 0.05 {
		return 0
	}
	w := wallSec
	if w < 0.001 {
		w = 0.001
	}
	ceiling := prevGH
	if ceiling < 0.5 {
		ceiling = 12 // single mid-class GPU bootstrap until share/wall samples land
	}
	if ceiling > maxWorkerHashrateGHS {
		ceiling = maxWorkerHashrateGHS
	}
	maxHashes := ceiling * 1e9 * w * 1.25
	credited := float64(batch)
	if credited > maxHashes {
		credited = maxHashes
	}
	return clampWorkerHashrateGHS(credited / w / 1e9)
}
