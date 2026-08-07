package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestOpenSetsUserVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mig.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != CurrentSchemaVersion {
		t.Fatalf("PRAGMA user_version = %d, want %d", v, CurrentSchemaVersion)
	}
}

func TestOpenMigratesEconomicsFloatMetaToUnits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate legacy DB state before v6 economics-units migration.
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES('econ_total_minted_hmc','12.345678') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES('econ_total_burned_hmc','1.234567') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM meta WHERE key IN ('econ_total_minted_units','econ_total_burned_units')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	var mintedUnits, burnedUnits string
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='econ_total_minted_units'`).Scan(&mintedUnits); err != nil {
		t.Fatal(err)
	}
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='econ_total_burned_units'`).Scan(&burnedUnits); err != nil {
		t.Fatal(err)
	}
	if mintedUnits != "1234567800" {
		t.Fatalf("minted units mismatch: got %s want 1234567800", mintedUnits)
	}
	if burnedUnits != "123456700" {
		t.Fatalf("burned units mismatch: got %s want 123456700", burnedUnits)
	}

	var unitsPerHMC string
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='units_per_hmc'`).Scan(&unitsPerHMC); err != nil {
		t.Fatal(err)
	}
	if unitsPerHMC != fmt.Sprintf("%d", int64(100_000_000)) {
		t.Fatalf("units_per_hmc mismatch: got %s", unitsPerHMC)
	}
}

func TestFuzzNativeQueueMigration(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "fuzz_native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='fuzz_native_queue'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "fuzz_native_queue" {
		t.Fatalf("table %q", name)
	}
}

func TestOpenFuzzCreatesFuzzTablesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fuzz_only.db")
	db, err := OpenFuzz(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	want := []string{
		"fuzz_campaigns",
		"fuzz_work_items",
		"fuzz_findings",
		"fuzz_campaign_escrow",
		"fuzz_native_queue",
		"fuzz_settle_outbox",
		"fuzz_settle_applied",
	}
	for _, table := range want {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	for _, chain := range []string{"blocks", "wallet", "tasks"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, chain).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("OpenFuzz must not create %s", chain)
		}
	}
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != CurrentSchemaVersion {
		t.Fatalf("user_version=%d want %d", v, CurrentSchemaVersion)
	}
	// Smoke: insert campaign without chain tables.
	_, err = db.Exec(`INSERT INTO fuzz_campaigns(
		id, campaign_type, status, title, created_at
	) VALUES (?,?,?,?,?)`,
		"c1", "marketplace", "queued", "t", 1)
	if err != nil {
		t.Fatalf("insert fuzz_campaigns: %v", err)
	}
}
