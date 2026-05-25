package main

import (
	"os"
	"strconv"
	"strings"

	"hackme/internal/integrator"
)

func integratorSelfRegisterEnabled() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_INTEGRATOR_SELF_REGISTER"))
	if v == "" {
		return true // default on for B2B automatic issuance
	}
	return !strings.EqualFold(v, "0") && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "off")
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
