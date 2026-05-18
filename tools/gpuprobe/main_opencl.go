//go:build opencl

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"hackme/internal/gpupoh"
)

func main() {
	list, err := gpupoh.GetAllGPUDevices()
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetAllGPUDevices:", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"devices": list,
	})

	accs, err := gpupoh.DiscoverAccelerators()
	if err != nil {
		fmt.Fprintln(os.Stderr, "DiscoverAccelerators:", err)
		os.Exit(1)
	}
	if len(accs) == 0 {
		fmt.Fprintln(os.Stderr, "DiscoverAccelerators: no usable accelerators. Per-device kernel init:")
		for _, line := range gpupoh.OpenCLAcceleratorInitDiagnostics() {
			fmt.Fprintln(os.Stderr, " ", line)
		}
		fmt.Fprintln(os.Stderr, "Hint: install `ocl-icd-opencl-dev` + a GPU ICD (Mesa/AMD); see README § OpenCL.")
		os.Exit(3)
	}
	for _, a := range accs {
		fmt.Fprintf(os.Stdout, "usable: %s | %s | backend=%s\n", a.Label(), a.DeviceName(), a.Backend())
		_ = a.Close()
	}
}
