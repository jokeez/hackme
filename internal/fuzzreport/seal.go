package fuzzreport

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/nacl/box"
)

const sealVersion = "nacl-box-v1"

// NewKey returns an X25519 key pair. The public half goes on the campaign as
// report_seal_pub. The private half stays with the customer. The hub never
// sees it, so a stored report cannot be opened here.
func NewKey() (pub, priv *[32]byte, err error) {
	return box.GenerateKey(rand.Reader)
}

// Pub parses report_seal_pub. Empty or malformed means the campaign is not sealed.
func Pub(cfg map[string]any) *[32]byte {
	if cfg == nil {
		return nil
	}
	s := strings.TrimSpace(stringValue(cfg["report_seal_pub"]))
	if s == "" {
		return nil
	}
	raw, err := decodeKey(s)
	if err != nil || len(raw) != 32 {
		return nil
	}
	var out [32]byte
	copy(out[:], raw)
	return &out
}

// Seal encrypts plaintext to pub. The result is base64 of a NaCl anonymous box.
func Seal(pub *[32]byte, plaintext []byte) (string, error) {
	if pub == nil {
		return "", errors.New("fuzzreport: seal public key required")
	}
	sealed, err := box.SealAnonymous(nil, plaintext, pub, rand.Reader)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal. The customer holds both halves. A different key pair
// does not open the box.
func Open(pub, priv *[32]byte, sealedB64 string) ([]byte, error) {
	if pub == nil || priv == nil {
		return nil, errors.New("fuzzreport: seal key pair required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(sealedB64))
	if err != nil {
		return nil, err
	}
	out, ok := box.OpenAnonymous(nil, raw, pub, priv)
	if !ok {
		return nil, errors.New("fuzzreport: seal box rejected")
	}
	return out, nil
}

// Envelope is the only detail a sealed finding stores in the clear.
func Envelope(pub *[32]byte, payload map[string]any, crashDedup string) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	boxed, err := Seal(pub, body)
	if err != nil {
		return "", err
	}
	env := map[string]any{"seal": sealVersion, "box": boxed}
	if crashDedup != "" {
		env["crash_dedup"] = crashDedup
	}
	out, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// InputFingerprint is a stable dedup id that is not the SHA-256 of the input.
// A short crashing seed cannot be recovered by hashing candidates against the
// findings table. The pepper is not stored next to the row.
func InputFingerprint(campaignID, rawSHA string) string {
	rawSHA = strings.TrimSpace(strings.ToLower(rawSHA))
	if rawSHA == "" {
		return ""
	}
	mac := hmac.New(sha256.New, pepper())
	_, _ = mac.Write([]byte(strings.TrimSpace(campaignID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(rawSHA))
	return hex.EncodeToString(mac.Sum(nil))
}

// BucketToken hides the sanitizer bucket the same way. Dedup still matches.
// The stored token is not the function name or the bug class.
func BucketToken(campaignID, bucket string) string {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" || bucket == "crash|generic" {
		return ""
	}
	mac := hmac.New(sha256.New, pepper())
	_, _ = mac.Write([]byte("bucket"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(campaignID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(bucket))
	return hex.EncodeToString(mac.Sum(nil))
}

func decodeKey(s string) ([]byte, error) {
	if raw, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(s), "0x")); err == nil && len(raw) == 32 {
		return raw, nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	return nil, errors.New("fuzzreport: bad seal key")
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

var (
	pepperOnce sync.Once
	pepperKey  []byte
)

func pepper() []byte {
	pepperOnce.Do(func() {
		if p := strings.TrimSpace(os.Getenv("HACKME_REPORT_SEAL_PEPPER")); p != "" {
			sum := sha256.Sum256([]byte(p))
			pepperKey = sum[:]
			return
		}
		root := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
		if root == "" {
			root = "."
		}
		path := filepath.Join(root, ".cache", "report-seal.pepper")
		if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
			pepperKey = append([]byte(nil), b[:32]...)
			return
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			sum := sha256.Sum256([]byte("fuzzreport-pepper-fallback"))
			pepperKey = sum[:]
			return
		}
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, buf, 0o600); err != nil {
			pepperKey = buf
			return
		}
		pepperKey = buf
	})
	return pepperKey
}
