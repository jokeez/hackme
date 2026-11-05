package main

import (
	"strings"
	"testing"
)

func TestParseOptionalHMCAddressFormats(t *testing.T) {
	long := "HMC-" + strings.Repeat("ab", 32)
	for in, want := range map[string]string{
		"HMC-0123456789abcdef":   "HMC-0123456789abcdef",
		" HMC-0123456789abcdef ": "HMC-0123456789abcdef",
		long:                     long,
		"HMC-" + strings.ToUpper(strings.Repeat("ab", 32)): "",
		"HMC-" + strings.Repeat("ab", 31):                  "",
		"HMC-xyz":                                          "",
		"":                                                 "",
	} {
		if got := parseOptionalHMCAddress(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
