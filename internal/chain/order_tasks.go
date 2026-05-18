package chain

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"hackme/internal/sandbox"
)

// ErrInsufficientBalance is returned when the node wallet cannot cover order escrow.
var ErrInsufficientBalance = errors.New("chain: insufficient wallet balance for order escrow (reward_hmc × target_solves)")
var ErrOrderEscrowRateLimited = errors.New("chain: order escrow rate limited (hourly cap)")

// TaskSourceOrder marks a task row loaded from SQLite (POST /api/tasks).
const TaskSourceOrder = "order"

// Order task statuses. Runtime transitions are open -> completed and
// open -> cancelled (timeout/refund path).
const (
	TaskStatusOpen      = "open"
	TaskStatusCompleted = "completed"
	TaskStatusCancelled = "cancelled"
)

// OrderTaskRow is one row for GET /api/tasks and the Orders dashboard.
type OrderTaskRow struct {
	ID              string  `json:"id"`
	ArtifactHash    string  `json:"artifact_hash"`
	Reward          float64 `json:"reward"`
	DifficultyScore int     `json:"difficulty_score,omitempty"`
	Status          string  `json:"status"`
	CreatedAtUnix   int64   `json:"created_at"`
	Kind            string  `json:"kind"`
	TargetSolves    int     `json:"target_solves"`
	ProgressCount   int     `json:"progress_count"`
	ProgressPct     float64 `json:"progress_pct"`
	PayerRef        string  `json:"payer_ref,omitempty"`
	PrepaidHMC      float64 `json:"prepaid_hmc,omitempty"`
	ExpiresAtUnix   int64   `json:"expires_at,omitempty"`
	CancelledAtUnix int64   `json:"cancelled_at,omitempty"`
	CancelReason    string  `json:"cancel_reason,omitempty"`
	RefundedHMC     float64 `json:"refunded_hmc,omitempty"`
	ManifestJSON    string  `json:"manifest_json,omitempty"`
}

type orderManifestJSON struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	ArtifactHash     string  `json:"artifact_hash"`
	WasmArtifactPath string  `json:"wasm_artifact_path"`
	RewardHMC        float64 `json:"reward_hmc"`
	DifficultyScore  int     `json:"difficulty_score,omitempty"`
	TimeoutMS        int64   `json:"timeout_ms"`
	TargetSolves     int     `json:"target_solves"`
	TTLSeconds       int64   `json:"ttl_sec,omitempty"`
	PayerRef         string  `json:"payer_ref"`
}

const (
	maxPayerRefRunes                = 256
	maxOrderTargetSolves            = 10000
	defaultMaxOrderEscrowPerHourHMC = 25.0
	defaultOrderMinTTLSeconds       = int64(2 * 3600)
	defaultOrderMaxTTLSeconds       = int64(72 * 3600)
	defaultOrderAutoSafetyFactor    = 2.5
	defaultOrderEstNetEvalPerSec    = 2_000_000.0
)

// InsertOrderResult is returned after a successful paid order insert.
type InsertOrderResult struct {
	ID            string  `json:"id"`
	PayerRef      string  `json:"payer_ref,omitempty"`
	PrepaidHMC    float64 `json:"prepaid_hmc"`
	OrderFeeHMC   float64 `json:"order_fee_hmc"`
	TotalDebitHMC float64 `json:"total_debit_hmc"`
	BurnHMC       float64 `json:"burn_hmc"`
	BalanceAfter  float64 `json:"balance_after"`
	ExpiresAtUnix int64   `json:"expires_at,omitempty"`
	TTLSeconds    int64   `json:"ttl_sec,omitempty"`
}

