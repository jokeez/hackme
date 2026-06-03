package chain

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestValidateTransferShapeMemoByteLimit(t *testing.T) {
	base := TransferTx{
		TxType:        "transfer_v1",
		From:          "HMC-aaaaaaaaaaaaaaaa",
		To:            "HMC-bbbbbbbbbbbbbbbb",
		AmountUnits:   1,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: strings.Repeat("a", 64),
		SigEd25519:    strings.Repeat("b", 128),
	}

	// Exactly 256 UTF-8 bytes (ASCII).
	base.Memo = strings.Repeat("m", 256)
	if code, _ := ValidateTransferShape(base); code != "" {
		t.Fatalf("256-byte memo: want ok, got code=%q", code)
	}

	// 257 bytes must fail (byte length, not rune count).
	base.Memo = strings.Repeat("m", 257)
	if code, msg := ValidateTransferShape(base); code != "invalid_memo" {
		t.Fatalf("257-byte memo: want invalid_memo, got code=%q msg=%q", code, msg)
	}

	// Cyrillic: 128 runes × 2 bytes = 256 bytes OK.
	base.Memo = strings.Repeat("я", 128)
	if len(base.Memo) != 256 {
		t.Fatalf("cyrillic memo bytes=%d runes=%d", len(base.Memo), utf8.RuneCountInString(base.Memo))
	}
	if code, _ := ValidateTransferShape(base); code != "" {
		t.Fatalf("256-byte cyrillic memo: want ok, got code=%q", code)
	}

	// Emoji: 64 × 4 bytes = 256 bytes OK.
	base.Memo = strings.Repeat("😀", 64)
	if len(base.Memo) != 256 {
		t.Fatalf("emoji memo bytes=%d", len(base.Memo))
	}
	if code, _ := ValidateTransferShape(base); code != "" {
		t.Fatalf("256-byte emoji memo: want ok, got code=%q", code)
	}

	// 65 emoji = 260 bytes → reject.
	base.Memo = strings.Repeat("😀", 65)
	if code, _ := ValidateTransferShape(base); code != "invalid_memo" {
		t.Fatalf("260-byte emoji memo: want invalid_memo, got code=%q", code)
	}
}
