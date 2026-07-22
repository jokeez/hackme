package chain

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	UnitsPerHMC             = uint64(100_000_000)
	DefaultTransferMinFee   = uint64(1000)
	DefaultTransferMaxBatch = 256
	TransferSigAlgEd25519   = "ed25519"
)

type TransferTx struct {
	TxType        string `json:"tx_type"`
	SigAlg        string `json:"sig_alg,omitempty"`
	From          string `json:"from"`
	To            string `json:"to"`
	AmountUnits   uint64 `json:"amount_units"`
	FeeUnits      uint64 `json:"fee_units"`
	Nonce         uint64 `json:"nonce"`
	TimestampUnix int64  `json:"timestamp_unix"`
	Memo          string `json:"memo,omitempty"`
	PubKeyEd25519 string `json:"pubkey_ed25519"`
	SigEd25519    string `json:"sig_ed25519"`
}

type TransferAddressState struct {
	Address      string `json:"address"`
	BalanceUnits uint64 `json:"balance_units"`
	NextNonce    uint64 `json:"next_nonce"`
}

type WalletEarningsBucket struct {
	BucketUnix    int64   `json:"bucket_unix"`
	ReceivedHMC   float64 `json:"received_hmc"`
	SentHMC       float64 `json:"sent_hmc"`
	NetHMC        float64 `json:"net_hmc"`
	SettledOutHMC float64 `json:"settled_out_hmc"`
	TxCount       int64   `json:"tx_count"`
}

type WalletEarningsSummary struct {
	Address          string  `json:"address"`
	WindowHours      int     `json:"window_hours"`
	BucketSec        int     `json:"bucket_sec"`
	NowUnix          int64   `json:"now_unix"`
	TotalReceivedHMC float64 `json:"total_received_hmc"`
	TotalSentHMC     float64 `json:"total_sent_hmc"`
	TotalNetHMC      float64 `json:"total_net_hmc"`
	Received24hHMC   float64 `json:"received_24h_hmc"`
	Sent24hHMC       float64 `json:"sent_24h_hmc"`
	Net24hHMC        float64 `json:"net_24h_hmc"`
	SettledOut24hHMC float64 `json:"settled_out_24h_hmc"`
	TxCount24h       int64   `json:"tx_count_24h"`
	// Window totals match window_hours + bucket_sec query (not rolling 24h).
	SettledOutWindowHMC float64                `json:"settled_out_window_hmc"`
	TxCountWindow       int64                  `json:"tx_count_window"`
	Buckets             []WalletEarningsBucket `json:"buckets"`
}

type TransferStatusRow struct {
	TxHash      string `json:"tx_hash"`
	Status      string `json:"status"`
	BlockIndex  int64  `json:"block_index,omitempty"`
	BlockHash   string `json:"block_hash,omitempty"`
	RejectCode  string `json:"reject_code,omitempty"`
	From        string `json:"from"`
	To          string `json:"to"`
	AmountUnits uint64 `json:"amount_units"`
	FeeUnits    uint64 `json:"fee_units"`
	Nonce       uint64 `json:"nonce"`
}

func HMCToUnits(v float64) uint64 {
	if v <= 0 {
		return 0
	}
	return uint64(v*float64(UnitsPerHMC) + 0.5)
}

func UnitsToHMC(v uint64) float64 {
	return float64(v) / float64(UnitsPerHMC)
}

func addressFromPubKeyHex(pubHex string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return "", errors.New("invalid pubkey")
	}
	sum := sha256.Sum256(raw)
	return "HMC-" + hex.EncodeToString(sum[:])[:16], nil
}

func (tx TransferTx) canonicalBytes() ([]byte, error) {
	wire := struct {
		TxType        string `json:"tx_type"`
		SigAlg        string `json:"sig_alg,omitempty"`
		From          string `json:"from"`
		To            string `json:"to"`
		AmountUnits   uint64 `json:"amount_units"`
		FeeUnits      uint64 `json:"fee_units"`
		Nonce         uint64 `json:"nonce"`
		TimestampUnix int64  `json:"timestamp_unix"`
		Memo          string `json:"memo,omitempty"`
		PubKeyEd25519 string `json:"pubkey_ed25519"`
	}{
		TxType:        tx.TxType,
		SigAlg:        strings.TrimSpace(strings.ToLower(tx.SigAlg)),
		From:          strings.TrimSpace(tx.From),
		To:            strings.TrimSpace(tx.To),
		AmountUnits:   tx.AmountUnits,
		FeeUnits:      tx.FeeUnits,
		Nonce:         tx.Nonce,
		TimestampUnix: tx.TimestampUnix,
		Memo:          tx.Memo,
		PubKeyEd25519: strings.TrimSpace(tx.PubKeyEd25519),
	}
	return json.Marshal(wire)
}

