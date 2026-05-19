//go:build !opencl && !cuda

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "gpuprobe: this build has no OpenCL/CUDA. Run:")
	fmt.Fprintln(os.Stderr, "  CGO_ENABLED=1 go run -tags opencl ./tools/gpuprobe")
	fmt.Fprintln(os.Stderr, "Expect GPU \"name\" in JSON when OpenCL ICD is installed.")
	os.Exit(2)
}
