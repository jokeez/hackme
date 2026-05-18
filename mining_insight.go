package main

import "math"

// computeMiningInsight returns ETA (seconds) for the next PoH hit at current rate, a 0..~1 progress
// curve for the current round (attempts since last block), and projected HMC/hour from that ETA.
// ETA uses order-of-magnitude M / rate; progress uses 1-exp(-attempts/M). Not a guarantee (retarget, races).
func computeMiningInsight(attempts, targetMod uint64, attemptsPerSec, rewardHMC float64) (etaSec, progress, projectedHmcHour float64) {
	if targetMod < 251 {
		targetMod = 251
	}
	if attemptsPerSec < 1e-9 {
		return -1, 0, 0
	}
	etaSec = float64(targetMod) / attemptsPerSec
	if math.IsNaN(etaSec) || math.IsInf(etaSec, 0) || etaSec <= 0 {
		return -1, 0, 0
	}
	progress = 1 - math.Exp(-float64(attempts)/float64(targetMod))
	if progress > 0.995 {
		progress = 0.995
	}
	if rewardHMC > 0 {
		projectedHmcHour = rewardHMC * (3600.0 / etaSec)
	}
	return etaSec, progress, projectedHmcHour
}
