package hms

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// HMS market kernel sheet — frozen at compile time.
// Do not read overrides from env; changing values requires a coordinated lane upgrade
// and a new market_policy_hash (see TestMarketPolicyMatchesChainKernel).
const (
	MarketPlatformFeeRate = 0.05 // align chain.OrderPlatformFeeRate
	MarketBurnRate        = 0.10 // align chain.OrderBurnRate
	MarketMinPrepaidHMC   = 0.01
	// StorageHMCPerGBMonth is the storage pool accrual price (GB × month fraction).
	StorageHMCPerGBMonth       = 0.002
	MarketDefaultRetentionDays = 30
	MarketMinRetentionDays     = 7
	MarketMaxRetentionDays     = 365
	MarketMinBillableBytes     = 1
	MarketMaxBillableBytes     = 16 << 40 // 16 TiB soft cap per order
)

// MarketQuote is a priced storage order from the kernel sheet.
type MarketQuote struct {
	SizeBytes          int64   `json:"size_bytes"`
	RetentionDays      int     `json:"retention_days"`
	StorageGB          float64 `json:"storage_gb"`
	GBMonths           float64 `json:"gb_months"`
	StorageSubtotalHMC float64 `json:"storage_subtotal_hmc"`
	PlatformFeeHMC     float64 `json:"platform_fee_hmc"`
	BurnHMC            float64 `json:"burn_hmc"`
	TotalDebitHMC      float64 `json:"total_debit_hmc"`
	PolicyHash         string  `json:"policy_hash"`
	QuoteHash          string  `json:"quote_hash"`
}

// MarketPricingPolicy is exposed read-only to clients (dashboard / API).
type MarketPricingPolicy struct {
	StorageHMCPerGBMonth float64 `json:"storage_hmc_per_gb_month"`
	PlatformFeeRate      float64 `json:"platform_fee_rate"`
	BurnRate             float64 `json:"burn_rate"`
	MinPrepaidHMC        float64 `json:"min_prepaid_hmc"`
	DefaultRetentionDays int     `json:"default_retention_days"`
	MinRetentionDays     int     `json:"min_retention_days"`
	MaxRetentionDays     int     `json:"max_retention_days"`
	PolicyHash           string  `json:"policy_hash"`
	ChunkMaxBytes        int     `json:"chunk_max_bytes"`
	PaymentAsset         string  `json:"payment_asset"`
	PaymentNote          string  `json:"payment_note"`
}

func MarketPricingPolicySnapshot() MarketPricingPolicy {
	return MarketPricingPolicy{
		StorageHMCPerGBMonth: StorageHMCPerGBMonth,
		PlatformFeeRate:      MarketPlatformFeeRate,
		BurnRate:             MarketBurnRate,
		MinPrepaidHMC:        MarketMinPrepaidHMC,
		DefaultRetentionDays: MarketDefaultRetentionDays,
		MinRetentionDays:     MarketMinRetentionDays,
		MaxRetentionDays:     MarketMaxRetentionDays,
		PolicyHash:           marketPolicyHash(),
		ChunkMaxBytes:        maxMarketChunkBytes,
		PaymentAsset:         "HMC",
		PaymentNote:          "Debit local node wallet; rates are kernel-locked (not env-tunable).",
	}
}

func marketPolicyHash() string {
	wire := marketPolicyWire()
	sum := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(sum[:])
}

func marketPolicyWire() string {
	return "storage_hmc_per_gb_month=" + strconv.FormatFloat(StorageHMCPerGBMonth, 'f', 9, 64) + ";" +
		"platform_fee_rate=" + strconv.FormatFloat(MarketPlatformFeeRate, 'f', 6, 64) + ";" +
		"burn_rate=" + strconv.FormatFloat(MarketBurnRate, 'f', 6, 64) + ";" +
		"min_prepaid_hmc=" + strconv.FormatFloat(MarketMinPrepaidHMC, 'f', 6, 64) + ";" +
		"default_retention_days=" + strconv.Itoa(MarketDefaultRetentionDays) + ";" +
		"min_retention_days=" + strconv.Itoa(MarketMinRetentionDays) + ";" +
		"max_retention_days=" + strconv.Itoa(MarketMaxRetentionDays)
}

