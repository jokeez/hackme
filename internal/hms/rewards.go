package hms

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// Seal reward kernel — frozen at compile time (lane upgrade requires new policy_hash).
const (
	HMSUnitsPerCoin = 100_000_000

	// SealEpochBaseBudgetHMS is minted per sealed epoch before prepaid scaling.
	SealEpochBaseBudgetHMS = 0.01
	// SealBudgetPrepaidShareRate maps summed epoch client prepaid HMC into extra HMS seal budget.
	SealBudgetPrepaidShareRate = 0.01

	SealWinnerShareRate        = 0.75
	SealParticipationShareRate = 0.25

	// SealParticipationMinUnits documents the UX floor target for dashboards (proportional pool enforces fairness).
	SealParticipationMinUnits = 1000 // 0.00001 HMS
)

// SealPayoutLine is one worker's accrual for a sealed epoch.
type SealPayoutLine struct {
	WorkerID           string `json:"worker_id"`
	WinnerUnits        uint64 `json:"winner_units"`
	ParticipationUnits uint64 `json:"participation_units"`
	TotalUnits         uint64 `json:"total_units"`
	SharesOK           uint64 `json:"shares_ok"`
}

// SealEpochBudgetUnits returns HMS units for a sealed epoch budget.
func SealEpochBudgetUnits(prepaidHMCSum float64) uint64 {
	base := hmsToUnits(SealEpochBaseBudgetHMS)
	if prepaidHMCSum <= 0 {
		return base
	}
	bonus := hmsToUnits(prepaidHMCSum * SealBudgetPrepaidShareRate)
	return base + bonus
}

func hmsToUnits(hms float64) uint64 {
	if hms <= 0 {
		return 0
	}
	u := math.Round(hms * HMSUnitsPerCoin)
	if u < 0 {
		return 0
	}
	return uint64(u)
}

// ComputeSealEpochPayouts splits budget: 75% winner + 25% proportional to shares_ok.
// Winner also receives their share of the participation pool.
// If no shares are recorded, the winner receives the full budget.
func ComputeSealEpochPayouts(budgetUnits uint64, winnerID string, sharesOK map[string]uint64) ([]SealPayoutLine, error) {
	winnerID = trimWorkerID(winnerID)
	if budgetUnits == 0 {
		return nil, fmt.Errorf("seal budget must be > 0")
	}
	if winnerID == "" {
		return nil, fmt.Errorf("seal winner required")
	}
	if len(sharesOK) == 0 {
		return []SealPayoutLine{{
			WorkerID:    winnerID,
			WinnerUnits: budgetUnits,
			TotalUnits:  budgetUnits,
		}}, nil
	}

	var totalShares uint64
	for wid, n := range sharesOK {
		if trimWorkerID(wid) == "" {
			continue
		}
		totalShares += n
	}
	if totalShares == 0 {
		return []SealPayoutLine{{
			WorkerID:    winnerID,
			WinnerUnits: budgetUnits,
			TotalUnits:  budgetUnits,
		}}, nil
	}

	winnerUnits := uint64(math.Floor(float64(budgetUnits) * SealWinnerShareRate))
	partPool := budgetUnits - winnerUnits
	if partPool == 0 && winnerUnits < budgetUnits {
		partPool = budgetUnits - winnerUnits
	}

	byWorker := map[string]*SealPayoutLine{}
	ensure := func(wid string) *SealPayoutLine {
		if byWorker[wid] == nil {
			byWorker[wid] = &SealPayoutLine{WorkerID: wid}
		}
		return byWorker[wid]
	}
	w := ensure(winnerID)
	w.WinnerUnits = winnerUnits

	// Proportional participation pool with largest-remainder fixup.
	type sharePart struct {
		worker string
		raw    float64
		floor  uint64
		frac   float64
	}
	parts := make([]sharePart, 0, len(sharesOK))
	var assigned uint64
	for wid, n := range sharesOK {
		wid = trimWorkerID(wid)
		if wid == "" || n == 0 {
			continue
		}
		raw := float64(partPool) * float64(n) / float64(totalShares)
		floor := uint64(math.Floor(raw))
		parts = append(parts, sharePart{worker: wid, raw: raw, floor: floor, frac: raw - float64(floor)})
		assigned += floor
	}
	// Distribute remainder by fractional parts (deterministic: sort by worker id).
	for assigned < partPool && len(parts) > 0 {
		bestIdx := 0
		for i := 1; i < len(parts); i++ {
			if parts[i].frac > parts[bestIdx].frac {
				bestIdx = i
			} else if parts[i].frac == parts[bestIdx].frac && parts[i].worker < parts[bestIdx].worker {
				bestIdx = i
			}
		}
		parts[bestIdx].floor++
		parts[bestIdx].frac = 0
		assigned++
	}

	for _, p := range parts {
		line := ensure(p.worker)
		line.ParticipationUnits = p.floor
		line.SharesOK = sharesOK[p.worker]
	}

	out := make([]SealPayoutLine, 0, len(byWorker))
	var sum uint64
	for _, line := range byWorker {
		line.TotalUnits = line.WinnerUnits + line.ParticipationUnits
		sum += line.TotalUnits
		out = append(out, *line)
	}
	if sum != budgetUnits {
		return nil, fmt.Errorf("seal payout drift: sum=%d budget=%d", sum, budgetUnits)
	}
	return out, nil
}

func trimWorkerID(wid string) string {
	return strings.TrimSpace(wid)
}

// SealRewardPolicyHash fingerprints the reward kernel for dashboards and tests.
func SealRewardPolicyHash() string {
	wire := fmt.Sprintf("base_hms=%.9f;prepaid_rate=%.9f;winner=%.9f;part=%.9f;min_part_units=%d",
		SealEpochBaseBudgetHMS, SealBudgetPrepaidShareRate, SealWinnerShareRate, SealParticipationShareRate, SealParticipationMinUnits)
	sum := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(sum[:])
}
