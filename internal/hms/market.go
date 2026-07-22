package hms

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	OrderStatusDraft     = "draft"
	OrderStatusUploading = "uploading"
	OrderStatusStored    = "stored"
	OrderStatusFailed    = "failed"

	maxMarketChunkBytes = 8 << 20 // 8 MiB per upload
)

// StorageOrder is a client backup job on the HMS lane.
type StorageOrder struct {
	OrderID       string  `json:"order_id"`
	Label         string  `json:"label"`
	ClientRef     string  `json:"client_ref"`
	SizePlanBytes int64   `json:"size_plan_bytes"`
	BytesUploaded int64   `json:"bytes_uploaded"`
	ChunkCount    int     `json:"chunk_count"`
	Status        string  `json:"status"`
	QuoteHash     string  `json:"quote_hash,omitempty"`
	PrepaidHMC    float64 `json:"prepaid_hmc,omitempty"`
	RetentionDays int     `json:"retention_days,omitempty"`
	PaymentID     string  `json:"payment_id,omitempty"`
	HealthStatus  string  `json:"health_status,omitempty"`
	HealthDetail  string  `json:"health_detail,omitempty"`
	CreatedUnix   int64   `json:"created_unix"`
	UpdatedUnix   int64   `json:"updated_unix"`
}

// CreateStorageOrder opens a market order; upload_token returned once (not in list API).
type CreateStorageOrderResult struct {
	Order         StorageOrder `json:"order"`
	UploadToken   string       `json:"upload_token"`
	UploadURL     string       `json:"upload_url"`
	ChunkMaxBytes int          `json:"chunk_max_bytes"`
	Quote         *MarketQuote `json:"quote,omitempty"`
}

func marketDataRoot() string {
	if v := strings.TrimSpace(os.Getenv("HMS_MARKET_DATA_DIR")); v != "" {
		return v
	}
	return "data/hms_market"
}

func marketStorageRoot() string {
	if v := strings.TrimSpace(os.Getenv("HMS_MARKET_STORAGE_ROOT")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("HACKME_STORAGE_ROOT"))
}