// CanonicalBytes returns the canonical JSON payload used for signature checks.
func (tx TransferTx) CanonicalBytes() ([]byte, error) {
	return tx.canonicalBytes()
}

func (tx TransferTx) HashHex() (string, error) {
	b, err := tx.canonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateTransferShape checks structural fields without DB access (fast API reject).
func ValidateTransferShape(tx TransferTx) (code, msg string) {
	if tx.TxType != "transfer_v1" {
		return "invalid_tx_type", "tx_type must be transfer_v1"
	}
	if strings.TrimSpace(tx.From) == "" || strings.TrimSpace(tx.To) == "" || tx.From == tx.To {
		return "invalid_address", "from/to invalid"
	}
	if tx.AmountUnits == 0 {
		return "invalid_amount", "amount_units must be > 0"
	}
	if tx.FeeUnits < DefaultTransferMinFee {
		return "invalid_fee", "fee below minimum"
	}
	if len(tx.Memo) > 256 {
		return "invalid_memo", "memo too long"
	}
	if tx.TimestampUnix <= 0 {
		return "invalid_timestamp", "timestamp required"
	}
	now := time.Now().Unix()
	if tx.TimestampUnix < now-86400 {
		return "tx_too_old", "tx too old"
	}
	if tx.TimestampUnix > now+3600 {
		return "tx_too_far_in_future", "tx too far in future"
	}
	return "", ""
}

func (s *Service) validateTransferTx(ctx context.Context, tx TransferTx, q queryRowContext) (string, string) {
	if tx.TxType != "transfer_v1" {
		return "invalid_tx_type", "tx_type must be transfer_v1"
	}
	if strings.TrimSpace(tx.From) == "" || strings.TrimSpace(tx.To) == "" || tx.From == tx.To {
		return "invalid_address", "from/to invalid"
	}
	if tx.AmountUnits == 0 {
		return "invalid_amount", "amount_units must be > 0"
	}
	if tx.FeeUnits < DefaultTransferMinFee {
		return "invalid_fee", "fee below minimum"
	}
	if len(tx.Memo) > 256 {
		return "invalid_memo", "memo too long"
	}
	if tx.TimestampUnix <= 0 {
		return "invalid_timestamp", "timestamp required"
	}
	now := time.Now().Unix()
	if tx.TimestampUnix < now-86400 {
		return "tx_too_old", "tx too old"
	}
	if tx.TimestampUnix > now+3600 {
		return "tx_too_far_in_future", "tx too far in future"
	}
	alg := strings.TrimSpace(strings.ToLower(tx.SigAlg))
	if alg == "" {
		alg = TransferSigAlgEd25519
	}
	if alg != TransferSigAlgEd25519 {
		return "unsupported_sig_alg", "unsupported signature algorithm"
	}
	pub, err := hex.DecodeString(strings.TrimSpace(tx.PubKeyEd25519))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "invalid_signature", "invalid pubkey"
	}
	sig, err := hex.DecodeString(strings.TrimSpace(tx.SigEd25519))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "invalid_signature", "invalid signature"
	}
	b, err := tx.canonicalBytes()
	if err != nil {
		return "invalid_tx_encoding", err.Error()
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), b, sig) {
		return "invalid_signature", "signature verify failed"
	}
	derived, err := addressFromPubKeyHex(tx.PubKeyEd25519)
	if err != nil {
		return "invalid_signature", "invalid pubkey format"
	}
	if derived != tx.From {
		return "address_pubkey_mismatch", "from does not match pubkey"
	}
	var bal, nonce uint64
	err = q.QueryRowContext(ctx, `SELECT balance_units, next_nonce FROM accounts WHERE address = ?`, tx.From).Scan(&bal, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "insufficient_balance", "sender not funded"
	}
	if err != nil {
		return "internal_error", err.Error()
	}
	if nonce != tx.Nonce {
		return "invalid_nonce", "nonce mismatch"
	}
	total := tx.AmountUnits + tx.FeeUnits
	if total < tx.AmountUnits || bal < total {
		return "insufficient_balance", "insufficient balance"
	}
	return "", ""
}

