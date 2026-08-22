package fuzznative

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzupstream"
)

const ossReproTimeout = 45 * time.Second

// ReproModeOssStdin runs pinned OSS CVE stdin drivers (ASAN + upstream sources).
const ReproModeOssStdin ReproMode = "oss_upstream"

func evalReproOssStdin(upstreamTarget, guardName string, input []byte, repoRoot string) (ReproResult, bool) {
	targetID := resolveOssTargetID(upstreamTarget, guardName)
	inHex := hexEncode(input)
	res := ReproResult{
		Status:         StatusSkipped,
		UpstreamTarget: targetID,
		InputHex:       inHex,
		Note:           "oss_upstream repro skipped",
	}
	if targetID == "" {
		res.Note = "oss_upstream: missing upstream/parser target"
		return res, false
	}
	if repoRoot == "" {
		repoRoot = "."
	}
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		res.Note = "oss_upstream: " + err.Error()
		return res, false
	}
	tgt, err := manifest.TargetByID(targetID)
	if err != nil {
		res.Note = err.Error()
		return res, false
	}
	res.Harness = tgt.Driver
	if tgt.Repo != "" {
		res.UpstreamCommit = strings.TrimSpace(tgt.Ref)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ossReproTimeout)
	defer cancel()
	binPath, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, tgt)
	if err != nil {
		res.Note = "oss_upstream build: " + err.Error()
		return res, false
	}
	maxInput := manifest.Defaults.MaxInputBytes
	if maxInput <= 0 {
		maxInput = 65536
	}
	crash, sanitizer, tail, err := fuzzupstream.RunInput(ctx, binPath, input, maxInput)
	if err != nil {
		res.Note = "oss_upstream run: " + err.Error()
		return res, false
	}
	res.SanitizerTail = tail
	if crash && fuzzupstream.IsSecuritySanitizer(sanitizer) {
		res.Status = StatusNativeCrash
		res.NativeSignal = true
		res.StableCrashKey = fuzzengine.StableCrashBucket("oss_asan", sanitizer+"|"+tail)
		res.Note = fmt.Sprintf("ASAN crash on %s (%s) — triage before CVE/disclosure", targetID, tgt.Driver)
		return res, true
	}
	if crash {
		res.Status = StatusRejected
		res.Note = fmt.Sprintf("non-security sanitizer on %s (%s) — not bounty-class", targetID, sanitizer)
		return res, true
	}
	res.Status = StatusRejected
	res.NativeSignal = false
	res.Note = fmt.Sprintf("wasm/parser signal not reproduced on native %s ASAN harness", targetID)
	return res, true
}

func resolveOssTargetID(upstreamTarget, guardName string) string {
	if id := strings.TrimSpace(strings.ToLower(upstreamTarget)); id != "" {
		if id == "oss" {
			return ossTargetFromGuard(guardName)
		}
		return id
	}
	return ossTargetFromGuard(guardName)
}

func ossTargetFromGuard(guardName string) string {
	g := strings.TrimSpace(strings.ToLower(guardName))
	switch {
	case g == "":
		return ""
	case g == "parser_expat", strings.Contains(g, "expat"):
		return "expat"
	case g == "parser_md4c", strings.Contains(g, "md4c"):
		return "md4c"
	case g == "parser_cjson", strings.Contains(g, "cjson"):
		return "cjson"
	default:
		if strings.HasPrefix(g, "parser_") {
			return strings.TrimPrefix(g, "parser_")
		}
		return g
	}
}

func hexEncode(input []byte) string {
	const hex = "0123456789abcdef"
	if len(input) == 0 {
		return ""
	}
	out := make([]byte, len(input)*2)
	for i, b := range input {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
}