func validateMarketPolicy() error {
	if MarketPlatformFeeRate < 0 || MarketBurnRate < 0 {
		return fmt.Errorf("hms market: negative fee or burn rate")
	}
	if StorageHMCPerGBMonth <= 0 {
		return fmt.Errorf("hms market: storage rate must be positive")
	}
	if MarketMinPrepaidHMC <= 0 {
		return fmt.Errorf("hms market: min prepaid must be positive")
	}
	if MarketMinRetentionDays <= 0 || MarketMaxRetentionDays < MarketMinRetentionDays {
		return fmt.Errorf("hms market: invalid retention bounds")
	}
	return nil
}

// QuoteStorageOrder computes a kernel-locked price for a backup order.
func QuoteStorageOrder(sizeBytes int64, retentionDays int) (*MarketQuote, error) {
	if err := validateMarketPolicy(); err != nil {
		return nil, err
	}
	if sizeBytes < MarketMinBillableBytes {
		return nil, fmt.Errorf("size_bytes below minimum %d", MarketMinBillableBytes)
	}
	if sizeBytes > MarketMaxBillableBytes {
		return nil, fmt.Errorf("size_bytes above maximum %d", MarketMaxBillableBytes)
	}
	if retentionDays <= 0 {
		retentionDays = MarketDefaultRetentionDays
	}
	if retentionDays < MarketMinRetentionDays || retentionDays > MarketMaxRetentionDays {
		return nil, fmt.Errorf("retention_days must be %d..%d", MarketMinRetentionDays, MarketMaxRetentionDays)
	}
	gb := float64(sizeBytes) / (1024 * 1024 * 1024)
	const minBillableGB = 5.0 // kernel sheet floor per order
	if gb < minBillableGB {
		gb = minBillableGB
	}
	gbMonths := gb * (float64(retentionDays) / 30.0)
	storage := roundHMC(gbMonths * StorageHMCPerGBMonth)
	if storage <= 0 {
		storage = roundHMC(MarketMinPrepaidHMC / (1 + MarketPlatformFeeRate))
	}
	fee := roundHMC(storage * MarketPlatformFeeRate)
	burn := roundHMC(storage * MarketBurnRate)
	total := roundHMC(storage + fee)
	if total+1e-12 < MarketMinPrepaidHMC {
		return nil, fmt.Errorf("quote below min prepaid %.4f HMC", MarketMinPrepaidHMC)
	}
	q := &MarketQuote{
		SizeBytes:          sizeBytes,
		RetentionDays:      retentionDays,
		StorageGB:          roundHMC(gb),
		GBMonths:           roundHMC(gbMonths),
		StorageSubtotalHMC: storage,
		PlatformFeeHMC:     fee,
		BurnHMC:            burn,
		TotalDebitHMC:      total,
		PolicyHash:         marketPolicyHash(),
	}
	q.QuoteHash = quoteHash(q)
	return q, nil
}

func quoteHash(q *MarketQuote) string {
	wire := fmt.Sprintf("size=%d;ret=%d;storage=%.9f;fee=%.9f;burn=%.9f;total=%.9f;policy=%s",
		q.SizeBytes, q.RetentionDays, q.StorageSubtotalHMC, q.PlatformFeeHMC, q.BurnHMC, q.TotalDebitHMC, q.PolicyHash)
	sum := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(sum[:])
}

// VerifyQuoteHash recomputes kernel pricing and checks the client-supplied hash.
func VerifyQuoteHash(sizeBytes int64, retentionDays int, quoteHash string) (*MarketQuote, error) {
	q, err := QuoteStorageOrder(sizeBytes, retentionDays)
	if err != nil {
		return nil, err
	}
	if q.QuoteHash != strings.TrimSpace(quoteHash) {
		return nil, fmt.Errorf("quote_hash mismatch (repriced — use fresh /api/market/quote)")
	}
	return q, nil
}

func roundHMC(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1e8) / 1e8
}
