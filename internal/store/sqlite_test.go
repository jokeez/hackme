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
