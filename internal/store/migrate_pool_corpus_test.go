package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateFuzzPoolCorpusIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrate-pool.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assertTable(t, db, "fuzz_pool_corpus")
	assertColumn(t, db, "fuzz_work_items", "expected_input_bytes")
	assertColumn(t, db, "fuzz_work_items", "expected_input_locked")
	assertColumn(t, db, "fuzz_work_items", "corpus_snapshot_json")
	assertColumn(t, db, "fuzz_work_items", "corpus_snapshot_sha256")
	assertColumn(t, db, "fuzz_pool_corpus", "input_bytes")

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db2.Close()
}

func assertTable(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("table %s missing", name)
	}
}

func assertColumn(t *testing.T, db *sql.DB, table, col string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == col {
			return
		}
	}
	t.Fatalf("column %s.%s missing", table, col)
}
