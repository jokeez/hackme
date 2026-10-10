package fuzzupstream

import (
	"os"
	"testing"
)

func TestApplyHarnessVariantDeep(t *testing.T) {
	base := Target{ID: "mpack", Driver: "mpack_stdin", DeepDriver: "mpack_deep_stdin"}
	deep := ApplyHarnessVariant(base, HarnessVariantDeepV1)
	if deep.Driver != "mpack_deep_stdin" || deep.HarnessVariant != HarnessVariantDeepV1 {
		t.Fatalf("deep=%+v", deep)
	}
	shallow := ApplyHarnessVariant(base, HarnessVariantShallow)
	if shallow.Driver != "mpack_stdin" || shallow.HarnessVariant != HarnessVariantShallow {
		t.Fatalf("shallow=%+v", shallow)
	}
}

func TestApplyHarnessVariantNoDeepDriver(t *testing.T) {
	base := Target{ID: "jsmn", Driver: "jsmn_stdin"}
	got := ApplyHarnessVariant(base, HarnessVariantDeepV1)
	if got.Driver != "jsmn_stdin" {
		t.Fatalf("should keep shallow driver: %+v", got)
	}
}

func TestHarnessVariantFromEnv(t *testing.T) {
	t.Setenv("HACKME_OSS_HARNESS_VARIANT", "")
	if HarnessVariantFromEnv() != HarnessVariantShallow {
		t.Fatal(HarnessVariantFromEnv())
	}
	t.Setenv("HACKME_OSS_HARNESS_VARIANT", "deep_v1")
	if HarnessVariantFromEnv() != HarnessVariantDeepV1 {
		t.Fatal(HarnessVariantFromEnv())
	}
	_ = os.Unsetenv("HACKME_OSS_HARNESS_VARIANT")
}
