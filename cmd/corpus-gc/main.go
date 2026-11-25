// corpus-gc reports, and on request deletes, corpus objects that nothing references.
//
// The pool keeps large corpus payloads on disk in a content-addressed store
// ("<fuzzdb>.corpus-objects") and leaves a small "@obj:<sha256>" marker in SQLite. The
// store had write and read paths and no delete path, so every object the row retention
// trimmed a marker for stayed on disk forever. On 2026-11-25 that filled the hub's disk
// to 100%: the chain node entered a restart loop on "sqlite: disk I/O error", the public
// site answered 502, and the exchange could no longer verify its HMC/SUP reserves.
// Measured there afterwards: 307,397 objects / 27.40 GB, of which 145,010 / 11.35 GB were
// referenced by nothing.
//
// Safety, in order of how much it matters:
//
//   - Marker-bearing columns are DISCOVERED from the schema, not hardcoded. A list taken
//     from the writer code would have missed fuzz_native_queue and fuzz_corpus_namespace,
//     and a missed column means deleting a live campaign's corpus.
//   - Dry run is the default. Deleting needs -delete.
//   - It refuses to delete when the reachable set is empty, which is what being pointed at
//     the wrong database looks like.
//   - Objects younger than -min-age are kept regardless: an object is written before its
//     marker is committed, so a fresh one can look unreferenced for a moment.
//   - Pass every database that writes to the store. With HACKME_POOL_CORPUS_DIR set, more
//     than one can share it; the report prints each one's contribution so a zero is
//     visible rather than silent.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const markerPrefix = "@obj:"

type objectFile struct {
	id   string
	kind string
	path string
	size int64
	mod  time.Time
}

type plan struct {
	Total      int
	TotalBytes int64
	Keep       int
	KeepBytes  int64
	Young      int
	YoungBytes int64
	Garbage    []objectFile
}

func (p plan) garbageBytes() int64 {
	var n int64
	for _, g := range p.Garbage {
		n += g.size
	}
	return n
}

// classify splits the store into what is referenced, what is too young to judge, and what
// is collectable. Pure, so the decision rules are testable without a database.
func classify(files []objectFile, refs map[string]bool, now time.Time, minAge time.Duration) plan {
	var p plan
	for _, f := range files {
		p.Total++
		p.TotalBytes += f.size
		switch {
		case refs[f.id]:
			p.Keep++
			p.KeepBytes += f.size
		case now.Sub(f.mod) < minAge:
			p.Young++
			p.YoungBytes += f.size
		default:
			p.Garbage = append(p.Garbage, f)
		}
	}
	sort.Slice(p.Garbage, func(i, j int) bool { return p.Garbage[i].path < p.Garbage[j].path })
	return p
}

