package main

// Payout-lock lifecycle hardening on top of the report #27 durable binding:
//   1. admin unbind surfaces a durable DELETE failure instead of ok:true
//   2. idle rows are GC-ed (seen_at backfilled, first address never rewritten)
//   3. startup load is bounded newest-first (durable fallback keeps evicted
//      bindings enforced)
//   4. admin unbind accepts worker_ids[] for fleet retirement

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/store"
)

func payoutLockSetup(t *testing.T) (*sql.DB, *workManager, *http.ServeMux) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "pl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wm := &workManager{worker: map[string]workerPayoutStat{}}
	wm.attachDedupDB(db)
	mux := http.NewServeMux()
	addWorkRoutes(mux, "admin-tok", "worker-tok", false, nil, wm, nil)
	return db, wm, mux
}

func payoutLockPost(t *testing.T, mux *http.ServeMux, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/work/admin/unbind-payout-lock", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func payoutLockAddr(t *testing.T, db *sql.DB, workerID string) (string, bool) {
	t.Helper()
	var addr string
	err := db.QueryRow(`SELECT payout_address FROM worker_payout_lock WHERE worker_id=?`, workerID).Scan(&addr)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return addr, true
}

func payoutLockSeen(t *testing.T, db *sql.DB, workerID string) (string, int64, bool) {
	t.Helper()
	var addr string
	var seen int64
	err := db.QueryRow(`SELECT payout_address, seen_at FROM worker_payout_lock WHERE worker_id=?`, workerID).Scan(&addr, &seen)
	if err == sql.ErrNoRows {
		return "", 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return addr, seen, true
}

func TestUnbindPayoutLockSurfacesDurableDeleteFailure(t *testing.T) {
	db, wm, mux := payoutLockSetup(t)
	wm.persistPayoutLock("wk-fail", "HMC-aaaa1111aaaa1111")
	if addr := wm.lockedPayoutAddress("wk-fail"); addr != "HMC-aaaa1111aaaa1111" {
		t.Fatalf("pre: binding = %q", addr)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	code, out := payoutLockPost(t, mux, map[string]any{"worker_id": "wk-fail"})
	if code != http.StatusInternalServerError || out["ok"] != false {
		t.Fatalf("durable DELETE failure must surface, got code=%d out=%v", code, out)
	}
}

func TestUnbindPayoutLockHappyPathStillClears(t *testing.T) {
	db, wm, mux := payoutLockSetup(t)
	wm.persistPayoutLock("wk-ok", "HMC-bbbb2222bbbb2222")
	code, out := payoutLockPost(t, mux, map[string]any{"worker_id": "wk-ok"})
	if code != http.StatusOK || out["ok"] != true || out["cleared"] != true {
		t.Fatalf("happy path: code=%d out=%v", code, out)
	}
	if out["previous_address"] != "HMC-bbbb2222bbbb2222" {
		t.Fatalf("previous_address = %v", out["previous_address"])
	}
	if addr := wm.lockedPayoutAddress("wk-ok"); addr != "" {
		t.Fatalf("memory binding survived: %q", addr)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_payout_lock WHERE worker_id=?`, "wk-ok").Scan(&n); err != nil || n != 0 {
		t.Fatalf("durable row survived: n=%d err=%v", n, err)
	}
}

func TestPayoutLockMigrationBackfillsSeenAt(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "pl-mig.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP TABLE IF EXISTS worker_payout_lock`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE worker_payout_lock (
		worker_id TEXT PRIMARY KEY,
		payout_address TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO worker_payout_lock(worker_id, payout_address) VALUES(?,?)`, "wk-old", "HMC-cccc3333cccc3333"); err != nil {
		t.Fatal(err)
	}
	wm := &workManager{worker: map[string]workerPayoutStat{}}
	wm.attachDedupDB(db) // runs the upgrade + backfill
	addr, seen, ok := payoutLockSeen(t, db, "wk-old")
	if !ok || addr != "HMC-cccc3333cccc3333" || seen <= 0 {
		t.Fatalf("backfill: ok=%v addr=%q seen=%d", ok, addr, seen)
	}
}

func TestPersistPayoutLockKeepsFirstAddressAndRefreshesSeenAt(t *testing.T) {
	db, wm, _ := payoutLockSetup(t)
	wm.persistPayoutLock("wk-first", "HMC-dddd4444dddd4444")
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=1 WHERE worker_id=?`, "wk-first"); err != nil {
		t.Fatal(err)
	}
	wm.persistPayoutLock("wk-first", "HMC-eeee5555eeee5555") // must NOT rebind
	addr, seen, _ := payoutLockSeen(t, db, "wk-first")
	if addr != "HMC-dddd4444dddd4444" {
		t.Fatalf("first-bound address was rewritten: %q", addr)
	}
	if seen <= 1 {
		t.Fatalf("seen_at not refreshed: %d", seen)
	}
}

func TestPayoutLockGCDropsOnlyIdleRows(t *testing.T) {
	t.Setenv("HACKME_PAYOUT_LOCK_IDLE_SEC", "2592000") // 30d, the default
	db, wm, _ := payoutLockSetup(t)
	wm.persistPayoutLock("wk-live", "HMC-ffff6666ffff6666")
	wm.persistPayoutLock("wk-idle", "HMC-1111777711117777")
	old := time.Now().Unix() - 40*24*3600
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=? WHERE worker_id=?`, old, "wk-idle"); err != nil {
		t.Fatal(err)
	}
	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db) // GC runs on attach
	if _, _, ok := payoutLockSeen(t, db, "wk-idle"); ok {
		t.Fatal("idle row survived GC")
	}
	if _, _, ok := payoutLockSeen(t, db, "wk-live"); !ok {
		t.Fatal("live row was GC-ed")
	}
	t.Setenv("HACKME_PAYOUT_LOCK_IDLE_SEC", "0") // disabled
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=? WHERE worker_id=?`, old, "wk-live"); err != nil {
		t.Fatal(err)
	}
	wm3 := &workManager{worker: map[string]workerPayoutStat{}}
	wm3.attachDedupDB(db)
	if _, _, ok := payoutLockSeen(t, db, "wk-live"); !ok {
		t.Fatal("GC ran despite the disabled flag")
	}
}

func TestUnbindPayoutLockBulk(t *testing.T) {
	db, wm, mux := payoutLockSetup(t)
	for _, id := range []string{"wk-b1", "wk-b2", "wk-b3"} {
		wm.persistPayoutLock(id, "HMC-8888999988889999")
	}
	code, out := payoutLockPost(t, mux, map[string]any{"worker_ids": []string{"wk-b1", "wk-b2", "wk-b3"}})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("bulk unbind: code=%d out=%v", code, out)
	}
	if fmt.Sprint(out["cleared"]) != "3" {
		t.Fatalf("cleared = %v", out["cleared"])
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_payout_lock WHERE worker_id LIKE 'wk-b%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows survived bulk unbind: n=%d err=%v", n, err)
	}
	many := make([]string, 257)
	for i := range many {
		many[i] = fmt.Sprintf("wk-c%03d", i)
	}
	if code, _ := payoutLockPost(t, mux, map[string]any{"worker_ids": many}); code != http.StatusBadRequest {
		t.Fatalf("bulk cap not enforced: %d", code)
	}
	if code, _ := payoutLockPost(t, mux, map[string]any{"worker_ids": []string{"wk-b1", "bad id!"}}); code != http.StatusBadRequest {
		t.Fatalf("invalid id in bulk accepted: %d", code)
	}
}

func TestLoadPayoutLocksBoundedNewestFirst(t *testing.T) {
	t.Setenv("HACKME_WORK_DEDUP_MAX_IN_MEMORY", "10000")
	db, _, _ := payoutLockSetup(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Unix() - 20000 // fresh enough to survive the 30d idle GC
	for i := 0; i <= 10000; i++ {
		if _, err := tx.Exec(`INSERT INTO worker_payout_lock(worker_id, payout_address, seen_at) VALUES(?,?,?)`,
			fmt.Sprintf("wk-l%05d", i), fmt.Sprintf("HMC-l%05d", i), base+int64(i)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db)
	if got := len(wm2.worker); got != 10000 {
		t.Fatalf("loaded %d rows, want 10000 (bounded, newest-first)", got)
	}
	if _, ok := wm2.worker["wk-l10000"]; !ok {
		t.Fatal("newest binding missing from memory")
	}
	if _, ok := wm2.worker["wk-l00000"]; ok {
		t.Fatal("oldest binding should be evicted from memory")
	}
	if addr, ok := payoutLockAddr(t, db, "wk-l00000"); !ok || addr != "HMC-l00000" {
		t.Fatalf("evicted durable row lost: ok=%v addr=%q", ok, addr)
	}
	if addr := wm2.lockedPayoutAddress("wk-l00000"); addr != "HMC-l00000" {
		t.Fatalf("evicted binding no longer enforced via fallback: %q", addr)
	}
}

func TestPayoutLockTouchKeepsActiveBindingFreshForGC(t *testing.T) {
	t.Setenv("HACKME_PAYOUT_LOCK_IDLE_SEC", "2592000") // 30d, the default
	db, wm, _ := payoutLockSetup(t)
	wm.persistPayoutLock("wk-act", "HMC-1234123412341234")
	// simulate a worker that bound its address 40d ago and stayed memory-warm
	old := time.Now().Unix() - 40*24*3600
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=? WHERE worker_id=?`, old, "wk-act"); err != nil {
		t.Fatal(err)
	}
	if addr := wm.lockedPayoutAddress("wk-act"); addr != "HMC-1234123412341234" {
		t.Fatalf("first read: %q", addr)
	}
	// second read: memory-warm path, throttled touch must refresh the durable row
	if addr := wm.lockedPayoutAddress("wk-act"); addr != "HMC-1234123412341234" {
		t.Fatalf("second read: %q", addr)
	}
	_, seen, ok := payoutLockSeen(t, db, "wk-act")
	if !ok || seen <= old {
		t.Fatalf("active lock read did not refresh seen_at: ok=%v seen=%d old=%d", ok, seen, old)
	}
	// coordinator restart: idle-GC must not reap the refreshed binding
	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db)
	if _, _, ok := payoutLockSeen(t, db, "wk-act"); !ok {
		t.Fatal("active binding reaped by GC after restart")
	}
}

func TestPayoutLockTouchIsThrottled(t *testing.T) {
	db, wm, _ := payoutLockSetup(t)
	wm.persistPayoutLock("wk-th", "HMC-4321432143214321")
	_ = wm.lockedPayoutAddress("wk-th") // fallback caches the binding
	_ = wm.lockedPayoutAddress("wk-th") // memory hit: touch fires
	_, s2, _ := payoutLockSeen(t, db, "wk-th")
	// immediate repeat read: within the window, no new DB write
	_ = wm.lockedPayoutAddress("wk-th")
	if _, s3, _ := payoutLockSeen(t, db, "wk-th"); s3 != s2 {
		t.Fatalf("touch was not throttled: %d -> %d", s2, s3)
	}
	// expire the throttle and backdate the durable row, then expect a refresh
	wm.mu.Lock()
	st := wm.worker["wk-th"]
	st.LockSeenUnix = time.Now().Unix() - 7200
	wm.worker["wk-th"] = st
	wm.mu.Unlock()
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=100 WHERE worker_id=?`, "wk-th"); err != nil {
		t.Fatal(err)
	}
	_ = wm.lockedPayoutAddress("wk-th")
	if _, s4, _ := payoutLockSeen(t, db, "wk-th"); s4 <= 100 {
		t.Fatalf("expired throttle did not refresh: %d", s4)
	}
}

func TestPayoutLockLookupFailsClosedOnDBError(t *testing.T) {
	db, wm, _ := payoutLockSetup(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// memory miss + broken durable store must not read as "no binding"
	if addr := wm.lockedPayoutAddress("wk-nn"); addr != payoutLockLookupFailed {
		t.Fatalf("lookup on broken store = %q, want the fail-closed sentinel", addr)
	}
}

func TestPayoutLockEvictedReadRefreshesSeenAt(t *testing.T) {
	db, wm, _ := payoutLockSetup(t)
	wm.persistPayoutLock("wk-ev", "HMC-5678567856785678")
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=100 WHERE worker_id=?`, "wk-ev"); err != nil {
		t.Fatal(err)
	}
	// drop the binding from the warm cache as if evicted by the load cap
	wm.mu.Lock()
	delete(wm.worker, "wk-ev")
	wm.mu.Unlock()
	if addr := wm.lockedPayoutAddress("wk-ev"); addr != "HMC-5678567856785678" {
		t.Fatalf("fallback read: %q", addr)
	}
	if _, seen, ok := payoutLockSeen(t, db, "wk-ev"); !ok || seen <= 100 {
		t.Fatalf("first fallback read did not refresh liveness: ok=%v seen=%d", ok, seen)
	}
}

func TestPayoutLockTouchFailureRetriesImmediately(t *testing.T) {
	db, wm, _ := payoutLockSetup(t)
	wm.persistPayoutLock("wk-rt", "HMC-8765876587658765")
	_ = wm.lockedPayoutAddress("wk-rt") // cache
	_ = wm.lockedPayoutAddress("wk-rt") // memory-hit touch
	backdated := time.Now().Unix() - 7200
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=100 WHERE worker_id=?`, "wk-rt"); err != nil {
		t.Fatal(err)
	}
	wm.mu.Lock()
	st := wm.worker["wk-rt"]
	st.LockSeenUnix = backdated
	wm.worker["wk-rt"] = st
	wm.mu.Unlock()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_ = wm.lockedPayoutAddress("wk-rt") // touch attempt must fail
	wm.mu.Lock()
	got := wm.worker["wk-rt"].LockSeenUnix
	wm.mu.Unlock()
	if got != backdated {
		t.Fatalf("throttle advanced despite failed touch: %d want %d", got, backdated)
	}
}

func TestLoadPayoutLocksRespectsMaxWorkers(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "pl-cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	warm := &workManager{worker: map[string]workerPayoutStat{}}
	warm.attachDedupDB(db) // create the schema before the inserts below
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Unix() - 5000
	for i := 0; i < 5; i++ {
		if _, err := tx.Exec(`INSERT INTO worker_payout_lock(worker_id, payout_address, seen_at) VALUES(?,?,?)`,
			fmt.Sprintf("wk-m%d", i), fmt.Sprintf("HMC-m%d", i), base+int64(i)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wm := &workManager{worker: map[string]workerPayoutStat{}, maxWorkers: 3}
	wm.attachDedupDB(db)
	if got := len(wm.worker); got != 3 {
		t.Fatalf("warm-up loaded %d bindings, want the maxWorkers cap of 3", got)
	}
	if _, ok := wm.worker["wk-m4"]; !ok {
		t.Fatal("newest binding missing despite newest-first order")
	}
	if _, ok := wm.worker["wk-m0"]; ok {
		t.Fatal("binding beyond the cap was loaded")
	}
	if addr := wm.lockedPayoutAddress("wk-m0"); addr != "HMC-m0" {
		t.Fatalf("capped binding lost enforcement via fallback: %q", addr)
	}
}

func TestUnbindWithUnpaidRequiresRebindOrForfeit(t *testing.T) {
	_, wm, mux := payoutLockSetup(t)
	wm.persistPayoutLock("wk-unpaid", "HMC-aaaaaaaaaaaaaaaa")
	wm.mu.Lock()
	st := wm.worker["wk-unpaid"]
	st.PayoutAddress = "HMC-aaaaaaaaaaaaaaaa"
	st.PayoutHMC = 1.25
	st.PayoutUnits = 125000000
	wm.worker["wk-unpaid"] = st
	wm.mu.Unlock()

	code, out := payoutLockPost(t, mux, map[string]any{"worker_id": "wk-unpaid"})
	if code != http.StatusConflict {
		t.Fatalf("expected 409 without rebind/forfeit, got %d %v", code, out)
	}

	code, out = payoutLockPost(t, mux, map[string]any{
		"worker_id": "wk-unpaid", "rebind_to": "HMC-bbbbbbbbbbbbbbbb",
	})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("rebind_to: %d %v", code, out)
	}
	wm.mu.Lock()
	got := wm.worker["wk-unpaid"]
	wm.mu.Unlock()
	if !strings.EqualFold(strings.TrimSpace(got.PayoutAddress), "HMC-bbbbbbbbbbbbbbbb") {
		t.Fatalf("rebind address = %q", got.PayoutAddress)
	}
	if got.PayoutUnits < 125000000 {
		t.Fatalf("rebind must keep unpaid units, got %d", got.PayoutUnits)
	}
	if addr := wm.lockedPayoutAddress("wk-unpaid"); !strings.EqualFold(addr, "HMC-bbbbbbbbbbbbbbbb") {
		t.Fatalf("lock after rebind = %q", addr)
	}
}

func TestUnbindForfeitUnpaidClearsAccruals(t *testing.T) {
	_, wm, mux := payoutLockSetup(t)
	wm.persistPayoutLock("wk-forfeit", "HMC-cccccccccccccccc")
	wm.mu.Lock()
	wm.worker["wk-forfeit"] = workerPayoutStat{
		PayoutAddress: "HMC-cccccccccccccccc",
		PayoutHMC:     2, PayoutUnits: 200000000, PayoutSUP: 0.5,
	}
	wm.mu.Unlock()
	code, out := payoutLockPost(t, mux, map[string]any{
		"worker_id": "wk-forfeit", "forfeit_unpaid": true,
	})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("forfeit: %d %v", code, out)
	}
	wm.mu.Lock()
	got := wm.worker["wk-forfeit"]
	wm.mu.Unlock()
	if workerStatHasUnpaidAccrual(got) || strings.TrimSpace(got.PayoutAddress) != "" {
		t.Fatalf("forfeit must zero unpaid+address: %+v", got)
	}
}

func TestNotePayoutLockDoesNotAdoptOntoUnpaidBlank(t *testing.T) {
	_, wm, _ := payoutLockSetup(t)
	wm.mu.Lock()
	wm.worker["wk-snipe"] = workerPayoutStat{
		PayoutAddress: "", PayoutHMC: 3, PayoutUnits: 300000000,
	}
	wm.mu.Unlock()
	wm.notePayoutLock("wk-snipe", "HMC-dddddddddddddddd")
	wm.mu.Lock()
	got := wm.worker["wk-snipe"]
	wm.mu.Unlock()
	if strings.TrimSpace(got.PayoutAddress) != "" {
		t.Fatalf("unpaid blank must not adopt claim address, got %q", got.PayoutAddress)
	}
	if addr := wm.lockedPayoutAddress("wk-snipe"); !strings.EqualFold(addr, "HMC-dddddddddddddddd") {
		t.Fatalf("identity lock should still bind, got %q", addr)
	}
}
