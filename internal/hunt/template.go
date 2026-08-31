package hunt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stdinFuzzerWrapper = `/* Hunt template wrapper — stdin driver for LLVMFuzzerTestOneInput */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

extern int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size);

int main(void) {
	static uint8_t buf[65536];
	size_t n = fread(buf, 1, sizeof(buf), stdin);
	if (n == 0) {
		return 0;
	}
	return LLVMFuzzerTestOneInput(buf, n);
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
	b, err := os.ReadFile(src)
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
	abs := filepath.Clean(filepath.Join(pinPath, sourceRel))
	if !strings.HasPrefix(abs, filepath.Clean(pinPath)+string(os.PathSeparator)) && abs != filepath.Clean(pinPath) {
		return "", fmt.Errorf("hunt: source escapes pin root")
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("hunt: source must be a file")
	}
	return abs, nil
}
