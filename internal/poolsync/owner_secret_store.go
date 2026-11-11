package poolsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// OrdersOwnerSecretDir returns the directory for per-campaign owner secrets
// (mode 0700). Prefer this over plaintext in fuzz_campaigns.config_json (PUB-08).
func OrdersOwnerSecretDir() string {
	if d := strings.TrimSpace(os.Getenv("HACKME_ORDERS_OWNER_SECRET_DIR")); d != "" {
		return d
	}
	if d := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); d != "" {
		return filepath.Join(d, "orders_owner_secrets")
	}
	if root := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); root != "" {
		return filepath.Join(root, "data", "orders_owner_secrets")
	}
	return filepath.Join("data", "orders_owner_secrets")
}

func sanitizeOwnerSecretCampaignID(campaignID string) (string, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return "", fmt.Errorf("campaign_id required")
	}
	if len(campaignID) > 128 {
		return "", fmt.Errorf("campaign_id too long")
	}
	for _, r := range campaignID {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("campaign_id has invalid characters")
	}
	if strings.Contains(campaignID, "..") {
		return "", fmt.Errorf("campaign_id path rejected")
	}
	return campaignID, nil
}

// OrdersOwnerSecretPath is the on-disk path for one campaign's owner secret.
func OrdersOwnerSecretPath(campaignID string) (string, error) {
	id, err := sanitizeOwnerSecretCampaignID(campaignID)
	if err != nil {
		return "", err
	}
	return filepath.Join(OrdersOwnerSecretDir(), id), nil
}

// PersistOrdersOwnerSecret writes the secret to a 0600 file (creates dir 0700).
func PersistOrdersOwnerSecret(campaignID, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("owner secret required")
	}
	path, err := OrdersOwnerSecretPath(campaignID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(secret+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

// LoadOrdersOwnerSecret reads the on-disk owner secret (empty if missing).
func LoadOrdersOwnerSecret(campaignID string) string {
	path, err := OrdersOwnerSecretPath(campaignID)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
}
