package hunt

import (
	"fmt"
	"strings"
)

const stdinFuzzerWrapper = `/* Hunt template wrapper — stdin driver for LLVMFuzzerTestOneInput */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size);
extern int LLVMFuzzerInitialize(int *argc, char ***argv) __attribute__((weak));

int main(void) {
	static uint8_t buf[65536];
	size_t n = fread(buf, 1, sizeof(buf), stdin);
	if (n == 0) {
		return 0;
	}
	/* Exact-sized heap copy: a static buf[65536] hides ASAN OOB reads past the
	 * logical seed length (classic length-confusion / claim>body bugs). */
	uint8_t *heap = (uint8_t *)malloc(n ? n : 1);
	if (!heap) {
		return 2;
	}
	memcpy(heap, buf, n);
	if (LLVMFuzzerInitialize) {
		int argc = 1;
		char *argv0 = "harness";
		char **argv = &argv0;
		(void)LLVMFuzzerInitialize(&argc, &argv);
	}
	int rc = LLVMFuzzerTestOneInput(heap, n);
	free(heap);
	return rc;
}
`

// TemplatePreview describes whether a template wrapper is required.
type TemplatePreview struct {
	SourcePath     string `json:"source_path"`
	HasFuzzEntry   bool   `json:"has_fuzz_entry"`
	NeedsAccept    bool   `json:"needs_accept"`
	Summary        string `json:"summary"`
	WrapperPreview string `json:"wrapper_preview,omitempty"`
}

// PreviewTemplate inspects a source file for LLVMFuzzerTestOneInput.
func PreviewTemplate(repoRoot, pinPath, sourceRel string) (*TemplatePreview, error) {
	src, err := resolveSourceFile(repoRoot, pinPath, sourceRel)
	if err != nil {
		return nil, err
	}
	b, err := SafeReadFileUnder(pinPath, src)
	if err != nil {
		return nil, err
	}
	has := strings.Contains(string(b), inventoryMarker)
	out := &TemplatePreview{
		SourcePath:   sourceRel,
		HasFuzzEntry: has,
		NeedsAccept:  !has,
	}
	if has {
		out.Summary = "Reuse ready — file exports LLVMFuzzerTestOneInput (stdin wrapper used at build)."
		return out, nil
	}
	out.Summary = "No LLVMFuzzerTestOneInput — Hunt Standard requires template Accept to wrap stdin driver."
	out.WrapperPreview = "// After Accept, Hunt builds:\n" + stdinFuzzerWrapper + "\n// + your source: " + sourceRel
	return out, nil
}

func resolveSourceFile(repoRoot, pinPath, sourceRel string) (string, error) {
	pinPath = strings.TrimSpace(pinPath)
	sourceRel = strings.TrimSpace(sourceRel)
	if pinPath == "" || sourceRel == "" {
		return "", fmt.Errorf("hunt: pin path and source required")
	}
	abs, err := SafeJoinUnder(pinPath, sourceRel)
	if err != nil {
		return "", err
	}
	st, err := SafeStatUnder(pinPath, abs)
	if err != nil {
		return "", err
	}
	// Lstat via SafeStatUnder: refuse symlinks so clang cannot follow a pin-planted
	// link into host secrets during inventory/template compile (cdb092e twin).
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("hunt: source must be a regular file")
	}
	return abs, nil
}
