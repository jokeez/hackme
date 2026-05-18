package logsetup

import (
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// ConfigureFromEnv sets process-wide logging flags and optional rotating file output.
//
// Shared env:
//
//	HACKME_LOG_FILE           path to rotating log file (disabled when empty)
//	HACKME_LOG_MAX_MB         single file size cap in MB (default 100)
//	HACKME_LOG_MAX_BACKUPS    rotated files to keep (default 5)
//	HACKME_LOG_MAX_AGE_DAYS   rotated file max age (default 14)
//	HACKME_LOG_COMPRESS       1/true to gzip rotated files
//
// Component-specific env overrides are checked first:
//
//	<COMPONENT>_LOG_FILE
//	<COMPONENT>_LOG_MAX_MB
//	<COMPONENT>_LOG_MAX_BACKUPS
//	<COMPONENT>_LOG_MAX_AGE_DAYS
//	<COMPONENT>_LOG_COMPRESS
func ConfigureFromEnv(componentPrefix string) {
	// Date/time with microseconds helps diagnose freezes and long handlers.
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	filePath := firstEnv(componentPrefix+"_LOG_FILE", "HACKME_LOG_FILE")
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return
	}

	maxMB := parseInt(firstEnv(componentPrefix+"_LOG_MAX_MB", "HACKME_LOG_MAX_MB"), 100, 1, 10240)
	maxBackups := parseInt(firstEnv(componentPrefix+"_LOG_MAX_BACKUPS", "HACKME_LOG_MAX_BACKUPS"), 5, 1, 1000)
	maxAgeDays := parseInt(firstEnv(componentPrefix+"_LOG_MAX_AGE_DAYS", "HACKME_LOG_MAX_AGE_DAYS"), 14, 1, 3650)
	compress := parseBool(firstEnv(componentPrefix+"_LOG_COMPRESS", "HACKME_LOG_COMPRESS"))

	rot := &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    maxMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   compress,
	}
	log.SetOutput(io.MultiWriter(os.Stdout, rot))
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(strings.TrimSpace(k))); v != "" {
			return v
		}
	}
	return ""
}

func parseInt(raw string, def, min, max int) int {
	v := strings.TrimSpace(raw)
	if v == "" {
		return def
	}
	x, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	if x < min {
		return min
	}
	if x > max {
		return max
	}
	return x
}

func parseBool(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