// InsertOrderTask validates a manifest JSON, debits escrow from the node wallet (reward × target_solves), and inserts an open task.
func (s *Service) InsertOrderTask(ctx context.Context, manifestJSON []byte) (*InsertOrderResult, error) {
	var m orderManifestJSON
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("chain: invalid manifest json: %w", err)
	}
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return nil, errors.New("chain: manifest id required")
	}
	kind := TaskKind(strings.TrimSpace(m.Kind))
	if kind == "" {
		kind = TaskKindSyntheticPoH
	}
	if kind != TaskKindSyntheticPoH {
		return nil, fmt.Errorf("chain: unsupported task kind %q (only %s for now)", kind, TaskKindSyntheticPoH)
	}
	if m.RewardHMC <= 0 {
		return nil, errors.New("chain: reward_hmc must be > 0 for paid orders")
	}
	diffScore := m.DifficultyScore
	if diffScore <= 0 {
		diffScore = MinDifficultyScore
	}
	if diffScore < MinDifficultyScore || diffScore > MaxDifficultyScore {
		return nil, fmt.Errorf("chain: difficulty_score must be %d..%d", MinDifficultyScore, MaxDifficultyScore)
	}
	minReward := float64(diffScore) * RewardPerDifficultyUnit
	if m.RewardHMC+1e-12 < minReward {
		return nil, fmt.Errorf("chain: reward_hmc too low for difficulty_score=%d (min %.6f HMC)", diffScore, minReward)
	}
	if wb, err := s.WasmCheckFromManifestJSON(manifestJSON); err != nil {
		return nil, err
	} else if len(wb) > 0 {
		if err := sandbox.ValidateCheckWasm(ctx, wb); err != nil {
			return nil, fmt.Errorf("chain: wasm check module: %w", err)
		}
	}
	payerRef := strings.TrimSpace(m.PayerRef)
	if utf8.RuneCountInString(payerRef) > maxPayerRefRunes {
		return nil, fmt.Errorf("chain: payer_ref too long (max %d runes)", maxPayerRefRunes)
	}
	artifact := strings.TrimSpace(m.ArtifactHash)
	if strings.TrimSpace(m.WasmArtifactPath) != "" {
		if artifact == "" {
			return nil, errors.New("chain: artifact_hash required when wasm_artifact_path is set")
		}
	} else if artifact == "" {
		sum := sha256.Sum256(manifestJSON)
		artifact = hex.EncodeToString(sum[:])
	}
	target := m.TargetSolves
	if target < 1 {
		target = 1
	}
	if target > maxOrderTargetSolves {
		return nil, fmt.Errorf("chain: target_solves too large (max %d)", maxOrderTargetSolves)
	}
	ttlSec, err := resolveOrderTTLSeconds(m.TTLSeconds, target)
	if err != nil {
		return nil, err
	}
	prepaid := m.RewardHMC * float64(target)
	if prepaid <= 0 || prepaid != prepaid { // NaN
		return nil, errors.New("chain: invalid prepaid total")
	}
	orderFee := prepaid * OrderPlatformFeeRate
	if orderFee < 0 || orderFee != orderFee {
		return nil, errors.New("chain: invalid order fee total")
	}
	totalDebit := prepaid + orderFee
	if totalDebit <= 0 || totalDebit != totalDebit {
		return nil, errors.New("chain: invalid total debit")
	}
	prepaidUnits := HMCToUnits(prepaid)
	if prepaidUnits == 0 {
		return nil, errors.New("chain: invalid prepaid total in units")
	}
	orderFeeUnits := HMCToUnits(orderFee)
	totalDebitUnits := prepaidUnits
	if ^uint64(0)-totalDebitUnits < orderFeeUnits {
		return nil, errors.New("chain: total debit overflow")
	}
	totalDebitUnits += orderFeeUnits
	hourCap := defaultMaxOrderEscrowPerHourHMC
	if v := strings.TrimSpace(os.Getenv("HACKME_MAX_ORDER_ESCROW_PER_HOUR_HMC")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x > 0 {
			hourCap = x
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var balUnits uint64
	var walletAddr string
	if err := tx.QueryRowContext(ctx, `SELECT address, balance_units FROM wallet WHERE id = 1`).Scan(&walletAddr, &balUnits); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("chain: genesis required (wallet row missing)")
		}
		return nil, err
	}
	if balUnits < totalDebitUnits {
		return nil, ErrInsufficientBalance
	}
	var hourEscrow float64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(prepaid_hmc),0) FROM tasks WHERE created_at >= ?`,
		time.Now().Unix()-3600).Scan(&hourEscrow); err != nil {
		return nil, err
	}
	if hourEscrow+prepaid > hourCap+1e-12 {
		return nil, ErrOrderEscrowRateLimited
	}
	var accountUnits uint64
	if err := tx.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address = ?`, walletAddr).Scan(&accountUnits); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInsufficientBalance
		}
		return nil, err
	}
	if accountUnits < totalDebitUnits {
		return nil, ErrInsufficientBalance
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units - ?, balance_hmc = (balance_units - ?) / ? WHERE id = 1`, totalDebitUnits, totalDebitUnits, float64(UnitsPerHMC)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_units = balance_units - ?,
			updated_at = strftime('%s','now')
		WHERE address = ?`,
		totalDebitUnits, walletAddr); err != nil {
		return nil, err
	}
	if orderFeeUnits > 0 {
		devAddr := DevFeeAddress
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
			 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
			devAddr, orderFeeUnits); err != nil {
			return nil, err
		}
		if devAddr == walletAddr {
			if _, err := tx.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id = 1`, orderFeeUnits, orderFeeUnits, float64(UnitsPerHMC)); err != nil {
				return nil, err
			}
		}
	}
	burn := prepaid * OrderBurnRate
	burnUnits := HMCToUnits(burn)
	escrowUnits := prepaidUnits
	if burn > 0 {
		mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, tx)
		if err != nil {
			return nil, err
		}
		if err := s.upsertMetaUint(ctx, tx, metaTotalBurnedUnits, burnedUnits+burnUnits); err != nil {
			return nil, err
		}
		if err := s.upsertMetaFloat(ctx, tx, metaTotalBurnedHMC, UnitsToHMC(burnedUnits+burnUnits)); err != nil {
			return nil, err
		}
		// keep minted float in sync with units view for compatibility dashboards.
		if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits)); err != nil {
			return nil, err
		}
	}
	curEscrowUnits, err := s.metaUint(ctx, tx, metaOrderEscrowUnits, 0)
	if err != nil {
		return nil, err
	}
	if ^uint64(0)-curEscrowUnits < escrowUnits {
		return nil, errors.New("chain: order escrow counter overflow")
	}
	if err := s.upsertMetaUint(ctx, tx, metaOrderEscrowUnits, curEscrowUnits+escrowUnits); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	expiresAt := now + ttlSec
	_, err = tx.ExecContext(ctx,
		`INSERT INTO tasks (id, artifact_hash, reward, status, created_at, manifest_json, kind, target_solves, progress_count, payer_ref, prepaid_hmc, expires_at)
		 VALUES (?,?,?,?,?,?,?,?,0,?,?,?)`,
		id, artifact, m.RewardHMC, TaskStatusOpen, now, string(manifestJSON), string(kind), target, payerRef, prepaid, expiresAt,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("chain: task id %q already exists", id)
		}
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id = 1`).Scan(&balUnits); err != nil {
		return nil, err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &InsertOrderResult{
		ID:            id,
		PayerRef:      payerRef,
		PrepaidHMC:    prepaid,
		OrderFeeHMC:   orderFee,
		TotalDebitHMC: totalDebit,
		BurnHMC:       burn,
		BalanceAfter:  UnitsToHMC(balUnits),
		ExpiresAtUnix: expiresAt,
		TTLSeconds:    ttlSec,
	}, nil
}

