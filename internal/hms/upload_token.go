package hms

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

func hashUploadToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func looksHashedUploadToken(stored string) bool {
	stored = strings.TrimSpace(strings.ToLower(stored))
	if len(stored) != 64 {
		return false
	}
	_, err := hex.DecodeString(stored)
	return err == nil
}

func allowLegacyPlaintextUploadToken() bool {
	for _, k := range []string{
		"HMS_ALLOW_LEGACY_PLAINTEXT",
		"ALLOW_LEGACY_PLAINTEXT",
		"HACKME_HMS_ALLOW_LEGACY_PLAINTEXT",
	} {
		v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
}

func matchUploadToken(stored, presented string) error {
	presented = strings.TrimSpace(presented)
	stored = strings.TrimSpace(stored)
	if presented == "" {
		return errors.New("upload token required")
	}
	if stored == "" {
		return errors.New("invalid upload token")
	}
	if looksHashedUploadToken(stored) {
		want := hashUploadToken(presented)
		if subtle.ConstantTimeCompare([]byte(strings.ToLower(want)), []byte(strings.ToLower(stored))) != 1 {
			return errors.New("invalid upload token")
		}
		return nil
	}
	// Legacy rows stored the bearer token in plaintext.
	if !allowLegacyPlaintextUploadToken() {
		return errors.New("legacy plaintext upload token rejected (set ALLOW_LEGACY_PLAINTEXT=1)")
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(presented)) != 1 {
		return errors.New("invalid upload token")
	}
	return nil
}