// CreateStorageOrder registers a paid backup order (quote + payment_id required unless pilot skip).
// Paid path is fail-closed: HMAC secret must be configured and payment_proof must verify
// (payment_id alone is never proof of debit). Pilot skip requires allowInsecurePilot
// (HTTP RemoteAddr loopback) plus explicit insecure/skip env flags.
func (c *Coordinator) CreateStorageOrder(label, clientRef string, sizePlanBytes int64, retentionDays int, quoteHash, paymentID, paymentProof string, allowInsecurePilot bool) (*CreateStorageOrderResult, error) {
	label = strings.TrimSpace(label)
	clientRef = strings.TrimSpace(clientRef)
	if label == "" {
		label = "backup"
	}
	if sizePlanBytes < 0 {
		sizePlanBytes = 0
	}
	paymentID = strings.TrimSpace(paymentID)
	quoteHash = strings.TrimSpace(quoteHash)
	paymentProof = strings.TrimSpace(paymentProof)
	pilotSkip := allowInsecurePilot && PilotPaymentSkipAllowed()
	var q *MarketQuote
	var err error
	if pilotSkip && paymentID == "" {
		paymentID = "pilot-" + randomHex(6)
		q, err = QuoteStorageOrder(sizePlanBytes, retentionDays)
		if err != nil {
			return nil, err
		}
		quoteHash = q.QuoteHash
	} else {
		if paymentID == "" {
			return nil, errors.New("payment_id required (pay via node wallet first)")
		}
		q, err = VerifyQuoteHash(sizePlanBytes, retentionDays, quoteHash)
		if err != nil {
			return nil, err
		}
		if MarketPaymentHMACSecret() == "" {
			return nil, errors.New("payment HMAC secret not configured (fail-closed)")
		}
		if err := VerifyMarketPaymentProof(paymentID, quoteHash, paymentProof, q.TotalDebitHMC); err != nil {
			return nil, err
		}
		var existing string
		err = c.db.QueryRow(`SELECT order_id FROM hms_orders WHERE payment_id=?`, paymentID).Scan(&existing)
		if err == nil {
			return nil, errors.New("payment_id already used")
		}
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}
	if _, err := c.EnsureCapacity(RequiredCapacityBytes(sizePlanBytes)); err != nil {
		return nil, err
	}
	orderID := "ord-" + randomHex(8)
	token := randomHex(16)
	tokenAtRest := hashUploadToken(token)
	now := time.Now().Unix()
	_, err = c.db.Exec(`INSERT INTO hms_orders(order_id, label, client_ref, upload_token, size_plan_bytes, status, quote_hash, prepaid_hmc, retention_days, payment_id, created_unix, updated_unix)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderID, label, clientRef, tokenAtRest, sizePlanBytes, OrderStatusDraft, quoteHash, q.TotalDebitHMC, q.RetentionDays, paymentID, now, now)
	if err != nil {
		return nil, err
	}
	return &CreateStorageOrderResult{
		Order: StorageOrder{
			OrderID: orderID, Label: label, ClientRef: clientRef,
			SizePlanBytes: sizePlanBytes, Status: OrderStatusDraft,
			QuoteHash: quoteHash, PrepaidHMC: q.TotalDebitHMC, RetentionDays: q.RetentionDays,
			PaymentID: paymentID, CreatedUnix: now, UpdatedUnix: now,
		},
		UploadToken:   token,
		UploadURL:     "/api/market/orders/" + orderID + "/upload",
		ChunkMaxBytes: maxMarketChunkBytes,
		Quote:         q,
	}, nil
}

// ListStorageOrders returns recent orders (no upload tokens).
func (c *Coordinator) ListStorageOrders(limit int) ([]StorageOrder, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := c.db.Query(`SELECT order_id, label, client_ref, size_plan_bytes, bytes_uploaded, chunk_count, status,
		quote_hash, prepaid_hmc, retention_days, payment_id, health_status, health_detail, created_unix, updated_unix
		FROM hms_orders ORDER BY created_unix DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StorageOrder
	for rows.Next() {
		var o StorageOrder
		if err := rows.Scan(&o.OrderID, &o.Label, &o.ClientRef, &o.SizePlanBytes, &o.BytesUploaded, &o.ChunkCount, &o.Status,
			&o.QuoteHash, &o.PrepaidHMC, &o.RetentionDays, &o.PaymentID, &o.HealthStatus, &o.HealthDetail, &o.CreatedUnix, &o.UpdatedUnix); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetStorageOrder fetches one order.
func (c *Coordinator) GetStorageOrder(orderID string) (*StorageOrder, error) {
	var o StorageOrder
	err := c.db.QueryRow(`SELECT order_id, label, client_ref, size_plan_bytes, bytes_uploaded, chunk_count, status,
		quote_hash, prepaid_hmc, retention_days, payment_id, health_status, health_detail, created_unix, updated_unix
		FROM hms_orders WHERE order_id=?`, orderID).
		Scan(&o.OrderID, &o.Label, &o.ClientRef, &o.SizePlanBytes, &o.BytesUploaded, &o.ChunkCount, &o.Status,
			&o.QuoteHash, &o.PrepaidHMC, &o.RetentionDays, &o.PaymentID, &o.HealthStatus, &o.HealthDetail, &o.CreatedUnix, &o.UpdatedUnix)
	if err == sql.ErrNoRows {
		return nil, errors.New("order not found")
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (c *Coordinator) verifyUploadToken(orderID, token string) error {
	var stored string
	err := c.db.QueryRow(`SELECT upload_token FROM hms_orders WHERE order_id=?`, orderID).Scan(&stored)
	if err == sql.ErrNoRows {
		return errors.New("order not found")
	}
	if err != nil {
		return err
	}
	return matchUploadToken(stored, token)
}

// PickStorageWorker chooses an online storage worker with the most free quota headroom.
func (c *Coordinator) PickStorageWorker() (string, error) {
	cutoff := c.workerOnlineCutoff()
	var workerID string
	var quota int
	var chunks int
	err := c.db.QueryRow(`
		SELECT w.worker_id, w.quota_gb, COALESCE((SELECT COUNT(*) FROM hms_chunks c WHERE c.worker_id=w.worker_id),0)
		FROM hms_workers w
		WHERE w.role='storage' AND w.quota_gb > 0 AND w.last_seen_unix >= ?
		ORDER BY (w.quota_gb * 1073741824 - COALESCE((SELECT SUM(size) FROM hms_chunks c2 WHERE c2.worker_id=w.worker_id),0)) DESC
		LIMIT 1`, cutoff).Scan(&workerID, &quota, &chunks)
	if err == sql.ErrNoRows {
		return "", errors.New("no storage workers online — start workerstorage first")
	}
	if err != nil {
		return "", err
	}
	_ = quota
	_ = chunks
	return workerID, nil
}

// UploadOrderChunk stores ciphertext and registers chunk on the lane.
func (c *Coordinator) UploadOrderChunk(orderID, uploadToken string, chunkIndex int, ciphertext []byte) (map[string]any, error) {
	if err := c.verifyUploadToken(orderID, uploadToken); err != nil {
		return nil, err
	}
	if chunkIndex < 0 || chunkIndex > 1_000_000 {
		return nil, errors.New("invalid chunk_index")
	}
	if len(ciphertext) == 0 {
		return nil, errors.New("empty ciphertext")
	}
	if len(ciphertext) > maxMarketChunkBytes {
		return nil, fmt.Errorf("chunk exceeds %d bytes", maxMarketChunkBytes)
	}
	var sizePlan, uploaded int64
	if err := c.db.QueryRow(`SELECT size_plan_bytes, bytes_uploaded FROM hms_orders WHERE order_id=?`, orderID).Scan(&sizePlan, &uploaded); err != nil {
		return nil, err
	}
	var prevSize int64
	_ = c.db.QueryRow(`SELECT COALESCE(size,0) FROM hms_order_chunks WHERE order_id=? AND chunk_index=?`, orderID, chunkIndex).Scan(&prevSize)
	newUploaded := uploaded - prevSize + int64(len(ciphertext))
	if sizePlan > 0 && newUploaded > sizePlan {
		return nil, errors.New("upload exceeds order size plan")
	}
	additional := int64(len(ciphertext)) - prevSize
	if additional > 0 {
		if _, err := c.EnsureCapacity(RequiredCapacityBytes(additional)); err != nil {
			return nil, err
		}
	}
	replicaN := marketReplicaCount()
	workers, err := c.PickStorageWorkers(replicaN)
	if err != nil {
		return nil, err
	}
	workerID := workers[0]
	sum := sha256.Sum256(ciphertext)
	chunkID := fmt.Sprintf("%s-c%06d", orderID, chunkIndex)

	var written []string
	for _, wid := range workers {
		if err := c.writeMarketChunkFile(wid, chunkID, ciphertext); err != nil {
			continue
		}
		written = append(written, wid)
	}
	if len(written) == 0 {
		return nil, errors.New("failed to write chunk to any storage host")
	}
	warnDegraded := len(written) < replicaN
	if err := c.AssignMarketChunk(chunkID, written[0], sum[:], uint64(len(ciphertext)), nil); err != nil {
		return nil, err
	}
	if err := c.recordChunkReplicas(orderID, chunkIndex, written); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	_, err = c.db.Exec(`INSERT INTO hms_order_chunks(order_id, chunk_index, chunk_id, worker_id, size, ciphertext_sha256, replica_count, created_unix)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(order_id, chunk_index) DO UPDATE SET chunk_id=excluded.chunk_id, worker_id=excluded.worker_id, size=excluded.size,
		ciphertext_sha256=excluded.ciphertext_sha256, replica_count=excluded.replica_count, created_unix=excluded.created_unix`,
		orderID, chunkIndex, chunkID, workerID, len(ciphertext), sum[:], len(written), now)
	if err != nil {
		return nil, err
	}
	for _, wid := range written {
		c.recordReplicaHealth(orderID, chunkIndex, wid, true)
	}
	status := OrderStatusUploading
	health := HealthOK
	detail := ""
	if warnDegraded {
		health = HealthDegraded
		detail = fmt.Sprintf("replica shortfall %d/%d — repair will backfill when workers online", len(written), replicaN)
	}
	_, err = c.db.Exec(`UPDATE hms_orders SET
		bytes_uploaded=(SELECT COALESCE(SUM(size),0) FROM hms_order_chunks WHERE order_id=?),
		chunk_count=(SELECT COUNT(*) FROM hms_order_chunks WHERE order_id=?),
		status=?, health_status=?, health_detail=?, updated_unix=? WHERE order_id=?`,
		orderID, orderID, status, health, detail, now, orderID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":             true,
		"order_id":       orderID,
		"chunk_id":       chunkID,
		"chunk_index":    chunkIndex,
		"worker_id":      workerID,
		"replica_count":  len(written),
		"replica_hosts":  written,
		"replica_target": replicaN,
		"degraded":       warnDegraded,
		"size":           len(ciphertext),
		"sha256":         encodeHex(sum[:]),
	}, nil
}

func (c *Coordinator) writeMarketChunkFile(workerID, chunkID string, ciphertext []byte) error {
	if err := c.pushChunkToWorkerEndpoint(workerID, chunkID, ciphertext); err != nil {
		if requireRemotePush() {
			return err
		}
		// Soft-fail remote push when not required; same-host path still applies.
	}
	// Drop into worker storage dir when configured (pilot same-host).
	if root := marketStorageRoot(); root != "" {
		p := filepath.Join(root, workerID, chunkID+".dat")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, ciphertext, 0o600); err != nil {
			return err
		}
	}
	// Always keep coordinator copy for restore API later.
	root := filepath.Join(marketDataRoot(), workerID)
	p := filepath.Join(root, chunkID+".dat")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, ciphertext, 0o600)
}

// ListOrderChunks returns chunk metadata for restore.
func (c *Coordinator) ListOrderChunks(orderID string) ([]map[string]any, error) {
	rows, err := c.db.Query(`SELECT chunk_index, chunk_id, worker_id, size, replica_count FROM hms_order_chunks WHERE order_id=? ORDER BY chunk_index`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var idx, replicaCount int
		var chunkID, workerID string
		var size int64
		if err := rows.Scan(&idx, &chunkID, &workerID, &size, &replicaCount); err != nil {
			return nil, err
		}
		replicas, _ := c.listChunkReplicaWorkers(orderID, idx)
		out = append(out, map[string]any{
			"chunk_index": idx, "chunk_id": chunkID, "worker_id": workerID, "size": size,
			"replica_count": replicaCount, "replica_hosts": replicas,
		})
	}
	return out, rows.Err()
}

// DownloadOrderChunk returns encrypted bytes for restore (upload token required).
func (c *Coordinator) DownloadOrderChunk(orderID, uploadToken string, chunkIndex int) ([]byte, string, error) {
	if err := c.verifyUploadToken(orderID, uploadToken); err != nil {
		return nil, "", err
	}
	var chunkID string
	err := c.db.QueryRow(`SELECT chunk_id FROM hms_order_chunks WHERE order_id=? AND chunk_index=?`, orderID, chunkIndex).
		Scan(&chunkID)
	if err == sql.ErrNoRows {
		return nil, "", errors.New("chunk not found")
	}
	if err != nil {
		return nil, "", err
	}
	workers, err := c.listChunkReplicaWorkers(orderID, chunkIndex)
	if err != nil {
		return nil, "", err
	}
	for _, workerID := range workers {
		b, err := c.readMarketChunkFile(workerID, chunkID)
		if err == nil {
			return b, chunkID, nil
		}
	}
	return nil, "", errors.New("chunk file missing on all replicas")
}

// MarketStats for dashboard.
func (c *Coordinator) MarketStats() map[string]any {
	var total, active int
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM hms_orders`).Scan(&total)
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM hms_orders WHERE status IN ('uploading','stored')`).Scan(&active)
	pol := MarketPricingPolicySnapshot()
	capSnap, _ := c.NetworkCapacity()
	return map[string]any{
		"orders_total":       total,
		"orders_active":      active,
		"chunk_max_bytes":    maxMarketChunkBytes,
		"replica_target":     marketReplicaCount(),
		"market_policy_hash": pol.PolicyHash,
		"pricing":            pol,
		"capacity":           capSnap,
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
