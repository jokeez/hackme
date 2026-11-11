package main

import (
	"log"
	"math"
	"os"
	"strings"
)

// Found-rate audit (block withholding guard). Share-PPS pays every verified share, so a client
// that never reports its block hits (malicious, or a GPU build that drops them — PR #59) earns
// full pay while the chain gets no blocks: pool M stays low, the share price stays high and the
// day budget drains for everyone. Each found is a 1/M event per hash, so expected founds are
// known from share-implied hashes; a Poisson lower-tail test (conservative for the per-lease
// Bernoulli sum) flags identities far below that.
//
// HACKME_COORDINATOR_FOUND_AUDIT: log (default) counts and journals flagged identities only,
// on rejects their submits, off disables. Roll out as log, review the journal, then switch on.

const (
	foundAuditMinExpected = 30.0  // no verdict before ~30 expected blocks
	foundAuditMaxRatio    = 0.2   // flag only below 20% of expected founds…
	foundAuditMaxLogP     = -20.7 // …and P(X ≤ actual) < 1e-9
	foundAuditWindow      = 200.0 // halve counters past this many expected founds (recovery)
)

// foundAuditMaxEntries caps the warm cache; var so tests can shrink it.
var foundAuditMaxEntries = 50_000

type foundAuditState struct {
	Expected float64
	Actual   float64
	Flagged  bool

	dirty         bool  // in-memory change not yet in sqlite
	flushing      bool  // async writer in flight for this identity
	persistedUnix int64 // last sqlite write for this identity
}

func foundAuditMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_FOUND_AUDIT"))) {
	case "off", "0", "false":
		return "off"
	case "on", "1", "true", "reject":
		return "on"
	default:
		return "log"
	}
}

// foundAuditKey binds the audit to the payout identity (signer), not a free-form worker id.
func foundAuditKey(signerAddr, workerID string) string {
	if a := strings.TrimSpace(signerAddr); a != "" {
		return "addr:" + strings.ToUpper(a)
	}
	return "wid:" + strings.TrimSpace(workerID)
}

// foundAuditFlaggedNowLocked counts identities currently below their expected found rate.
func (m *workManager) foundAuditFlaggedNowLocked() int {
	n := 0
	for _, st := range m.foundAudit {
		if st.Flagged {
			n++
		}
	}
	return n
}

func (m *workManager) foundAuditEntryLocked(key string) *foundAuditState {
	if m.foundAudit == nil {
		m.foundAudit = make(map[string]*foundAuditState)
	}
	st := m.foundAudit[key]
	if st == nil {
		if len(m.foundAudit) >= foundAuditMaxEntries {
			evicted := false
			for k, v := range m.foundAudit {
				// Never drop Flagged evidence to make room for a new identity.
				if v != nil && v.Flagged {
					continue
				}
				delete(m.foundAudit, k)
				m.deleteFoundAuditLocked(k)
				evicted = true
				break
			}
			if !evicted {
				// Cap saturated with Flagged rows — degrade new-key tracking only.
				return &foundAuditState{}
			}
		}
		st = &foundAuditState{}
		m.foundAudit[key] = st
	}
	return st
}

// noteFoundAuditHitLocked records one block hit verified against the lease M (caller holds m.mu).
// Call only on the accept path (or immediately before the found-rate gate with undo on reject) —
// counting earlier lets retries inflate Actual and clear a withhold flag without paying a found.
func (m *workManager) noteFoundAuditHitLocked(key string) {
	if foundAuditMode() == "off" {
		return
	}
	m.foundAuditEntryLocked(key).Actual++
	m.markFoundAuditDirtyLocked(key, false)
}

// undoFoundAuditHitLocked rolls back noteFoundAuditHitLocked when the submit is rejected after
// the provisional credit (found-rate gate in on mode).
func (m *workManager) undoFoundAuditHitLocked(key string) {
	if foundAuditMode() == "off" {
		return
	}
	st := m.foundAudit[key]
	if st == nil || st.Actual < 1 {
		return
	}
	st.Actual--
	m.markFoundAuditDirtyLocked(key, false)
}

// noteFoundAuditWorkLocked adds share-implied hashes for a lease and reports whether the
// identity is now below its expected found rate (caller holds m.mu).
func (m *workManager) noteFoundAuditWorkLocked(key string, nShares int, shareMod, leaseMod uint64) bool {
	if foundAuditMode() == "off" || nShares <= 0 || shareMod == 0 || leaseMod == 0 {
		return false
	}
	st := m.foundAuditEntryLocked(key)
	// A lease reports at most one found, so its expectation is P(≥1 hit) = 1−e^(−λ), λ = hashes/M
	// (hashes estimated from verified shares). Counting λ itself would flag honest big-lease GPUs.
	lambda := float64(nShares) * float64(shareMod) / float64(leaseMod)
	st.Expected += -math.Expm1(-lambda)
	if st.Expected > foundAuditWindow {
		st.Expected /= 2
		st.Actual /= 2
	}
	suspicious := foundRateSuspicious(st.Expected, st.Actual)
	transition := suspicious != st.Flagged
	if transition {
		st.Flagged = suspicious
		if suspicious {
			m.foundAuditFlagged++
		}
		log.Printf("found audit: %s flagged=%v expected=%.1f actual=%.0f mode=%s",
			sanitizeLogToken(key), suspicious, st.Expected, st.Actual, foundAuditMode())
	}
	m.markFoundAuditDirtyLocked(key, transition) // flag changes are durable at once
	return suspicious
}

func foundRateSuspicious(expected, actual float64) bool {
	if expected < foundAuditMinExpected || actual > expected*foundAuditMaxRatio {
		return false
	}
	return poissonLogCDF(int(math.Floor(actual)), expected) < foundAuditMaxLogP
}

// poissonLogCDF returns ln P(X ≤ k) for X ~ Poisson(lambda).
func poissonLogCDF(k int, lambda float64) float64 {
	if k < 0 {
		return math.Inf(-1)
	}
	if lambda <= 0 {
		return 0
	}
	// log-sum-exp over ln(λ^i e^-λ / i!)
	terms := make([]float64, k+1)
	maxT := math.Inf(-1)
	for i := 0; i <= k; i++ {
		lg, _ := math.Lgamma(float64(i + 1))
		terms[i] = float64(i)*math.Log(lambda) - lambda - lg
		if terms[i] > maxT {
			maxT = terms[i]
		}
	}
	sum := 0.0
	for _, t := range terms {
		sum += math.Exp(t - maxT)
	}
	return maxT + math.Log(sum)
}

// staleFoundProvenLocked reports whether a found on a superseded tip is still a real hit on
// the lease's own prev_hash (it cannot mint, but it counts for the found-rate audit).
func (m *workManager) staleFoundProvenLocked(rec leaseRecord, req submitWorkRequest, signerAddr string, leaseMod uint64) bool {
	if leaseMod == 0 || req.FoundNonce < req.BaseNonce || req.FoundNonce-req.BaseNonce >= req.BatchSize {
		return false
	}
	addr := strings.TrimSpace(rec.MinerAddress)
	if addr == "" {
		addr = strings.TrimSpace(signerAddr)
	}
	if addr == "" {
		addr = strings.TrimSpace(req.MinerAddress)
	}
	fv := rec.FormulaVersion
	if fv <= 0 {
		fv = m.resolvedPoHFormulaVersionLocked()
	}
	return addr != "" && validFoundNoncePoH(req.FoundNonce, leaseMod, fv, strings.TrimSpace(rec.PrevHash), addr)
}
