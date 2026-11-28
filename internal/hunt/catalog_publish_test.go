package hunt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
)

func TestCollectLibFuzzerPublishUnitsSkipsCrashBootSymlink(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeAt := func(name string, body []byte, age time.Duration) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(age)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("grown.bin", []byte("grown-mid-wall"), time.Second)
	writeAt("boot.json", []byte("{}"), 2*time.Second)
	writeAt("arr.json", []byte("[]"), 3*time.Second)
	writeAt("null.json", []byte("null"), 4*time.Second)
	writeAt("z.bin", []byte{0x00}, 5*time.Second)
	writeAt("hi.bin", []byte{0x80}, 6*time.Second)
	writeAt("crash-deadbeef", []byte("crash-artifact"), 7*time.Second)
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("symlink-secret-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "via-link.bin")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.bin"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir.bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	units := CollectLibFuzzerPublishUnits(dir, nil, 0, 0)
	if len(units) != 1 || string(units[0]) != "grown-mid-wall" {
		t.Fatalf("publish set = %q", units)
	}
}

func TestPublishCursorDoesNotDumpTheCorpusItStartedWith(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	oldPath := filepath.Join(dir, "old.bin")
	if err := os.WriteFile(oldPath, []byte("already-on-disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldPath, now, now); err != nil {
		t.Fatal(err)
	}
	cur := newPublishCursor()
	cur.absorbNames(dir)
	files, hashes := cur.snapshot()
	units, _, _ := scanLibFuzzerPublish(dir, files, hashes, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
	if len(units) != 0 {
		t.Fatalf("first tick dumped the corpus: %q", units)
	}
	grown := filepath.Join(dir, "grown.bin")
	if err := os.WriteFile(grown, []byte("grown-mid-wall"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Second)
	if err := os.Chtimes(grown, later, later); err != nil {
		t.Fatal(err)
	}
	files, hashes = cur.snapshot()
	units, names, stamps := scanLibFuzzerPublish(dir, files, hashes, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
	if len(units) != 1 || string(units[0]) != "grown-mid-wall" {
		t.Fatalf("new unit missing: %q", units)
	}
	cur.rememberFiles(names, stamps)
	cur.rememberBytes(units)
	files, hashes = cur.snapshot()
	again, _, _ := scanLibFuzzerPublish(dir, files, hashes, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
	if len(again) != 0 {
		t.Fatalf("second tick repeated the unit: %q", again)
	}
}

func TestCollectLibFuzzerPublishUnitsCapsOneTick(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 40; i++ {
		path := filepath.Join(dir, fmt.Sprintf("unit-%02d.bin", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("unit-%02d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	units := CollectLibFuzzerPublishUnits(dir, nil, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
	if len(units) != LibFuzzerPublishTickItems {
		t.Fatalf("tick took %d units", len(units))
	}
	if string(units[0]) != "unit-39" {
		t.Fatalf("newest missing: %q", units[0])
	}
	seen := map[string]struct{}{}
	for _, u := range units {
		sum := sha256.Sum256(u)
		seen[hex.EncodeToString(sum[:])] = struct{}{}
	}
	rest := CollectLibFuzzerPublishUnits(dir, seen, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
	if len(rest) != 8 {
		t.Fatalf("rest = %d %q", len(rest), rest)
	}
	got := map[string]struct{}{}
	for _, u := range append(units, rest...) {
		got[string(u)] = struct{}{}
	}
	for i := 0; i < 40; i++ {
		if _, ok := got[fmt.Sprintf("unit-%02d", i)]; !ok {
			t.Fatalf("unit-%02d missing across ticks", i)
		}
	}
}

func TestPublishTickPrefersRareFrontierOverNewerMutants(t *testing.T) {
	buckets := map[int][][]byte{}
	for i := 0; i < 256; i++ {
		for j := 0; j < 256; j++ {
			b := []byte{byte(i), byte(j)}
			edge, _ := fuzzengine.CoverageBucketsStructural(b)
			buckets[edge] = append(buckets[edge], append([]byte(nil), b...))
		}
	}
	var mutants [][]byte
	shared := 0
	for edge, group := range buckets {
		if len(group) > len(mutants) {
			shared = edge
			mutants = group
		}
	}
	if len(mutants) < 40 {
		t.Fatalf("need a shared structural bucket, got %d", len(mutants))
	}
	mutants = mutants[:40]
	var rare []byte
	for n := 0; n < 200; n++ {
		cand := []byte(fmt.Sprintf(`{"frontier":%d,"stage":"opened-next","pad":"coverage-unit-xx"}`, n))
		edge, _ := fuzzengine.CoverageBucketsStructural(cand)
		if edge != shared && !genericBootSeed(cand) {
			rare = cand
			break
		}
	}
	if len(rare) <= len(mutants[0]) {
		t.Fatal("rare unit is not longer than the mutants")
	}
	dir := t.TempDir()
	now := time.Now()
	writeAt := func(name string, body []byte, mt time.Time) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("rare.bin", rare, now.Add(-time.Hour))
	for i, b := range mutants {
		writeAt(fmt.Sprintf("mut-%02d.bin", i), b, now.Add(time.Duration(i+1)*time.Second))
	}
	units := CollectLibFuzzerPublishUnits(dir, nil, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
	if len(units) != LibFuzzerPublishTickItems {
		t.Fatalf("tick took %d units", len(units))
	}
	pos := -1
	keptMutants := 0
	for i, u := range units {
		if bytes.Equal(u, rare) {
			pos = i
			continue
		}
		keptMutants++
	}
	if pos < 0 {
		t.Fatal("rare longer unit was dropped for newer trivial mutants")
	}
	if pos >= len(units)-1 {
		t.Fatal("rare unit ranked behind the whole mutant pile")
	}
	if keptMutants >= len(mutants) {
		t.Fatal("tick kept every newer mutant")
	}
	var after int
	for _, u := range units[pos+1:] {
		if !bytes.Equal(u, rare) {
			after++
		}
	}
	if after == 0 {
		t.Fatal("no newer mutant was published after the rare unit")
	}
}

func TestPublishLoopRetriesUntilAccepted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grown.bin"), []byte("grown-mid-wall"), 0o600); err != nil {
		t.Fatal(err)
	}
	cur := newPublishCursor()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	go publishLibFuzzerSeeds(ctx, dir, 10*time.Millisecond, cur, func(context.Context, [][]byte) error {
		if calls.Add(1) == 1 {
			return errors.New("try again")
		}
		return nil
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		files, hashes := cur.snapshot()
		left, _, _ := scanLibFuzzerPublish(dir, files, hashes, LibFuzzerPublishTickItems, LibFuzzerPublishTickBytes)
		if calls.Load() >= 2 && len(left) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("publish did not stick after retry, calls=%d", calls.Load())
}

func TestLibFuzzerShareStopsBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	share := startLibFuzzerShare(context.Background(), dir, 5*time.Millisecond, func(ctx context.Context) ([][]byte, error) {
		once.Do(func() { close(entered) })
		<-release
		return [][]byte{[]byte("too-late")}, nil
	}, nil, newPublishCursor())
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("pull did not start")
	}
	finished := make(chan struct{})
	go func() {
		share.stop()
		close(finished)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		share.mu.Lock()
		closed := share.stopped
		share.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop did not close the share")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-finished:
		t.Fatal("stop returned while the pull was still inside the request")
	default:
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not wait for the pull goroutine")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("InstallEngineSeedFiles ran after stop: %d files", len(ents))
	}
}
