package fuzzengine_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/fuzzengine"
)

// asanlab is the repo's own deliberately buggy parser (tasks/sources/fuzz/selftest).
// These tests run it under real sanitizers instead of fixture strings, because the
// property that matters for payouts is measured on real output: one bug must collapse
// to one finding family however many different inputs reach it, and two different bugs
// must never share a family.

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root not found")
	return ""
}

func buildAsanlab(t *testing.T, sanitize string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root := repoRootFromTest(t)
	src := filepath.Join(root, "tasks", "sources", "fuzz", "selftest")
	for _, f := range []string{"asanlab.c", "asanlab_stdin.c", "asanlab.h"} {
		if _, err := os.Stat(filepath.Join(src, f)); err != nil {
			t.Skipf("selftest source missing: %v", err)
		}
	}
	bin := filepath.Join(t.TempDir(), "asanlab")
	args := []string{"-g", "-O1", "-fno-omit-frame-pointer", "-fsanitize=" + sanitize}
	if strings.Contains(sanitize, "undefined") {
		args = append(args, "-fno-sanitize-recover=undefined")
	}
	args = append(args, "-I", src,
		filepath.Join(src, "asanlab.c"),
		filepath.Join(src, "asanlab_stdin.c"),
		"-o", bin)
	cmd := exec.Command("clang", args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Skipf("clang build (%s) unavailable: %v: %s", sanitize, err, errb.String())
	}
	return bin
}

// runAsanlab feeds one input and returns the sanitizer output (stderr) and whether it crashed.
func runAsanlab(t *testing.T, bin string, input []byte) (string, bool) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(),
		"ASAN_OPTIONS=detect_leaks=1:abort_on_error=0:exitcode=66",
		"UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1")
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	return errb.String(), err != nil
}

func input(op byte, payload ...byte) []byte {
	return append([]byte{'A', 'L', op}, payload...)
}

func TestAsanlabMemoryBugsAreDetected(t *testing.T) {
	bin := buildAsanlab(t, "address")

	cases := []struct {
		name  string
		in    []byte
		wants string
	}{
		{"heap read", input(0x11, 0x7f, 0x03), "heap-buffer-overflow"},
		{"heap write", input(0x12, 0x7e, 0x05), "heap-buffer-overflow"},
		{"use after free", input(0x13, 0x55), "heap-use-after-free"},
		{"double free", input(0x14, 0x44), "double-free"},
		{"stack overflow", input(0x15, 0x33, 0x02), "stack-buffer-overflow"},
		{"global overflow", input(0x16, 0x22, 0x01), "global-buffer-overflow"},
		{"leak", input(0x17, 0x11), "detected memory leaks"},
		{"null deref", input(0x18, 0x00), "SEGV"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, crashed := runAsanlab(t, bin, tc.in)
			if !crashed {
				t.Fatalf("expected a sanitizer failure, got clean exit: %s", out)
			}
			if !strings.Contains(out, tc.wants) {
				t.Fatalf("want %q in sanitizer output, got:\n%s", tc.wants, out)
			}
		})
	}
}

func TestAsanlabUndefinedBehaviourIsDetected(t *testing.T) {
	bin := buildAsanlab(t, "undefined")

	cases := []struct {
		name  string
		in    []byte
		wants string
	}{
		{"signed overflow", input(0x19, 0x66), "signed integer overflow"},
		{"shift out of range", input(0x1a, 0x77, 0x09), "shift exponent"},
		{"division by zero", input(0x1b, 0x88, 0x00), "division by zero"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := runAsanlab(t, bin, tc.in)
			if !strings.Contains(out, tc.wants) {
				t.Fatalf("want %q in sanitizer output, got:\n%s", tc.wants, out)
			}
		})
	}
}

// The clean record path must stay clean: a finding there would mean the lab target
// itself is unreliable and every bucketing assertion below would be meaningless.
func TestAsanlabCleanPathDoesNotCrash(t *testing.T) {
	bin := buildAsanlab(t, "address")
	for _, in := range [][]byte{
		{},
		{'A'},
		{'A', 'L', 0x00, 0x01, 0x41},
		{'x', 'y', 0x02, 0x41, 0x42},
		bytes.Repeat([]byte{0x03, 0x41, 0x42, 0x43}, 64),
		input(0x11, 0x00, 0x00), // bug opcode, guard byte not satisfied
		input(0x14, 0x00),
		input(0x18, 0x01),
	} {
		out, crashed := runAsanlab(t, bin, in)
		if crashed {
			t.Fatalf("clean input %x crashed:\n%s", in, out)
		}
	}
}

// One bug reached by many different inputs must be one payable family; two different
// bugs must never collapse into one. This is the property a miner would farm.
func TestAsanlabCrashBucketsAreStablePerBug(t *testing.T) {
	bin := buildAsanlab(t, "address")

	// Same bug, several inputs: different offsets, addresses and allocation sites.
	sameBug := map[string][][]byte{
		"heap-read": {
			input(0x11, 0x7f, 0x00),
			input(0x11, 0x7f, 0x03),
			input(0x11, 0x7f, 0x07),
		},
		"use-after-free": {
			input(0x13, 0x55),
			append(input(0x13, 0x55), bytes.Repeat([]byte{0x41}, 32)...),
		},
		"stack-overflow": {
			input(0x15, 0x33, 0x00),
			input(0x15, 0x33, 0x03),
		},
	}
	bucketsPerBug := map[string]map[string]bool{}
	for bug, inputs := range sameBug {
		bucketsPerBug[bug] = map[string]bool{}
		for _, in := range inputs {
			out, crashed := runAsanlab(t, bin, in)
			if !crashed {
				t.Fatalf("%s: input %x did not crash:\n%s", bug, in, out)
			}
			b := fuzzengine.StableCrashBucket("crash", out)
			if strings.TrimSpace(b) == "" {
				t.Fatalf("%s: empty bucket for:\n%s", bug, out)
			}
			bucketsPerBug[bug][b] = true
		}
		if len(bucketsPerBug[bug]) != 1 {
			t.Fatalf("%s: %d buckets for the same bug, want 1: %v", bug, len(bucketsPerBug[bug]), bucketsPerBug[bug])
		}
	}

	// Distinct bugs must land in distinct families.
	seen := map[string]string{}
	for bug, buckets := range bucketsPerBug {
		for b := range buckets {
			if other, dup := seen[b]; dup {
				t.Fatalf("bugs %q and %q share bucket %s", bug, other, b)
			}
			seen[b] = bug
		}
	}

	// And the family counter must agree with that view.
	var msgs []string
	for _, bug := range []byte{0x11, 0x13, 0x15, 0x16} {
		var in []byte
		switch bug {
		case 0x11:
			in = input(0x11, 0x7f, 0x02)
		case 0x13:
			in = input(0x13, 0x55)
		case 0x15:
			in = input(0x15, 0x33, 0x01)
		case 0x16:
			in = input(0x16, 0x22, 0x02)
		}
		out, crashed := runAsanlab(t, bin, in)
		if !crashed {
			t.Fatalf("opcode %#x did not crash:\n%s", bug, out)
		}
		msgs = append(msgs, out)
	}
	if n := fuzzengine.CountFindingFamilies("crash", msgs); n != 4 {
		t.Fatalf("CountFindingFamilies = %d for four distinct bugs, want 4", n)
	}
}
