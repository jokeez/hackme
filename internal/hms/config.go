package hms

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config tunes the HMS lane coordinator (heavy VPS #2).
type Config struct {
	PoolID            string
	EpochDuration     time.Duration
	FreezeAfter       time.Duration // from epoch start
	SealWindow        time.Duration // after freeze
	MinQuotaGB        int
	MaxQuotaGB        int
	MaxStrikes        int
	ChallengeTTL      time.Duration
	WorkerOnlineSec   int64 // storage worker considered offline after this silence
	RepairIntervalSec int
	HealthSlashStreak int
	InitialSealTarget []byte // 32-byte big-endian difficulty
	DesiredSealSec    int
	SealRetargetClamp float64
}

func ConfigFromEnv() Config {
	c := Config{
		PoolID:            "hackme-official",
		EpochDuration:     time.Hour,
		FreezeAfter:       50 * time.Minute,
		SealWindow:        10 * time.Minute,
		MinQuotaGB:        50,
		MaxQuotaGB:        16_384,
		MaxStrikes:        3,
		ChallengeTTL:      15 * time.Minute,
		DesiredSealSec:    600,
		SealRetargetClamp: 4.0,
	}
	if v := strings.TrimSpace(os.Getenv("HMS_POOL_ID")); v != "" {
		c.PoolID = v
	}
	if n := envInt("HMS_EPOCH_SECONDS", 3600); n > 0 {
		c.EpochDuration = time.Duration(n) * time.Second
	}
	if n := envInt("HMS_FREEZE_AFTER_SEC", 3000); n > 0 {
		c.FreezeAfter = time.Duration(n) * time.Second
	}
	if n := envInt("HMS_SEAL_WINDOW_SEC", 600); n > 0 {
		c.SealWindow = time.Duration(n) * time.Second
	}
	c.MinQuotaGB = envInt("HMS_MIN_QUOTA_GB", c.MinQuotaGB)
	c.MaxQuotaGB = envInt("HMS_MAX_QUOTA_GB", c.MaxQuotaGB)
	c.MaxStrikes = envInt("HMS_MAX_STRIKES", c.MaxStrikes)
	if n := envInt("HMS_WORKER_ONLINE_SEC", 300); n > 0 {
		c.WorkerOnlineSec = int64(n)
	} else {
		c.WorkerOnlineSec = 300
	}
	c.RepairIntervalSec = envInt("HMS_REPAIR_INTERVAL_SEC", 30)
	c.HealthSlashStreak = envInt("HMS_HEALTH_SLASH_STREAK", c.MaxStrikes)
	if n := envInt("HMS_CHALLENGE_TTL_SEC", 900); n > 0 {
		c.ChallengeTTL = time.Duration(n) * time.Second
	}
	c.DesiredSealSec = envInt("HMS_DESIRED_SEAL_SEC", c.DesiredSealSec)
	c.InitialSealTarget = decodeHex32(os.Getenv("HMS_INITIAL_SEAL_TARGET"), defaultSealTarget())
	return c
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func defaultSealTarget() []byte {
	// ~ moderate demo difficulty (many leading zero bits in BE compare).
	t := make([]byte, 32)
	t[0] = 0x00
	t[1] = 0x00
	t[2] = 0x0f
	t[3] = 0xff
	return t
}
