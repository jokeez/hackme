package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/sandbox"
)

const (
	modeDryRun = "dry-run"

	sourcePreset = "dig_preset"
	sourceWasm   = "custom_wasm"

	maxTitleRunes = 120
)

// Options is everything the action passes in. Paths are untrusted (they come
// from the calling workflow / PR tree) and are confined before use.
type Options struct {
	Mode       string
	Workspace  string // root the custom wasm must live under
	HackmeRoot string // hackme checkout (shipped packs live here)
	TmpDir     string // scratch for pack builds
	WasmPath   string
	DigPreset  string
	GuardPack  string
	Package    string
	Title      string
}

// WasmInfo describes the guard module without carrying its bytes.
type WasmInfo struct {
	Path   string `json:"path,omitempty"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Valid  bool   `json:"valid"`
	Error  string `json:"error,omitempty"`
}

// Plan is what the action would send to the pool, minus the wasm bytes.
type Plan struct {
	Mode       string         `json:"mode"`
	Source     string         `json:"source"`
	Endpoint   string         `json:"endpoint"`
	DigPreset  string         `json:"dig_preset,omitempty"`
	GuardPack  string         `json:"guard_pack,omitempty"`
	DigPackage string         `json:"dig_package"`
	Wasm       WasmInfo       `json:"wasm"`
	Payload    map[string]any `json:"payload"`
}

// OK reports whether the plan is safe to publish (wasm passed the pool gate).
func (p *Plan) OK() bool { return p != nil && p.Wasm.Valid }

// errUsage marks bad inputs (exit 2) as opposed to a failed wasm gate (exit 1).
var errUsage = errors.New("usage")

func usageErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, a...))
}

// BuildPlan resolves preset/pack/package, loads and validates the guard wasm
// with the same sandbox gate the pool uses, and assembles the security-audit
// payload. A wasm that fails the gate still yields a plan (Valid=false) so the
// PR comment can explain why; input errors return errUsage.
func BuildPlan(ctx context.Context, o Options) (*Plan, error) {
	mode := strings.TrimSpace(strings.ToLower(o.Mode))
	if mode == "" {
		mode = modeDryRun
	}
	if mode != modeDryRun {
		return nil, usageErr("mode %q is not supported yet (only %q)", o.Mode, modeDryRun)
	}
	wasmPath := strings.TrimSpace(o.WasmPath)
	presetName := strings.TrimSpace(o.DigPreset)
	packName := strings.TrimSpace(o.GuardPack)
	if wasmPath == "" && presetName == "" && packName == "" {
		return nil, usageErr("set wasm, dig-preset or guard-pack")
	}
	if wasmPath != "" && presetName != "" {
		return nil, usageErr("dig-preset runs a shipped guard; to fuzz your own wasm use wasm (+ optional guard-pack)")
	}

	var (
		pack     fuzzingcli.GuardPack
		havePack bool
		preset   fuzzingcli.DigPreset
	)
	if presetName != "" {
		p, err := fuzzingcli.DigPresetFor(presetName)
		if err != nil {
			return nil, usageErr("%v", err)
		}
		preset = p
		if packName != "" {
			gp, err := fuzzingcli.GuardPackFor(packName)
			if err != nil {
				return nil, usageErr("%v", err)
			}
			if gp.ID != preset.GuardPack {
				return nil, usageErr("dig-preset %q uses pack %q, not guard-pack %q", preset.ID, preset.GuardPack, gp.ID)
			}
		}
		packName = preset.GuardPack
	}
	if packName != "" {
		gp, err := fuzzingcli.GuardPackFor(packName)
		if err != nil {
			return nil, usageErr("%v", err)
		}
		pack, havePack = gp, true
	}

	pkgName := strings.TrimSpace(o.Package)
	if pkgName == "" {
		switch {
		case preset.DigPackage != "":
			pkgName = preset.DigPackage
		case havePack && pack.DefaultPackage != "":
			pkgName = pack.DefaultPackage
		default:
			pkgName = "scan"
		}
	}
	pkg, err := fuzzingcli.B2BPackageFor(pkgName)
	if err != nil {
		return nil, usageErr("%v", err)
	}
	if havePack {
		pkg = fuzzingcli.AdjustPackageForPack(pkg, pack)
	}

	plan := &Plan{
		Mode:       mode,
		Endpoint:   "POST /api/security-audit",
		DigPackage: pkg.Name,
	}
	if havePack {
		plan.GuardPack = pack.ID
	}

	var raw []byte
	if wasmPath != "" {
		plan.Source = sourceWasm
		plan.Wasm.Path = wasmPath
		raw, err = readWasmUnder(o.Workspace, wasmPath)
		if err != nil {
			return nil, usageErr("wasm: %v", err)
		}
	} else {
		plan.Source = sourcePreset
		if preset.ID != "" {
			plan.DigPreset = preset.ID
		}
		plan.Wasm.Path = pack.WasmRelPath
		raw, err = loadPackWasm(ctx, o.HackmeRoot, o.TmpDir, pack)
		if err != nil {
			return nil, err
		}
	}
	plan.Wasm.Bytes = len(raw)
	plan.Wasm.SHA256 = sha256Hex(raw)
	if verr := sandbox.ValidateCheckWasm(ctx, raw); verr != nil {
		plan.Wasm.Error = verr.Error()
	} else {
		plan.Wasm.Valid = true
	}

	plan.Payload = buildPayload(pkg, pack, havePack, plan, cleanTitle(o.Title, pack, havePack))
	return plan, nil
}

// buildPayload mirrors `hackme-fuzzing wizard` so the dry-run shows exactly
// what a real publish would send. The wasm itself is never embedded: presets
// let the server resolve its shipped guard, custom wasm is referenced by hash.
func buildPayload(pkg fuzzingcli.B2BPackage, pack fuzzingcli.GuardPack, havePack bool, plan *Plan, title string) map[string]any {
	payload := map[string]any{
		"title":            title,
		"depth_tier":       string(pkg.DepthTier),
		"budget_hmc":       pkg.BudgetHMC,
		"budget_runs":      pkg.BudgetRuns,
		"budget_seconds":   pkg.BudgetSeconds,
		"pool_distributed": pkg.PoolDistributed,
		"create_poh_order": pkg.CreatePoHOrder,
	}
	payload = fuzzingcli.ApplyDigEngineToPayload(payload, pkg)
	if havePack {
		cfg := fuzzingcli.ApplyPackConfig(map[string]any{}, pack)
		for _, k := range []string{"input_mode", "max_input_bytes", "guided_scheduling", "mutation_rounds", "seed_byte_corpus", "seed_corpus"} {
			if v, ok := cfg[k]; ok {
				payload[k] = v
			}
		}
		payload["guard_pack"] = pack.ID
		payload["guard_name"] = pack.ID
		if pack.InputMode == "bytes" {
			payload["depth_tier"] = string(fuzzengine.DepthBytesCorpus)
		}
	}
	if pkg.RewardHMC > 0 {
		payload["reward_hmc"] = pkg.RewardHMC
	}
	if plan.Source == sourcePreset && plan.DigPreset != "" {
		payload["dig_preset"] = plan.DigPreset
	} else if plan.Source == sourceWasm {
		payload["wasm_check_hex"] = fmt.Sprintf("<%d bytes, sha256:%s>", plan.Wasm.Bytes, plan.Wasm.SHA256)
	}
	return payload
}

// cleanTitle strips control/format characters and caps the length so a
// workflow-supplied title cannot smuggle newlines into logs or comments.
func cleanTitle(t string, pack fuzzingcli.GuardPack, havePack bool) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(t) {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		if n >= maxTitleRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	out := strings.TrimSpace(b.String())
	if out != "" {
		return out
	}
	if havePack {
		return "HackMe Dig · " + pack.Title
	}
	return "HackMe Dig · CI"
}
