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
	withV2CompiledDefaults(t)
	setV2HeightEnv(t, "777")
	if got := EconomicsV2ActivationHeight(); got != 777 {
		t.Fatalf("env height %d", got)
	}
	setV2HeightEnv(t, "")
	if got := EconomicsV2ActivationHeight(); got != DefaultEconomicsV2ActivationHeight {
		t.Fatalf("default height %d want %d", got, DefaultEconomicsV2ActivationHeight)
	}
}

// withV2CompiledDefaults runs a test with the compiled announce defaults in effect.
func withV2CompiledDefaults(t *testing.T) {
	t.Helper()
	SetV2CompiledDefaultsOffForTest(false)
	t.Cleanup(func() { SetV2CompiledDefaultsOffForTest(true) })
}

func setV2HeightEnv(t *testing.T, h string) {
	t.Helper()
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", h)
	ResetEconomicsV2ActivationHeightForTest()
}

// The announce release: height 550 000 and the hub's block key, with no env at all.
func TestV2CompiledDefaultsAnnounceValues(t *testing.T) {
	withV2CompiledDefaults(t)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "")
	t.Setenv("HACKME_CHAIN_AUTHORITY_PUBKEY", "")
	ResetEconomicsV2ActivationHeightForTest()
	if got := EconomicsV2ActivationHeight(); got != 550_000 {
		t.Fatalf("height %d", got)
	}
	if EconomicsV2ActiveForIndex(549_999) || !EconomicsV2ActiveForIndex(550_000) {
		t.Fatal("activation boundary")
	}
	if got := authorityPubKeyFromEnv(); got != DefaultAuthorityPubKey || !validAuthorityPubKeyHex(got) {
		t.Fatalf("authority %q", got)
	}
}
