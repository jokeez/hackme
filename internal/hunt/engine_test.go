package hunt

import (
	"runtime"
	"testing"
)

func TestParseHuntEngine(t *testing.T) {
	cases := map[string]string{
		"":          EngineLibFuzzer,
		"auto":      EngineLibFuzzer,
		"libfuzzer": EngineLibFuzzer,
		"aflpp":     EngineAFLPP,
		"afl++":     EngineAFLPP,
		"byte":      EngineByte,
		"legacy":    EngineByte,
		"nope":      EngineByte,
	}
	for in, want := range cases {
		if got := ParseHuntEngine(in); got != want {
			t.Fatalf("ParseHuntEngine(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsCoverageEngine(t *testing.T) {
	if !IsCoverageEngine("libfuzzer") || !IsCoverageEngine("aflpp") {
		t.Fatal("coverage")
	}
	if IsCoverageEngine("byte") {
		t.Fatal("byte not coverage")
	}
}

func TestEngineLeaseSecClamp(t *testing.T) {
	if got := clampEngineLeaseSec(1); got != MinEngineLeaseSec {
		t.Fatalf("min %d", got)
	}
	if got := clampEngineLeaseSec(9999); got != MaxEngineLeaseSec {
		t.Fatalf("max %d", got)
	}
}

func TestHuntEngineFromConfigExplicit(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE", "")
	cfg := map[string]any{"hunt_engine": "aflpp"}
	if got := HuntEngineFromConfig(cfg); got != EngineAFLPP {
		t.Fatalf("got %q", got)
	}
	cfg["hunt_engine"] = "byte"
	if got := HuntEngineFromConfig(cfg); got != EngineByte {
		t.Fatalf("byte got %q", got)
	}
}

func TestHuntEngineInventoryForcesLibFuzzer(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE", "")
	cfg := map[string]any{
		"hunt_source": "inventory", "hunt_package": "hunt_standard",
		"hunt_engine": "aflpp", "upstream_target_id": "inv_fuzz_parse_c",
	}
	if got := HuntEngineFromConfig(cfg); got != EngineLibFuzzer {
		t.Fatalf("inventory aflpp coerce got %q", got)
	}
	if got := EngineProfileFromConfig(cfg); got != EngineProfileFleet {
		t.Fatalf("inventory profile got %q", got)
	}
	ApplyCoverageEngineDefaults(cfg, "hunt_lite")
	if cfg["hunt_engine"] != EngineLibFuzzer || cfg["hunt_engine_profile"] != EngineProfileFleet {
		t.Fatalf("defaults %#v", cfg)
	}
}

func TestEngineProfileDeepFromPackage(t *testing.T) {
	if got := EngineProfileFromConfig(map[string]any{"hunt_package": "hunt_heavy"}); got != EngineProfileDeep {
		t.Fatalf("got %q", got)
	}
	if got := EngineProfileFromConfig(map[string]any{"hunt_package": "hunt_lite"}); got != EngineProfileFleet {
		t.Fatalf("lite got %q", got)
	}
}

func TestCoverageKindForEngine(t *testing.T) {
	if CoverageKindForEngine(EngineLibFuzzer) != "hunt_libfuzzer" {
		t.Fatal("lf")
	}
	if CoverageKindForEngine(EngineAFLPP) != "hunt_aflpp" {
		t.Fatal("afl")
	}
}

func TestRunnerAvailableByte(t *testing.T) {
	ok, reason := RunnerAvailable(EngineByte)
	if runtime.GOOS != "linux" {
		if ok {
			t.Fatalf("byte must be unavailable on %s: %s", runtime.GOOS, reason)
		}
		return
	}
	if !BubblewrapAvailable() || !BubblewrapWorking() {
		if ok {
			t.Fatalf("byte must be unavailable without working bwrap: %s", reason)
		}
		return
	}
	if !ok || reason != "" {
		t.Fatalf("byte should be available with linux+bwrap: %v %s", ok, reason)
	}
}

func TestRunnerAvailableCoverageEnginesLinuxOnly(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("linux host — coverage engines may be available")
	}
	if ok, reason := RunnerAvailable(EngineLibFuzzer); ok {
		t.Fatalf("libfuzzer must be unavailable on %s (%s)", runtime.GOOS, reason)
	}
	if ok, reason := RunnerAvailable(EngineAFLPP); ok {
		t.Fatalf("aflpp must be unavailable on %s (%s)", runtime.GOOS, reason)
	}
}

func TestHuntEngineFromConfigMonochromePerCampaign(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE", "")
	t.Setenv("HACKME_HUNT_ENGINE_DEEP_AFL", "")
	cfg := map[string]any{"hunt_package": "hunt_standard", "hunt_source": "catalog"}
	e1 := HuntEngineFromConfig(cfg)
	e2 := HuntEngineFromConfig(cfg)
	if e1 != e2 || e1 != EngineLibFuzzer {
		t.Fatalf("campaign engine must be stable libfuzzer, got %q/%q", e1, e2)
	}
}
