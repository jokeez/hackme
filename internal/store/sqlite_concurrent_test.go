package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestSQLiteConcurrentWritersStress hammers one DB with parallel writers (WAL + busy_timeout profile).
func TestSQLiteConcurrentWritersStress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS stress_kv (
		k TEXT PRIMARY KEY,
		v TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}

	const workers = 80
	const ops = 40
	var wg sync.WaitGroup
	errCh := make(chan error, workers*ops)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				k := fmt.Sprintf("w%d-i%d", id, i)
				v := strings.Repeat("x", 32)
				if _, err := db.Exec(`INSERT INTO stress_kv (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v); err != nil {
					if strings.Contains(strings.ToLower(err.Error()), "locked") {
						errCh <- fmt.Errorf("sqlite locked worker=%d op=%d: %w", id, i, err)
						return
					}
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}
