// orders-legacy-owner issues the Orders owner secret for a grandfathered pool campaign
// (published before owner-secret settle receipts) so the customer node that holds the
// escrow can drain its settle outbox and pay the miners.
//
// Why a hub-side ops tool: the normal path (authenticated publish refresh) now requires a
// live hub escrow for prepaid tickets, which a legacy campaign whose escrow lives on the
// customer node cannot present. The hub never dials the customer node for this.
//
// Safety: only campaigns with an EMPTY owner_ref (or the same owner_ref) and NO secret
// hash are touched; the secret is minted once (BEGIN IMMEDIATE) and written to a new file
// (0600, O_EXCL) — never printed. Hand it to the customer out of band.
//
//	orders-legacy-owner -db /opt/hackme/data/coordinator_fuzz.db \
//	  -campaign hunt-libarchive-20261111t110733z -owner-ref lab:founderb -out /root/secret.txt
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
	"hackme/internal/store"
)

func main() {
	dbPath := flag.String("db", "/opt/hackme/data/coordinator_fuzz.db", "coordinator fuzz DB")
	campaign := flag.String("campaign", "", "legacy campaign id")
	owner := flag.String("owner-ref", "", "owner_ref to bind (e.g. lab:founderb)")
	out := flag.String("out", "", "new file for the plaintext secret (0600, must not exist)")
	dry := flag.Bool("dry-run", false, "only report the campaign state")
	flag.Parse()
	if err := run(context.Background(), *dbPath, *campaign, *owner, *out, *dry); err != nil {
		fmt.Fprintln(os.Stderr, "orders-legacy-owner:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dbPath, campaign, owner, out string, dry bool) error {
	campaign, owner, out = strings.TrimSpace(campaign), strings.TrimSpace(owner), strings.TrimSpace(out)
	if campaign == "" || (!dry && (owner == "" || out == "")) {
		return errors.New("-campaign, -owner-ref and -out are required (-dry-run needs only -campaign)")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	pf := &poolfuzz.Service{DB: db}

	var curOwner string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(owner_ref,'') FROM fuzz_campaigns WHERE id=?`, campaign).Scan(&curOwner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("campaign %s not found", campaign)
		}
		return err
	}
	curOwner = strings.TrimSpace(curOwner)
	hash, err := pf.CampaignOwnerSecretHash(ctx, campaign)
	if err != nil {
		return err
	}
	var pending int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM fuzz_settle_outbox WHERE campaign_id=? AND status='pending'`, campaign).Scan(&pending)
	fmt.Printf("campaign=%s owner_ref=%q secret_issued=%v pending_settle=%d\n", campaign, curOwner, strings.TrimSpace(hash) != "", pending)
	if dry {
		return nil
	}
	if strings.TrimSpace(hash) != "" {
		return errors.New("owner secret already issued — refusing to mint another")
	}
	if curOwner != "" && curOwner != owner {
		return fmt.Errorf("campaign is bound to owner_ref %q, not %q", curOwner, owner)
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("secret file: %w", err)
	}
	defer f.Close()

	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	secret := hex.EncodeToString(b[:])
	if err := pf.StampCampaignOwnerRefIfEmpty(ctx, campaign, owner); err != nil {
		_ = os.Remove(out)
		return err
	}
	stamped, err := pf.StampOrdersOwnerSecretHash(ctx, campaign, poolsync.OwnerSecretHash(secret))
	if err != nil || !stamped {
		_ = os.Remove(out)
		if err == nil {
			err = errors.New("a secret was issued concurrently — not overwritten")
		}
		return err
	}
	if _, err := fmt.Fprintf(f, "campaign=%s\nowner_ref=%s\nowner_secret=%s\n", campaign, owner, secret); err != nil {
		return err
	}
	fmt.Printf("issued: owner_ref=%s secret written to %s (0600) — deliver out of band, then delete\n", owner, out)
	return nil
}
