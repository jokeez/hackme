package hunt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"hackme/internal/fuzzupstream"
)

// CatalogHarnessHash returns stable content id for a pinned OSS catalog target.
func CatalogHarnessHash(repoRoot, targetID string) (string, error) {
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return "", err
	}
	t, err := manifest.TargetByID(targetID)
	if err != nil {
		return "", err
	}
	sumInput := t.ID + t.Repo + t.Ref + t.Driver + strings.Join(t.UpstreamSrc, ",") + strings.Join(t.BuildFlags, ",")
	sum := sha256.Sum256([]byte(sumInput))
	return hex.EncodeToString(sum[:16]), nil
}

// CatalogTarget loads one manifest target.
func CatalogTarget(repoRoot, targetID string) (fuzzupstream.Target, error) {
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return fuzzupstream.Target{}, err
	}
	t, err := manifest.TargetByID(targetID)
	if err != nil {
		return fuzzupstream.Target{}, fmt.Errorf("hunt: %w", err)
	}
	return t, nil
}
