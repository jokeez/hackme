//go:build ignore

// Export AllSimArch() as JSON for global_gpu_matrix_hardware_audit reports.
package main

import (
	"encoding/json"
	"os"

	"hackme/internal/gputune"
)

func main() {
	out := map[string]any{
		"green":   gputune.GreenCampCatalog(),
		"red":     gputune.RedCampCatalog(),
		"blue":    gputune.BlueCampCatalog(),
		"total":   len(gputune.AllSimArch()),
		"catalog": gputune.AllSimArch(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
