package main

import (
	"os"
	"strconv"
	"strings"

	"hackme/internal/integrator"
)

func integratorSelfRegisterEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_INTEGRATOR_SELF_REGISTER")))
	// Fail-closed: empty/unset → OFF. Opt-in only via 1/true/on/yes.
	if v == "" {
		return false
	}
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

func integratorMaxActive() int {
	v := strings.TrimSpace(os.Getenv("HACKME_INTEGRATOR_MAX_TOKENS"))
	if v == "" {
		return 200
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 200
	}
	return n
}

func integratorTokenValid(plain string) bool {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return false
	}
	if developerTokenFromEnv() != "" && secretsEqualConstantTime(plain, developerTokenFromEnv()) {
		return true
	}
	if integratorStore != nil && integratorStore.Validate(plain) {
		return true
	}
	return false
}

// integratorStore is opened from app.dataDir in main.
var integratorStore *integrator.Store