// ListOrderTasks returns all tasks (newest first) for admin / Orders UI.
func (s *Service) ListOrderTasks(ctx context.Context) ([]OrderTaskRow, error) {
	if err := s.ExpireOpenOrderTasks(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, artifact_hash, reward, status, created_at, kind, target_solves, progress_count, payer_ref, prepaid_hmc, expires_at, cancelled_at, cancel_reason, refunded_hmc, manifest_json
		 FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrderTaskRow
	for rows.Next() {
		var r OrderTaskRow
		if err := rows.Scan(&r.ID, &r.ArtifactHash, &r.Reward, &r.Status, &r.CreatedAtUnix, &r.Kind, &r.TargetSolves, &r.ProgressCount, &r.PayerRef, &r.PrepaidHMC, &r.ExpiresAtUnix, &r.CancelledAtUnix, &r.CancelReason, &r.RefundedHMC, &r.ManifestJSON); err != nil {
			return nil, err
		}
		if r.TargetSolves > 0 {
			r.ProgressPct = float64(r.ProgressCount) / float64(r.TargetSolves) * 100
			if r.ProgressPct > 100 {
				r.ProgressPct = 100
			}
		}
		if strings.TrimSpace(r.ManifestJSON) != "" {
			var m orderManifestJSON
			if err := json.Unmarshal([]byte(r.ManifestJSON), &m); err == nil && m.DifficultyScore > 0 {
				r.DifficultyScore = m.DifficultyScore
			}
		}
		out = append(out, r)
	}
	if out == nil {
		out = []OrderTaskRow{}
	}
	return out, rows.Err()
}

// ExpireOpenOrderTasks auto-cancels overdue orders and refunds unused escrow to wallet/account.
// Already paid solves are preserved; only remaining liability is refunded.
func (s *Service) ExpireOpenOrderTasks(ctx context.Context) error {
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := s.expireOpenOrderTasksTx(ctx, tx, now); err != nil {
		return err
	}
	if err := s.checkEconomicInvariants(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) expireOpenOrderTasksTx(ctx context.Context, tx *sql.Tx, now int64) (int, error) {
	var walletAddr string
	if err := tx.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id = 1`).Scan(&walletAddr); err != nil {
		// Genesis not installed yet: no wallet row. Nothing to expire/refund; skip so GET /api/tasks can list (empty).
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	escrowUnits, err := s.metaUint(ctx, tx, metaOrderEscrowUnits, 0)
	if err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id, reward, target_solves, progress_count
		 FROM tasks
		 WHERE status = ? AND expires_at > 0 AND expires_at <= ?`,
		TaskStatusOpen, now)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type expiredRow struct {
		id          string
		refundUnits uint64
	}
	var expired []expiredRow
	for rows.Next() {
		var id string
		var reward float64
		var target, progress int
		if err := rows.Scan(&id, &reward, &target, &progress); err != nil {
			return 0, err
		}
		remaining := target - progress
		if remaining < 0 {
			remaining = 0
		}
		refundUnits := uint64(0)
		if remaining > 0 && reward > 0 {
			refundUnits = HMCToUnits(reward * float64(remaining))
		}
		expired = append(expired, expiredRow{id: id, refundUnits: refundUnits})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(expired) == 0 {
		return 0, nil
	}
	for _, ex := range expired {
		if ex.refundUnits > escrowUnits {
			return 0, fmt.Errorf("chain: order escrow depleted during expire (%d < %d)", escrowUnits, ex.refundUnits)
		}
		if ex.refundUnits > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id = 1`, ex.refundUnits, ex.refundUnits, float64(UnitsPerHMC)); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
				 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
				walletAddr, ex.refundUnits); err != nil {
				return 0, err
			}
			escrowUnits -= ex.refundUnits
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks
			 SET status = ?, cancelled_at = ?, cancel_reason = ?, refunded_hmc = ?
			 WHERE id = ? AND status = ?`,
			TaskStatusCancelled, now, "timeout", UnitsToHMC(ex.refundUnits), ex.id, TaskStatusOpen); err != nil {
			return 0, err
		}
	}
	if err := s.upsertMetaUint(ctx, tx, metaOrderEscrowUnits, escrowUnits); err != nil {
		return 0, err
	}
	return len(expired), nil
}

func resolveOrderTTLSeconds(manualTTL int64, targetSolves int) (int64, error) {
	minTTL := defaultOrderMinTTLSeconds
	maxTTL := defaultOrderMaxTTLSeconds
	if v := strings.TrimSpace(os.Getenv("HACKME_ORDER_MIN_TTL_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= 1 {
			minTTL = x
		}
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_ORDER_MAX_TTL_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= minTTL {
			maxTTL = x
		}
	}
	if manualTTL > 0 {
		if manualTTL < minTTL || manualTTL > maxTTL {
			return 0, fmt.Errorf("chain: ttl_sec out of range (%d..%d)", minTTL, maxTTL)
		}
		return manualTTL, nil
	}
	estEvalPerSec := defaultOrderEstNetEvalPerSec
	if v := strings.TrimSpace(os.Getenv("HACKME_ORDER_EST_NET_EVAL_PER_SEC")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x > 0 {
			estEvalPerSec = x
		}
	}
	sf := defaultOrderAutoSafetyFactor
	if v := strings.TrimSpace(os.Getenv("HACKME_ORDER_TTL_SAFETY_FACTOR")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 1.0 {
			sf = x
		}
	}
	if targetSolves < 1 {
		targetSolves = 1
	}
	eta := (float64(targetSolves) * float64(DefaultPoHTargetMod)) / estEvalPerSec
	ttl := int64(eta * sf)
	if ttl < minTTL {
		ttl = minTTL
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}
	return ttl, nil
}

func (s *Service) bumpOrderTaskProgress(ctx context.Context, tx *sql.Tx, orderTaskID string) error {
	if orderTaskID == "" {
		return nil
	}
	// Status must be updated BEFORE progress_count: SQLite evaluates SET left-to-right,
	// and the CASE must use the *pre-increment* progress_count. If progress_count were
	// incremented first, (progress_count+1) in CASE would see the new value and e.g.
	// target_solves=2 would mark completed after a single block.
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET
			status = CASE WHEN progress_count + 1 >= target_solves THEN ? ELSE status END,
			progress_count = progress_count + 1
		 WHERE id = ? AND status = ?`,
		TaskStatusCompleted, orderTaskID, TaskStatusOpen,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if orderTaskID != "" && n == 0 {
		return fmt.Errorf("chain: order task %q not open or missing (refuse PoH block)", orderTaskID)
	}
	return nil
}
