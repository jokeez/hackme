package fuzzupstream

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestBuildDeepHarnessVariants(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root := repoRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	built := 0
	for _, tgt := range m.Targets {
		if tgt.DeepDriver == "" {
			continue
		}
		tgt := ApplyHarnessVariant(tgt, HarnessVariantDeepV1)
		t.Run(tgt.ID+"_deep_v1", func(t *testing.T) {
			driverSrc := DriverSourcePath(root, tgt)
			if _, err := os.Stat(driverSrc); err != nil {
				t.Fatalf("deep driver missing: %s", driverSrc)
			}
			bin, _, err := BuildTarget(ctx, root, tgt)
			if err != nil {
				t.Fatal(err)
			}
			if st, err := os.Stat(bin); err != nil || st.Size() == 0 {
				t.Fatalf("binary: %v", err)
			}
			// Smoke: empty stdin must not ASAN-crash.
			crash, _, _, err := RunInput(ctx, bin, nil, 65536)
			if err != nil {
				t.Fatalf("run empty: %v", err)
			}
			if crash {
				t.Fatal("empty input must not crash deep harness")
			}
			seed := []byte{0x80}
			_, _, _, _ = RunInput(ctx, bin, seed, 65536)
		})
		built++
	}
	if built == 0 {
		t.Fatal("expected at least one deep_driver target in manifest")
	}
}
