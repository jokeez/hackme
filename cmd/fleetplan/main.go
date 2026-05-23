// fleetplan — print JSON GPU worker fleet plan (CUDA / OpenCL / hybrid).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"hackme/internal/gpuhost"
)

func main() {
	repo := flag.String("repo", "", "repo root (bin/workerpoh-*)")
	worker := flag.String("worker", "worker-local", "base worker id")
	flag.Parse()
	root := *repo
	if root == "" {
		root = os.Getenv("HACKME_REPO_ROOT")
	}
	if root == "" {
		root = "."
	}
	plan := gpuhost.DefaultFleetPlanFromEnv(root, *worker)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(plan); err != nil {
		fmt.Fprintf(os.Stderr, "fleetplan: %v\n", err)
		os.Exit(1)
	}
}