func (s *Service) SubmitTransferTx(ctx context.Context, tx TransferTx) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, msg := s.validateTransferTx(ctx, tx, s.db)
	if code != "" {
		return "", code, errors.New(msg)
	}
	txHash, err := tx.HashHex()
	if err != nil {
		return "", "invalid_tx_encoding", err
	}
	var existingHash string
	err = s.db.QueryRowContext(ctx,
		`SELECT tx_hash FROM tx_pool WHERE from_address = ? AND nonce = ? AND status = 'pending' LIMIT 1`,
		tx.From, tx.Nonce).Scan(&existingHash)
	if err == nil {
		if strings.TrimSpace(existingHash) == txHash {
			return "", "duplicate_or_replay", errors.New("duplicate tx")
		}
		return "", "pending_nonce_conflict", errors.New("pending tx with same nonce already exists")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "internal_error", err
	}
	raw, _ := json.Marshal(tx)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')`,
		txHash, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits, time.Now().Unix(),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", "duplicate_or_replay", errors.New("duplicate tx")
		}
		return "", "internal_error", err
	}
	return txHash, "pending", nil
}

func (s *Service) TransferAddressState(ctx context.Context, address string) (TransferAddressState, error) {
	var st TransferAddressState
	err := s.db.QueryRowContext(ctx, `SELECT address, balance_units, next_nonce FROM accounts WHERE address = ?`, strings.TrimSpace(address)).
		Scan(&st.Address, &st.BalanceUnits, &st.NextNonce)
	if errors.Is(err, sql.ErrNoRows) {
		return TransferAddressState{Address: strings.TrimSpace(address)}, nil
	}
	return st, err
}

