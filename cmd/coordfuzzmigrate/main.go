// One-shot copy of fuzz tables from a full coordinator DB into a fuzz-only DB.
// Used by scripts/ops/migrate_coordinator_fuzz_db.sh (preferred path over raw SQL rewrite).
//
//	go run ./cmd/coordfuzzmigrate -src data/coordinator.db -dst data/coordinator_fuzz.db
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	"hackme/internal/store"

	_ "modernc.org/sqlite"
)

var fuzzTables = []string{
	"fuzz_campaigns",
	"fuzz_work_items",
	"fuzz_findings",
	"fuzz_corpus",
	"fuzz_pool_corpus",
	"fuzz_runtime_samples",
	"fuzz_coverage_seen",
	"fuzz_report_access_log",
	"fuzz_native_queue",
	"fuzz_campaign_escrow",
	"fuzz_settle_outbox",
	"fuzz_settle_applied",
}

func main() {
	src := flag.String("src", "", "source coordinator.db (full)")
	dst := flag.String("dst", "", "destination coordinator_fuzz.db")
	flag.Parse()
	if strings.TrimSpace(*src) == "" || strings.TrimSpace(*dst) == "" {
		fmt.Fprintln(os.Stderr, "usage: coordfuzzmigrate -src coordinator.db -dst coordinator_fuzz.db")
		os.Exit(2)
	}
	if _, err := os.Stat(*src); err != nil {
		fmt.Fprintf(os.Stderr, "src: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(*dst); err == nil {
		fmt.Fprintf(os.Stderr, "dst already exists: %s (refuse overwrite)\n", *dst)
		os.Exit(1)
	}

	fuzzDB, err := store.OpenFuzz(*dst)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenFuzz: %v\n", err)
		os.Exit(1)
	}
	defer fuzzDB.Close()

	if _, err := fuzzDB.Exec(fmt.Sprintf("ATTACH DATABASE %s AS src", quotePath(*src))); err != nil {
		fmt.Fprintf(os.Stderr, "ATTACH: %v\n", err)
		os.Exit(1)
	}

	for _, t := range fuzzTables {
		var n int
		err := fuzzDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM src.sqlite_master WHERE type='table' AND name=%s", quoteIdent(t))).Scan(&n)
		if err != nil || n == 0 {
			fmt.Printf("skip missing table %s\n", t)
			continue
		}
		cols, err := commonColumns(fuzzDB, "main", t, "src", t)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s columns: %v\n", t, err)
			os.Exit(1)
		}
		if len(cols) == 0 {
			fmt.Fprintf(os.Stderr, "%s: no common columns\n", t)
			os.Exit(1)
		}
		colList := strings.Join(cols, ", ")
		q := fmt.Sprintf("INSERT INTO main.%s (%s) SELECT %s FROM src.%s", t, colList, colList, t)
		res, err := fuzzDB.Exec(q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "copy %s: %v\n", t, err)
			os.Exit(1)
		}
		aff, _ := res.RowsAffected()
		var srcCount, dstCount int
		_ = fuzzDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM src.%s", t)).Scan(&srcCount)
		_ = fuzzDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM main.%s", t)).Scan(&dstCount)
		if srcCount != dstCount {
			fmt.Fprintf(os.Stderr, "ERROR %s: src=%d dst=%d (inserted=%d)\n", t, srcCount, dstCount, aff)
			os.Exit(1)
		}
		fmt.Printf("ok %s: %d rows\n", t, dstCount)
	}

	// Preserve AUTOINCREMENT counters when present on source.
	_, _ = fuzzDB.Exec(`
		INSERT INTO main.sqlite_sequence(name, seq)
		SELECT name, seq FROM src.sqlite_sequence
		WHERE name IN (
			'fuzz_settle_outbox','fuzz_work_items','fuzz_findings','fuzz_corpus',
			'fuzz_runtime_samples','fuzz_coverage_seen','fuzz_report_access_log','fuzz_native_queue'
		)
		ON CONFLICT(name) DO UPDATE SET seq=excluded.seq
	`)

	if _, err := fuzzDB.Exec("DETACH DATABASE src"); err != nil {
		fmt.Fprintf(os.Stderr, "DETACH: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("migrate ok")
}

func quotePath(p string) string {
	return "'" + strings.ReplaceAll(p, "'", "''") + "'"
}

func quoteIdent(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func tableColumns(db *sql.DB, schema, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA %s.table_info(%s)", schema, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func commonColumns(db *sql.DB, schemaA, tableA, schemaB, tableB string) ([]string, error) {
	a, err := tableColumns(db, schemaA, tableA)
	if err != nil {
		return nil, err
	}
	b, err := tableColumns(db, schemaB, tableB)
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, c := range b {
		set[c] = struct{}{}
	}
	var out []string
	for _, c := range a {
		if _, ok := set[c]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
