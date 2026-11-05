package chain

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// Address formats.
//
//   - legacy: "HMC-" + first 16 hex of sha256(pubkey). Spends also need the address/key binding
//     (pubkey_binding.go) from the Economics v2 height.
//   - long:   "HMC-" + all 64 hex of sha256(pubkey). The address is the full key hash, so it
//     needs no binding. Valid as sender and recipient from the Economics v2 height.
//
// From that height every transfer recipient must be one of the two formats in lowercase hex:
// an uppercase or malformed recipient is a different, unspendable account string, so funds sent
// there would be stranded. There is no automatic migration: an owner moves funds from the legacy
// address to the long address of the same key with an ordinary transfer, when it suits them.

const (
	addressPrefix        = "HMC-"
	legacyAddressHexLen  = 16
	longAddressHexLen    = 2 * sha256.Size
	legacyAddressFullLen = len(addressPrefix) + legacyAddressHexLen
	longAddressFullLen   = len(addressPrefix) + longAddressHexLen
)

// ErrAddressFormatNotActive: long addresses are accepted from the Economics v2 height only.
var ErrAddressFormatNotActive = errors.New("chain: long HMC addresses are accepted from the Economics v2 height")

// IsLegacyAddress reports "HMC-" + 16 lowercase hex.
func IsLegacyAddress(a string) bool {
	return len(a) == legacyAddressFullLen && strings.HasPrefix(a, addressPrefix) && isLowerHex(a[len(addressPrefix):])
}

// IsLongAddress reports "HMC-" + 64 lowercase hex.
func IsLongAddress(a string) bool {
	return len(a) == longAddressFullLen && strings.HasPrefix(a, addressPrefix) && isLowerHex(a[len(addressPrefix):])
}

// IsValidAddress reports either canonical format.
func IsValidAddress(a string) bool {
	return IsLegacyAddress(a) || IsLongAddress(a)
}

// LongAddressesActiveForIndex reports whether long addresses are valid in block idx.
func LongAddressesActiveForIndex(idx uint64) bool {
	return EconomicsV2ActiveForIndex(idx)
}

// LongAddressFromPubKeyHex returns "HMC-" + sha256(pubkey) in full.
func LongAddressFromPubKeyHex(pubHex string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return "", errors.New("invalid pubkey")
	}
	sum := sha256.Sum256(raw)
	return addressPrefix + hex.EncodeToString(sum[:]), nil
}

// pubKeyOwnsAddress reports whether addr is the legacy or the long address of pubHex. Whether a
// long address is allowed at a given height is checked separately (checkAddressRulesForIndex).
func pubKeyOwnsAddress(pubHex, addr string) bool {
	if legacy, err := addressFromPubKeyHex(pubHex); err == nil && legacy == addr {
		return true
	}
	if long, err := LongAddressFromPubKeyHex(pubHex); err == nil && long == addr {
		return true
	}
	return false
}

// checkAddressRulesForIndex applies the height-dependent address rules to a spend in block idx.
// to == "" for spends without a recipient (burns).
func checkAddressRulesForIndex(idx uint64, from, to string) (string, string) {
	active := LongAddressesActiveForIndex(idx)
	if !active {
		if IsLongAddress(from) || IsLongAddress(to) {
			return "address_format_not_active", ErrAddressFormatNotActive.Error()
		}
		return "", ""
	}
	if !IsValidAddress(from) {
		return "invalid_address", "from must be HMC- + 16 or 64 lowercase hex"
	}
	if to != "" && !IsValidAddress(to) {
		return "invalid_address", "to must be HMC- + 16 or 64 lowercase hex"
	}
	return "", ""
}

// checkSpendRulesQ is every height-dependent rule for a spend applied in block idx: address
// formats, then the legacy address/key binding.
func checkSpendRulesQ(ctx context.Context, q queryRowContext, idx uint64, from, to, pubKeyHex string) (string, string) {
	if code, msg := checkAddressRulesForIndex(idx, from, to); code != "" {
		return code, msg
	}
	return checkPubKeyBindingQ(ctx, q, idx, from, pubKeyHex)
}

// checkSpendRulesNextQ checks a submitted spend against the next block's rules.
func (s *Service) checkSpendRulesNextQ(ctx context.Context, q queryRowContext, from, to, pubKeyHex string) (string, string) {
	idx, err := s.nextBlockIndexQ(ctx, q)
	if err != nil {
		return "internal_error", err.Error()
	}
	return checkSpendRulesQ(ctx, q, idx, from, to, pubKeyHex)
}
