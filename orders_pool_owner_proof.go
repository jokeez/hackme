package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
)

// Fleet publish token alone must not spend another tenant's prepaid (rc20).
// Dig/Hunt/prepaid status require an Ed25519 proof that Owner-Ref is this node's wallet.

const ordersPoolOwnerProofTTLSec = 300

func ordersPoolOwnerProofMessage(owner string, ts int64) []byte {
	owner = chain.OrdersPrepaidNormalizeAddr(owner)
	return []byte(fmt.Sprintf("hackme-orders-pool-owner-v1\n%s\n%d", owner, ts))
}

func (a *app) signOrdersPoolOwnerProof(owner string) (pubHex, sigHex string, ts int64, err error) {
	if a == nil || a.signer == nil {
		return "", "", 0, fmt.Errorf("node signer unavailable")
	}
	owner = chain.OrdersPrepaidNormalizeAddr(owner)
	ts = time.Now().Unix()
	msg := ordersPoolOwnerProofMessage(owner, ts)
	pubHex = strings.TrimSpace(a.signer.PublicKeyHex())
	sigHex = strings.TrimSpace(a.signer.SignHex(msg))
	if pubHex == "" || sigHex == "" {
		return "", "", 0, fmt.Errorf("owner proof sign failed")
	}
	return pubHex, sigHex, ts, nil
}

func verifyOrdersPoolOwnerProof(owner, pubHex, sigHex string, ts int64) error {
	owner = chain.OrdersPrepaidNormalizeAddr(owner)
	pubHex = strings.TrimSpace(pubHex)
	sigHex = strings.TrimSpace(strings.ToLower(sigHex))
	if owner == "" || pubHex == "" || sigHex == "" || ts <= 0 {
		return fmt.Errorf("owner proof required (pubkey, sig, ts)")
	}
	now := time.Now().Unix()
	if ts > now+60 || now-ts > ordersPoolOwnerProofTTLSec {
		return fmt.Errorf("owner proof expired or skewed")
	}
	if !chain.PubKeyOwnsAddress(pubHex, owner) {
		return fmt.Errorf("owner proof pubkey does not match X-Hackme-Orders-Owner-Ref")
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("owner proof invalid pubkey")
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("owner proof invalid signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), ordersPoolOwnerProofMessage(owner, ts), sig) {
		return fmt.Errorf("owner proof bad signature")
	}
	return nil
}

func ordersPoolOwnerProofFromRequest(r *http.Request) (pubHex, sigHex string, ts int64) {
	pubHex = strings.TrimSpace(r.Header.Get("X-Hackme-Orders-Owner-PubKey"))
	sigHex = strings.TrimSpace(r.Header.Get("X-Hackme-Orders-Owner-Sig"))
	tsStr := strings.TrimSpace(r.Header.Get("X-Hackme-Orders-Owner-Ts"))
	if tsStr != "" {
		ts, _ = strconv.ParseInt(tsStr, 10, 64)
	}
	return pubHex, sigHex, ts
}

// requireOrdersPoolOwnerProof gates Dig/Hunt/prepaid so a leaked fleet publish token
// cannot burn another customer's prepaid by spoofing Owner-Ref.
func (a *app) requireOrdersPoolOwnerProof(w http.ResponseWriter, r *http.Request, owner string) bool {
	pub, sig, ts := ordersPoolOwnerProofFromRequest(r)
	if err := verifyOrdersPoolOwnerProof(owner, pub, sig, ts); err != nil {
		writeAPIError(w, http.StatusUnauthorized, "owner_proof_required", err.Error(), nil)
		return false
	}
	return true
}