// textColumns lists table.column pairs that can hold a marker: a declared text/blob type,
// or no declared type at all (SQLite allows anything there).
func textColumns(db *sql.DB) ([][2]string, error) {
	rows, err := db.Query(`
		SELECT m.name, p.name, UPPER(COALESCE(p.type,''))
		  FROM sqlite_master m JOIN pragma_table_info(m.name) p
		 WHERE m.type='table' AND m.name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var table, col, typ string
		if err := rows.Scan(&table, &col, &typ); err != nil {
			return nil, err
		}
		if typ == "" || strings.Contains(typ, "CHAR") || strings.Contains(typ, "TEXT") ||
			strings.Contains(typ, "BLOB") || strings.Contains(typ, "CLOB") {
			out = append(out, [2]string{table, col})
		}
	}
	return out, rows.Err()
}

// collectRefs adds every marker id found anywhere in db to refs, and reports how many it
// contributed so an operator can see a database that turned out to hold none.
func collectRefs(db *sql.DB, refs map[string]bool) (int, error) {
	cols, err := textColumns(db)
	if err != nil {
		return 0, err
	}
	found := 0
	for _, tc := range cols {
		q := fmt.Sprintf(
			`SELECT DISTINCT SUBSTR(CAST("%s" AS TEXT), %d) FROM "%s" WHERE CAST("%s" AS TEXT) LIKE '%s%%'`,
			tc[1], len(markerPrefix)+1, tc[0], tc[1], markerPrefix)
		rows, err := db.Query(q)
		if err != nil {
			// A view or virtual table can refuse the scan; that is not a reason to treat
			// the store as unreferenced, so say so and keep going.
			fmt.Fprintf(os.Stderr, "[corpus-gc] WARN cannot scan %s.%s: %v\n", tc[0], tc[1], err)
			continue
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return found, err
			}
			id = strings.ToLower(strings.TrimSpace(id))
			if id != "" && !refs[id] {
				refs[id] = true
				found++
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return found, err
		}
	}
	return found, nil
}

func walkStore(dir string) ([]objectFile, error) {
	var out []objectFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		kind := "?"
		if rerr == nil {
			if parts := strings.Split(rel, string(os.PathSeparator)); len(parts) > 0 {
				kind = parts[0]
			}
		}
		out = append(out, objectFile{
			id:   strings.ToLower(d.Name()),
			kind: kind,
			path: path,
			size: info.Size(),
			mod:  info.ModTime(),
		})
		return nil
	})
	return out, err
}

func gb(n int64) string { return fmt.Sprintf("%.2f GB", float64(n)/(1<<30)) }

type dbList []string

func (d *dbList) String() string     { return strings.Join(*d, ",") }
func (d *dbList) Set(v string) error { *d = append(*d, v); return nil }

func main() {
	var dbs dbList
	dir := flag.String("dir", "", "corpus object store directory (required)")
	flag.Var(&dbs, "db", "database that may hold markers; repeat for each one")
	minAge := flag.Duration("min-age", 24*time.Hour, "never collect objects younger than this")
	doDelete := flag.Bool("delete", false, "actually delete (default: report only)")
	flag.Parse()

	if strings.TrimSpace(*dir) == "" || len(dbs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: corpus-gc -dir <store> -db <a.db> [-db <b.db>] [-min-age 24h] [-delete]")
		os.Exit(2)
	}
	if st, err := os.Stat(*dir); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "[corpus-gc] FAIL store is not a directory: %s\n", *dir)
		os.Exit(1)
	}

	refs := map[string]bool{}
	for _, path := range dbs {
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(30000)")
		if err != nil {
			// Under-counting references is how live objects get deleted: stop instead.
			fmt.Fprintf(os.Stderr, "[corpus-gc] FAIL open %s: %v\n", path, err)
			os.Exit(1)
		}
		n, err := collectRefs(db, refs)
		_ = db.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[corpus-gc] FAIL scan %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("[corpus-gc] %s → %d new marker ids\n", path, n)
	}

	files, err := walkStore(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[corpus-gc] FAIL walk %s: %v\n", *dir, err)
		os.Exit(1)
	}
	p := classify(files, refs, time.Now(), *minAge)

	byKind := map[string]int{}
	for _, g := range p.Garbage {
		byKind[g.kind]++
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	fmt.Printf("[corpus-gc] store      %d objects, %s\n", p.Total, gb(p.TotalBytes))
	fmt.Printf("[corpus-gc] referenced %d objects, %s (%d distinct marker ids)\n", p.Keep, gb(p.KeepBytes), len(refs))
	fmt.Printf("[corpus-gc] too young  %d objects, %s (< %s old)\n", p.Young, gb(p.YoungBytes), *minAge)
	fmt.Printf("[corpus-gc] collectable %d objects, %s\n", len(p.Garbage), gb(p.garbageBytes()))
	for _, k := range kinds {
		fmt.Printf("[corpus-gc]   %-10s %d\n", k, byKind[k])
	}

	if !*doDelete {
		fmt.Println("[corpus-gc] dry run — nothing deleted; pass -delete to collect")
		return
	}
	if len(refs) == 0 {
		fmt.Fprintln(os.Stderr, "[corpus-gc] REFUSING to delete: no markers found in any database given."+
			" That is what pointing at the wrong database looks like, and every object would be collected.")
		os.Exit(1)
	}
	var freed int64
	removed := 0
	for _, g := range p.Garbage {
		if err := os.Remove(g.path); err != nil {
			fmt.Fprintf(os.Stderr, "[corpus-gc] WARN remove %s: %v\n", g.path, err)
			continue
		}
		freed += g.size
		removed++
	}
	fmt.Printf("[corpus-gc] deleted %d objects, freed %s\n", removed, gb(freed))
}