func (s *Service) WalletEarningsSummary(ctx context.Context, address string, windowHours, bucketSec int) (WalletEarningsSummary, error) {
	addr := strings.TrimSpace(address)
	if windowHours <= 0 {
		windowHours = 24
	}
	if windowHours > 24*90 {
		windowHours = 24 * 90
	}
	if bucketSec <= 0 {
		bucketSec = 3600
	}
	if bucketSec < 300 {
		bucketSec = 300
	}
	if bucketSec > 86400 {
		bucketSec = 86400
	}
	nowUnix := time.Now().Unix()
	windowStart := nowUnix - int64(windowHours*3600)
	dailyStart := nowUnix - 86400
	type agg struct {
		ReceivedUnits   uint64
		SentUnits       uint64
		SettledOutUnits uint64
		TxCount         int64
	}
	rollup := map[int64]*agg{}
	var totalRecv, totalSent, recv24, sent24, settled24, settledWindow uint64
	var tx24, txWindow int64

	rows, err := s.db.QueryContext(ctx,
		`SELECT from_address, to_address, amount_units, fee_units, applied_at, tx_json
		 FROM tx_history
		 WHERE status='included'
		   AND (from_address = ? OR to_address = ?)
		   AND applied_at >= ?
		 ORDER BY applied_at ASC`,
		addr, addr, windowStart,
	)
	if err != nil {
		return WalletEarningsSummary{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var fromAddr, toAddr string
		var amountUnits, feeUnits uint64
		var appliedAt int64
		var txJSON string
		if err := rows.Scan(&fromAddr, &toAddr, &amountUnits, &feeUnits, &appliedAt, &txJSON); err != nil {
			return WalletEarningsSummary{}, err
		}
		txWindow++
		key := (appliedAt / int64(bucketSec)) * int64(bucketSec)
		a := rollup[key]
		if a == nil {
			a = &agg{}
			rollup[key] = a
		}
		a.TxCount++
		if strings.EqualFold(strings.TrimSpace(toAddr), addr) {
			a.ReceivedUnits += amountUnits
			totalRecv += amountUnits
			if appliedAt >= dailyStart {
				recv24 += amountUnits
				tx24++
			}
		}
		if strings.EqualFold(strings.TrimSpace(fromAddr), addr) {
			// Sent includes fee as full wallet debit.
			spend := amountUnits + feeUnits
			if spend < amountUnits {
				spend = amountUnits
			}
			a.SentUnits += spend
			totalSent += spend
			if appliedAt >= dailyStart {
				sent24 += spend
			}
			var memo struct {
				Memo string `json:"memo"`
			}
			if err := json.Unmarshal([]byte(txJSON), &memo); err == nil && strings.EqualFold(strings.TrimSpace(memo.Memo), "worker_settlement") {
				a.SettledOutUnits += amountUnits
				settledWindow += amountUnits
				if appliedAt >= dailyStart {
					settled24 += amountUnits
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return WalletEarningsSummary{}, err
	}

	keys := make([]int64, 0, len(rollup))
	for k := range rollup {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	buckets := make([]WalletEarningsBucket, 0, len(keys))
	for _, k := range keys {
		a := rollup[k]
		recv := UnitsToHMC(a.ReceivedUnits)
		sent := UnitsToHMC(a.SentUnits)
		buckets = append(buckets, WalletEarningsBucket{
			BucketUnix:    k,
			ReceivedHMC:   recv,
			SentHMC:       sent,
			NetHMC:        recv - sent,
			SettledOutHMC: UnitsToHMC(a.SettledOutUnits),
			TxCount:       a.TxCount,
		})
	}

	return WalletEarningsSummary{
		Address:             addr,
		WindowHours:         windowHours,
		BucketSec:           bucketSec,
		NowUnix:             nowUnix,
		TotalReceivedHMC:    UnitsToHMC(totalRecv),
		TotalSentHMC:        UnitsToHMC(totalSent),
		TotalNetHMC:         UnitsToHMC(totalRecv) - UnitsToHMC(totalSent),
		Received24hHMC:      UnitsToHMC(recv24),
		Sent24hHMC:          UnitsToHMC(sent24),
		Net24hHMC:           UnitsToHMC(recv24) - UnitsToHMC(sent24),
		SettledOut24hHMC:    UnitsToHMC(settled24),
		TxCount24h:          tx24,
		SettledOutWindowHMC: UnitsToHMC(settledWindow),
		TxCountWindow:       txWindow,
		Buckets:             buckets,
	}, nil
}

type WalletCounterpartyRow struct {
	Peer        string  `json:"peer"`
	ReceivedHMC float64 `json:"received_hmc"`
	SentHMC     float64 `json:"sent_hmc"`
	NetHMC      float64 `json:"net_hmc"`
	TxCount     int64   `json:"tx_count"`
	LastUnix    int64   `json:"last_unix"`
}

type WalletTransferEvent struct {
	TxHash       string  `json:"tx_hash"`
	Direction    string  `json:"direction"`
	Counterparty string  `json:"counterparty"`
	AmountHMC    float64 `json:"amount_hmc"`
	FeeHMC       float64 `json:"fee_hmc"`
	AppliedUnix  int64   `json:"applied_unix"`
	Memo         string  `json:"memo,omitempty"`
}

type WalletActivitySummary struct {
	Address          string                  `json:"address"`
	WindowHours      int                     `json:"window_hours"`
	NowUnix          int64                   `json:"now_unix"`
	TotalReceivedHMC float64                 `json:"total_received_hmc"`
	TotalSentHMC     float64                 `json:"total_sent_hmc"`
	TotalNetHMC      float64                 `json:"total_net_hmc"`
	TxCountWindow    int64                   `json:"tx_count_window"`
	Counterparties   []WalletCounterpartyRow `json:"counterparties"`
	Recent           []WalletTransferEvent   `json:"recent"`
}

func (s *Service) WalletActivitySummary(ctx context.Context, address string, windowHours, recentLimit int) (WalletActivitySummary, error) {
	addr := strings.TrimSpace(address)
	if windowHours <= 0 {
		windowHours = 24
	}
	if windowHours > 24*90 {
		windowHours = 24 * 90
	}
	if recentLimit <= 0 {
		recentLimit = 40
	}
	if recentLimit > 200 {
		recentLimit = 200
	}
	nowUnix := time.Now().Unix()
	windowStart := nowUnix - int64(windowHours*3600)

	type peerAgg struct {
		recvUnits uint64
		sentUnits uint64
		txCount   int64
		lastUnix  int64
	}
	peers := map[string]*peerAgg{}
	var totalRecv, totalSent uint64
	var txWindow int64
	recent := make([]WalletTransferEvent, 0, recentLimit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT tx_hash, from_address, to_address, amount_units, fee_units, applied_at, tx_json
		 FROM tx_history
		 WHERE status='included'
		   AND (from_address = ? OR to_address = ?)
		   AND applied_at >= ?
		 ORDER BY applied_at DESC`,
		addr, addr, windowStart,
	)
	if err != nil {
		return WalletActivitySummary{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var txHash, fromAddr, toAddr, txJSON string
		var amountUnits, feeUnits uint64
		var appliedAt int64
		if err := rows.Scan(&txHash, &fromAddr, &toAddr, &amountUnits, &feeUnits, &appliedAt, &txJSON); err != nil {
			return WalletActivitySummary{}, err
		}
		txWindow++
		var memo struct {
			Memo string `json:"memo"`
		}
		_ = json.Unmarshal([]byte(txJSON), &memo)
		memoStr := strings.TrimSpace(memo.Memo)

		if strings.EqualFold(strings.TrimSpace(toAddr), addr) {
			totalRecv += amountUnits
			peer := strings.TrimSpace(fromAddr)
			pa := peers[peer]
			if pa == nil {
				pa = &peerAgg{}
				peers[peer] = pa
			}
			pa.recvUnits += amountUnits
			pa.txCount++
			if appliedAt > pa.lastUnix {
				pa.lastUnix = appliedAt
			}
			if len(recent) < recentLimit {
				recent = append(recent, WalletTransferEvent{
					TxHash:       txHash,
					Direction:    "in",
					Counterparty: peer,
					AmountHMC:    UnitsToHMC(amountUnits),
					FeeHMC:       UnitsToHMC(feeUnits),
					AppliedUnix:  appliedAt,
					Memo:         memoStr,
				})
			}
		}
		if strings.EqualFold(strings.TrimSpace(fromAddr), addr) {
			spend := amountUnits + feeUnits
			if spend < amountUnits {
				spend = amountUnits
			}
			totalSent += spend
			peer := strings.TrimSpace(toAddr)
			pa := peers[peer]
			if pa == nil {
				pa = &peerAgg{}
				peers[peer] = pa
			}
			pa.sentUnits += spend
			pa.txCount++
			if appliedAt > pa.lastUnix {
				pa.lastUnix = appliedAt
			}
			if len(recent) < recentLimit {
				recent = append(recent, WalletTransferEvent{
					TxHash:       txHash,
					Direction:    "out",
					Counterparty: peer,
					AmountHMC:    UnitsToHMC(amountUnits),
					FeeHMC:       UnitsToHMC(feeUnits),
					AppliedUnix:  appliedAt,
					Memo:         memoStr,
				})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return WalletActivitySummary{}, err
	}

	counterparties := make([]WalletCounterpartyRow, 0, len(peers))
	for peer, pa := range peers {
		recv := UnitsToHMC(pa.recvUnits)
		sent := UnitsToHMC(pa.sentUnits)
		counterparties = append(counterparties, WalletCounterpartyRow{
			Peer:        peer,
			ReceivedHMC: recv,
			SentHMC:     sent,
			NetHMC:      recv - sent,
			TxCount:     pa.txCount,
			LastUnix:    pa.lastUnix,
		})
	}
	sort.Slice(counterparties, func(i, j int) bool {
		ai := counterparties[i].ReceivedHMC + counterparties[i].SentHMC
		aj := counterparties[j].ReceivedHMC + counterparties[j].SentHMC
		if ai != aj {
			return ai > aj
		}
		return counterparties[i].LastUnix > counterparties[j].LastUnix
	})

	return WalletActivitySummary{
		Address:          addr,
		WindowHours:      windowHours,
		NowUnix:          nowUnix,
		TotalReceivedHMC: UnitsToHMC(totalRecv),
		TotalSentHMC:     UnitsToHMC(totalSent),
		TotalNetHMC:      UnitsToHMC(totalRecv) - UnitsToHMC(totalSent),
		TxCountWindow:    txWindow,
		Counterparties:   counterparties,
		Recent:           recent,
	}, nil
}

// RejectStaleLocalPending marks pending transfers from addr whose nonce does not match the
// authoritative next_nonce (desktop followers must not show local-fork ghost txs).
func (s *Service) RejectStaleLocalPending(ctx context.Context, from string, authoritativeNonce uint64) (int64, error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE tx_pool SET status='rejected', reject_code='stale_local_fork'
		 WHERE from_address=? AND status='pending' AND nonce != ?`,
		from, authoritativeNonce,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Service) TransferPool(ctx context.Context, limit int) ([]TransferStatusRow, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tx_hash, status, from_address, to_address, amount_units, fee_units, nonce, reject_code
		 FROM tx_pool WHERE status = 'pending'
		 ORDER BY fee_units DESC, received_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TransferStatusRow
	for rows.Next() {
		var r TransferStatusRow
		if err := rows.Scan(&r.TxHash, &r.Status, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce, &r.RejectCode); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) TransferTxByHash(ctx context.Context, txHash string) (TransferStatusRow, bool, error) {
	var r TransferStatusRow
	err := s.db.QueryRowContext(ctx,
		`SELECT tx_hash, status, block_index, block_hash, reject_code, from_address, to_address, amount_units, fee_units, nonce
		 FROM tx_history WHERE tx_hash = ?`, strings.TrimSpace(txHash)).
		Scan(&r.TxHash, &r.Status, &r.BlockIndex, &r.BlockHash, &r.RejectCode, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce)
	if err == nil {
		return r, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TransferStatusRow{}, false, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT tx_hash, status, from_address, to_address, amount_units, fee_units, nonce, reject_code
		 FROM tx_pool WHERE tx_hash = ?`, strings.TrimSpace(txHash)).
		Scan(&r.TxHash, &r.Status, &r.From, &r.To, &r.AmountUnits, &r.FeeUnits, &r.Nonce, &r.RejectCode)
	if errors.Is(err, sql.ErrNoRows) {
		return TransferStatusRow{}, false, nil
	}
	if err != nil {
		return TransferStatusRow{}, false, err
	}
	return r, true, nil
}

type pendingTransfer struct {
	hash       string
	tx         TransferTx
	receivedAt int64
}

func (s *Service) loadPendingTransfers(ctx context.Context, limit int) ([]pendingTransfer, error) {
	if limit <= 0 {
		limit = DefaultTransferMaxBatch
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tx_hash, tx_json, received_at FROM tx_pool WHERE status='pending' ORDER BY fee_units DESC, received_at ASC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]pendingTransfer, 0, limit)
	for rows.Next() {
		var h, raw string
		var recv int64
		if err := rows.Scan(&h, &raw, &recv); err != nil {
			return nil, err
		}
		var tx TransferTx
		if err := json.Unmarshal([]byte(raw), &tx); err != nil {
			continue
		}
		out = append(out, pendingTransfer{hash: h, tx: tx, receivedAt: recv})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].tx.From == out[j].tx.From && out[i].tx.Nonce != out[j].tx.Nonce {
			return out[i].tx.Nonce < out[j].tx.Nonce
		}
		if out[i].tx.FeeUnits != out[j].tx.FeeUnits {
			return out[i].tx.FeeUnits > out[j].tx.FeeUnits
		}
		return out[i].receivedAt < out[j].receivedAt
	})
	return out, nil
}

func (s *Service) applyPendingTransfers(ctx context.Context, txq queryRowExecContext, blockIndex uint64, blockHash string) error {
	pool, err := s.loadPendingTransfers(ctx, DefaultTransferMaxBatch)
	if err != nil {
		return err
	}
	var walletAddr string
	if err := txq.QueryRowContext(ctx, `SELECT address FROM wallet WHERE id = 1`).Scan(&walletAddr); err != nil {
		return err
	}
	devAddr := DevFeeAddress
	for _, item := range pool {
		code, _ := s.validateTransferTx(ctx, item.tx, txq)
		if code != "" {
			if _, err := txq.ExecContext(ctx, `UPDATE tx_pool SET status='rejected', reject_code=? WHERE tx_hash=?`, code, item.hash); err != nil {
				return err
			}
			if _, err := txq.ExecContext(ctx,
				`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
				 VALUES (?, ?, ?, ?, ?, ?, ?, 'rejected', -1, '', ?, ?)
				 ON CONFLICT(tx_hash) DO NOTHING`,
				item.hash, mustJSON(item.tx), item.tx.From, item.tx.To, item.tx.Nonce, item.tx.FeeUnits, item.tx.AmountUnits, time.Now().Unix(), code); err != nil {
				return err
			}
			if _, err := txq.ExecContext(ctx, `DELETE FROM tx_pool WHERE tx_hash=?`, item.hash); err != nil {
				return err
			}
			continue
		}
		var fromBal, fromNonce, toBal, toNonce uint64
		if err := txq.QueryRowContext(ctx, `SELECT balance_units, next_nonce FROM accounts WHERE address=?`, item.tx.From).Scan(&fromBal, &fromNonce); err != nil {
			return err
		}
		if err := txq.QueryRowContext(ctx, `SELECT balance_units, next_nonce FROM accounts WHERE address=?`, item.tx.To).Scan(&toBal, &toNonce); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			toBal = 0
			toNonce = 0
		}
		total := item.tx.AmountUnits + item.tx.FeeUnits
		fromBal -= total
		fromNonce++
		toBal += item.tx.AmountUnits
		burnFeeUnits := uint64(float64(item.tx.FeeUnits) * NetworkFeeBurnShare)
		devFeeUnits := item.tx.FeeUnits - burnFeeUnits
		if _, err := txq.ExecContext(ctx, `UPDATE accounts SET balance_units=?, next_nonce=?, updated_at=strftime('%s','now') WHERE address=?`, fromBal, fromNonce, item.tx.From); err != nil {
			return err
		}
		if _, err := txq.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, next_nonce, updated_at)
			 VALUES (?, ?, ?, strftime('%s','now'))
			 ON CONFLICT(address) DO UPDATE SET balance_units=excluded.balance_units, updated_at=excluded.updated_at`,
			item.tx.To, toBal, toNonce); err != nil {
			return err
		}
		if devFeeUnits > 0 {
			if _, err := txq.ExecContext(ctx,
				`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
				 ON CONFLICT(address) DO UPDATE SET balance_units=accounts.balance_units + excluded.balance_units, updated_at=excluded.updated_at`,
				devAddr, devFeeUnits); err != nil {
				return err
			}
			if devAddr == walletAddr {
				if _, err := txq.ExecContext(ctx, `UPDATE wallet SET balance_units = balance_units + ?, balance_hmc = (balance_units + ?) / ? WHERE id = 1`, devFeeUnits, devFeeUnits, float64(UnitsPerHMC)); err != nil {
					return err
				}
			}
		}
		if burnFeeUnits > 0 {
			mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, txq)
			if err != nil {
				return err
			}
			if err := s.upsertMetaUint(ctx, txq, metaTotalBurnedUnits, burnedUnits+burnFeeUnits); err != nil {
				return err
			}
			if err := s.upsertMetaFloat(ctx, txq, metaTotalBurnedHMC, UnitsToHMC(burnedUnits+burnFeeUnits)); err != nil {
				return err
			}
			if err := s.upsertMetaFloat(ctx, txq, metaTotalMintedHMC, UnitsToHMC(mintedUnits)); err != nil {
				return err
			}
		}
		if _, err := txq.ExecContext(ctx,
			`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'included', ?, ?, ?, '')`,
			item.hash, mustJSON(item.tx), item.tx.From, item.tx.To, item.tx.Nonce, item.tx.FeeUnits, item.tx.AmountUnits, blockIndex, blockHash, time.Now().Unix()); err != nil {
			return err
		}
		if _, err := txq.ExecContext(ctx, `DELETE FROM tx_pool WHERE tx_hash=?`, item.hash); err != nil {
			return err
		}
	}
	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
