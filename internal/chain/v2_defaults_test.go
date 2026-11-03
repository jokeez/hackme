package chain

import "testing"

// The announce release fills both compiled defaults together: a height without the hub key
// would let followers start v2 with no authority to check (they fail closed), and a key alone
// does nothing. Also the height must be past ASERT and not a vanity tiny value.
func TestV2CompiledDefaultsConsistent(t *testing.T) {
	h, k := DefaultEconomicsV2ActivationHeight, DefaultAuthorityPubKey
	if h == 0 && k == "" {
		return // not announced yet
	}
	if h == 0 || !validAuthorityPubKeyHex(k) {
		t.Fatalf("announce release must set both: height=%d authority=%q", h, k)
	}
	if h <= defaultPoHASERTActivationHeight {
		t.Fatalf("v2 height %d must be after ASERT %d", h, defaultPoHASERTActivationHeight)
	}
}

// Env still overrides the compiled default (labs), and locked meta beats both.
func TestV2HeightPreferenceOrder(t *testing.T) {
	setV2HeightEnv(t, "777")
	if got := EconomicsV2ActivationHeight(); got != 777 {
		t.Fatalf("env height %d", got)
	}
	setV2HeightEnv(t, "")
	if got := EconomicsV2ActivationHeight(); got != DefaultEconomicsV2ActivationHeight {
		t.Fatalf("default height %d want %d", got, DefaultEconomicsV2ActivationHeight)
	}
}

func setV2HeightEnv(t *testing.T, h string) {
	t.Helper()
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", h)
	ResetEconomicsV2ActivationHeightForTest()
}
