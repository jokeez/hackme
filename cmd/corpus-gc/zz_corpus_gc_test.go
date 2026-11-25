package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestClassify(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	files := []objectFile{
		{id: "aa11", kind: "snapshot", path: "/s/snapshot/aa/aa11", size: 1000, mod: now.Add(-72 * time.Hour)},
		{id: "bb22", kind: "seed", path: "/s/seed/bb/bb22", size: 2000, mod: now.Add(-72 * time.Hour)},
		{id: "cc33", kind: "snapshot", path: "/s/snapshot/cc/cc33", size: 4000, mod: now.Add(-1 * time.Minute)},
		{id: "dd44", kind: "expected", path: "/s/expected/dd/dd44", size: 8000, mod: now.Add(-72 * time.Hour)},
	}
	// aa11 and dd44 are still referenced; cc33 is unreferenced but was written a minute ago.
	refs := map[string]bool{"aa11": true, "dd44": true}
	p := classify(files, refs, now, 24*time.Hour)

	if p.Total != 4 || p.TotalBytes != 15000 {
		t.Fatalf("total = %d/%d, want 4/15000", p.Total, p.TotalBytes)
	}
	if p.Keep != 2 || p.KeepBytes != 9000 {
		t.Fatalf("kept %d/%d, want 2/9000", p.Keep, p.KeepBytes)
	}
	// An object is written before its marker is committed, so a fresh one must survive
	// even with nothing pointing at it yet.
	if p.Young != 1 || p.YoungBytes != 4000 {
		t.Fatalf("young %d/%d, want 1/4000 (cc33)", p.Young, p.YoungBytes)
	}
	if len(p.Garbage) != 1 || p.Garbage[0].id != "bb22" {
		t.Fatalf("garbage = %+v, want only bb22", p.Garbage)
	}
	if p.garbageBytes() != 2000 {
		t.Fatalf("garbage bytes = %d, want 2000", p.garbageBytes())
	}

	// With no references at all every old object looks collectable — which is exactly
	// why main refuses to delete in that case rather than trusting this result.
	empty := classify(files, map[string]bool{}, now, 24*time.Hour)
	if len(empty.Garbage) != 3 {
		t.Fatalf("with no refs, garbage = %d, want 3", len(empty.Garbage))
	}
}

// The writer code puts markers in three known columns. A GC built on that list would have
// missed fuzz_native_queue and fuzz_corpus_namespace, which the live database also holds —
// and a missed column means deleting a live campaign's corpus. Discovery must find a
// marker wherever it is, including in a column this test invents.
func TestCollectRefsDiscoversUnexpectedColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fuzz.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE fuzz_work_items (id INTEGER PRIMARY KEY, corpus_snapshot_json BLOB, expected_input_bytes BLOB)`,
		`CREATE TABLE fuzz_pool_corpus (input_u64 INTEGER, input_bytes BLOB)`,
		// A column no writer in the repo mentions today.
		`CREATE TABLE some_future_table (payload TEXT, note TEXT)`,
		// Untyped column: SQLite allows any value, so it must be scanned too.
		`CREATE TABLE untyped_table (whatever)`,
		`INSERT INTO fuzz_work_items (corpus_snapshot_json, expected_input_bytes) VALUES ('@obj:aaaa1111', '@obj:bbbb2222')`,
		`INSERT INTO fuzz_pool_corpus (input_u64, input_bytes) VALUES (1, '@obj:cccc3333')`,
		`INSERT INTO some_future_table (payload, note) VALUES ('@obj:dddd4444', 'not a marker')`,
		`INSERT INTO untyped_table (whatever) VALUES ('@obj:EEEE5555')`,
		// Inline payloads below the object threshold must not be mistaken for markers.
		`INSERT INTO fuzz_pool_corpus (input_u64, input_bytes) VALUES (2, 'plain bytes')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	refs := map[string]bool{}
	n, err := collectRefs(db, refs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"aaaa1111", "bbbb2222", "cccc3333", "dddd4444", "eeee5555"} {
		if !refs[want] {
			t.Errorf("missed marker %q — a GC would have deleted a referenced object", want)
		}
	}
	if len(refs) != 5 || n != 5 {
		t.Fatalf("refs = %d (n=%d), want 5: %v", len(refs), n, refs)
	}
	// Marker ids are compared against file names, which are lowercase hex.
	if refs["EEEE5555"] {
		t.Error("ids must be normalised to lower case")
	}
}

func TestWalkStore(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, body string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("snapshot/aa/aa11", "snap")
	write("seed/bb/bb22", "seedbytes")
	// A half-written object must never be classified or deleted.
	write("snapshot/cc/cc33.tmp", "partial")

	files, err := walkStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("walked %d files, want 2 (.tmp excluded): %+v", len(files), files)
	}
	kinds := map[string]string{}
	for _, f := range files {
		kinds[f.id] = f.kind
	}
	if kinds["aa11"] != "snapshot" || kinds["bb22"] != "seed" {
		t.Fatalf("kind taken from the wrong path segment: %v", kinds)
	}
}
