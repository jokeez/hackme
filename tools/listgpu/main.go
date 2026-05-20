// List GPUs via gpupoh (build with -tags cuda and/or opencl).
//   go run -tags cuda ./tools/listgpu
//   go run -tags opencl ./tools/listgpu
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"hackme/internal/gpupoh"
)

func main() {
	tag := "cpu"
	if os.Getenv("HACKME_LISTGPU_TAG") != "" {
		tag = os.Getenv("HACKME_LISTGPU_TAG")
	}
	devs, err := gpupoh.GetAllGPUDevices()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	accs, derr := gpupoh.DiscoverAccelerators()
	usable := []map[string]string{}
	if derr != nil {
		fmt.Fprintln(os.Stderr, "discover:", derr)
	}
	for _, a := range accs {
		usable = append(usable, map[string]string{
			"label":   a.Label(),
			"name":    a.DeviceName(),
			"backend": a.Backend(),
		})
		_ = a.Close()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"build_hint": tag,
		"devices":    devs,
		"usable":     usable,
	})
	if derr != nil && len(usable) == 0 {
		os.Exit(2)
	}
}
