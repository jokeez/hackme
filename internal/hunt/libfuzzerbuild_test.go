package hunt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPoolVsLocalInProcessBreakEven(t *testing.T) {
	// Same corpus argv shape. Pool heavy wall vs one local overnight process.
	// Numbers come from the flags, not from a fabricated exec count.
	wall := PoolEngineLeaseSecHeavy
	forks := libFuzzerSessionForkJobs(wall, false)
	localSec := LocalOvernightTimeLimitSec("hunt_heavy")
	pool := strings.Join(libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", wall, "", forks), " ")
	local := strings.Join(libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", localSec, "", 1), " ")
	if !strings.Contains(pool, "-max_total_time=300") || !strings.Contains(pool, "-fork=3") || !strings.Contains(pool, "-len_control=0") || !strings.Contains(pool, "-ignore_crashes=1") || !strings.Contains(pool, "-reduce_inputs=0") {
		t.Fatalf("pool heavy argv=%s", pool)
	}
	if strings.Contains(local, "-fork=") || strings.Contains(local, "-len_control=0") || !strings.Contains(local, "-max_total_time=43200") || !strings.Contains(local, "-timeout=25") {
		t.Fatalf("local overnight argv=%s", local)
	}
	// Job-seconds: 43200*1 / (300*3) = 48. One heavy shard is 1/48 of that budget.
	if got := InProcessShardsToMatchLocal(wall, forks, localSec, 1); got != 48 {
		t.Fatalf("heavy break-even=%d want 48", got)
	}
	if got := InProcessShardsToMatchLocal(PoolEngineLeaseSecStandard, libFuzzerSessionForkJobs(PoolEngineLeaseSecStandard, false), LocalOvernightTimeLimitSec("hunt_standard"), 1); got != 60 {
		t.Fatalf("standard break-even=%d want 60", got)
	}
	if got := InProcessShardsToMatchLocal(PoolEngineLeaseSecLite, libFuzzerSessionForkJobs(PoolEngineLeaseSecLite, false), LocalOvernightTimeLimitSec("hunt_lite"), 1); got != 10 {
		t.Fatalf("lite break-even=%d want 10", got)
	}
	// Stdin heavy stays at 2 jobs. Do not plug that into the in-process formula
	// and call it a win: the exec rate is a different class.
	if got := libFuzzerSessionForkJobs(wall, true); got != 2 {
		t.Fatalf("stdin heavy fork=%d", got)
	}
}

func TestLibFuzzerDriverPlan(t *testing.T) {
	t.Setenv("HACKME_REPO_ROOT", "")
	root := RepoRoot()
	if in, reason := libFuzzerDriverPlan(root, "jsmn", false, false); in || reason != "stdin" {
		t.Fatalf("jsmn in=%v reason=%s", in, reason)
	}
	if DedicatedLibFuzzerHarness(root, "jsmn") != "" {
		t.Fatal("jsmn must stay stdin-only; no dedicated LLVMFuzzerTestOneInput harness")
	}
	if in, reason := libFuzzerDriverPlan(root, "cjson", false, false); !in || reason != "catalog" {
		t.Fatalf("cjson in=%v reason=%s", in, reason)
	}
	if in, reason := libFuzzerDriverPlan(root, "jsmn", false, true); !in || reason != "published" {
		t.Fatalf("published ELF in=%v reason=%s", in, reason)
	}
	if in, reason := libFuzzerDriverPlan(root, "jsmn", true, false); !in || reason != "inventory" {
		t.Fatalf("inventory in=%v reason=%s", in, reason)
	}
	if catalogInProcessBinUsable("", "", nil) || catalogInProcessBinUsable("/no/such", "/stdin", nil) {
		t.Fatal("missing or stdin-paired bin must stay on the exec driver")
	}
	stub := filepath.Join(t.TempDir(), "stub")
	if err := os.WriteFile(stub, []byte("stdin_subprocess_libfuzzer.c"), 0o755); err != nil {
		t.Fatal(err)
	}
	if catalogInProcessBinUsable(stub, "", nil) {
		t.Fatal("subprocess stub must not count as in-process")
	}
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(stub, link); err != nil {
		t.Fatal(err)
	}
	if catalogInProcessBinUsable(link, "", nil) {
		t.Fatal("symlink must not be selected as the in-process target")
	}
}

func TestBuildLibFuzzerImportReusesDedicatedCacheWithoutClang(t *testing.T) {
	root := t.TempDir()
	id := "cjson"
	harnessDir := filepath.Join(root, "tasks", "sources", "fuzz", "benchmark")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, id+"_libfuzzer.c"), []byte("int LLVMFuzzerTestOneInput(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := LibFuzzerImportBinPath(root, id)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("dedicated-catalog-bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin+".kind", []byte("dedicated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	got, stdin, err := BuildLibFuzzerImport(context.Background(), root, id)
	if err != nil {
		t.Fatal(err)
	}
	if got != bin || stdin != "" {
		t.Fatalf("bin=%s stdin=%s", got, stdin)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, bin); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildLibFuzzerImport(context.Background(), root, id); err == nil {
		t.Fatal("symlink cache bin must not skip clang")
	}
	body, err := os.ReadFile(outside)
	if err != nil || string(body) != "secret" {
		t.Fatalf("symlink followed: %q err=%v", body, err)
	}
}

func TestLibFuzzerASANOptionsCrashPath(t *testing.T) {
	if !strings.Contains(LibFuzzerASANOptions, "abort_on_error=1") || !strings.Contains(LibFuzzerASANOptions, "symbolize=0") {
		t.Fatalf("asan=%s", LibFuzzerASANOptions)
	}
	if strings.Contains(LibFuzzerASANOptions, "detect_leaks=1") {
		t.Fatalf("leak detection hangs short pool walls: %s", LibFuzzerASANOptions)
	}
}

func TestLibFuzzerSessionArgsDeeperOnLongWall(t *testing.T) {
	short := libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", 90, "", 1)
	long := libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", 180, "/tmp/c/libfuzzer.dict", 2)
	joinedShort := strings.Join(short, " ")
	joinedLong := strings.Join(long, " ")
	if !strings.Contains(joinedShort, "-mutate_depth=8") || !strings.Contains(joinedShort, "-entropic=1") || !strings.Contains(joinedShort, "-use_value_profile=1") || !strings.Contains(joinedShort, "-len_control=0") || !strings.Contains(joinedShort, "-detect_leaks=0") || !strings.Contains(joinedShort, "-timeout=5") || !strings.Contains(joinedShort, "-ignore_crashes=1") || !strings.Contains(joinedShort, "-reduce_inputs=0") {
		t.Fatalf("short args=%v", short)
	}
	if strings.Contains(joinedShort, "-fork=") {
		t.Fatalf("short wall must stay single-process: %v", short)
	}
	if !strings.Contains(joinedLong, "-mutate_depth=16") || !strings.Contains(joinedLong, "-dict=/tmp/c/libfuzzer.dict") || !strings.Contains(joinedLong, "-fork=2") || !strings.Contains(joinedLong, "-rss_limit_mb=1024") || !strings.Contains(joinedLong, "-timeout=10") || !strings.Contains(joinedLong, "-reduce_inputs=0") {
		t.Fatalf("long args=%v", long)
	}
	heavy := strings.Join(libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", 300, "", 1), " ")
	if !strings.Contains(heavy, "-mutate_depth=20") || !strings.Contains(heavy, "-timeout=10") || !strings.Contains(heavy, "-reduce_inputs=0") {
		t.Fatalf("heavy=%s", heavy)
	}
	heavyFork := strings.Join(libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", 300, "", 3), " ")
	if !strings.Contains(heavyFork, "-fork=3") || !strings.Contains(heavyFork, "-reduce_inputs=0") || !strings.Contains(heavyFork, "-rss_limit_mb=768") {
		t.Fatalf("in-process heavy fork=%s", heavyFork)
	}
	leaseEdge := strings.Join(libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", 600, "", 1), " ")
	if !strings.Contains(leaseEdge, "-reduce_inputs=0") || !strings.Contains(leaseEdge, "-len_control=0") {
		t.Fatalf("600s lease=%s", leaseEdge)
	}
	overnight := strings.Join(libFuzzerSessionArgs("/tmp/c", "/tmp/c/crash-", 3600, "", 1), " ")
	if strings.Contains(overnight, "-len_control=0") || strings.Contains(overnight, "-ignore_crashes=1") || strings.Contains(overnight, "-reduce_inputs=0") || !strings.Contains(overnight, "-timeout=25") {
		t.Fatalf("overnight should keep length ramp, input reduction, and stop on crash: %s", overnight)
	}
	if libFuzzerTuneForWall(120) != 12 {
		t.Fatalf("mid depth=%d", libFuzzerTuneForWall(120))
	}
}

func TestLibFuzzerForkJobs(t *testing.T) {
	t.Setenv("HACKME_HUNT_LF_FORK", "")
	if libFuzzerForkJobs(90) != 1 || libFuzzerForkJobs(180) != 2 || libFuzzerForkJobs(300) != 3 {
		t.Fatalf("auto fork 90=%d 180=%d 300=%d", libFuzzerForkJobs(90), libFuzzerForkJobs(180), libFuzzerForkJobs(300))
	}
	t.Setenv("HACKME_HUNT_LF_FORK", "4")
	if libFuzzerForkJobs(30) != 4 {
		t.Fatalf("override=%d", libFuzzerForkJobs(30))
	}
	t.Setenv("HACKME_HUNT_LF_FORK", "99")
	if libFuzzerForkJobs(30) != 4 {
		t.Fatalf("cap=%d", libFuzzerForkJobs(30))
	}
	t.Setenv("HACKME_HUNT_LF_FORK", "0")
	if libFuzzerForkJobs(180) != 1 {
		t.Fatalf("zero=%d", libFuzzerForkJobs(180))
	}
}

func TestLibFuzzerSessionForkJobsStdinCap(t *testing.T) {
	t.Setenv("HACKME_HUNT_LF_FORK", "")
	if libFuzzerSessionForkJobs(300, false) != 3 || libFuzzerSessionForkJobs(180, false) != 2 || libFuzzerSessionForkJobs(90, false) != 1 {
		t.Fatalf("in-process 300=%d 180=%d 90=%d", libFuzzerSessionForkJobs(300, false), libFuzzerSessionForkJobs(180, false), libFuzzerSessionForkJobs(90, false))
	}
	if libFuzzerSessionForkJobs(300, true) != 2 || libFuzzerSessionForkJobs(180, true) != 2 || libFuzzerSessionForkJobs(90, true) != 1 {
		t.Fatalf("stdin 300=%d 180=%d 90=%d", libFuzzerSessionForkJobs(300, true), libFuzzerSessionForkJobs(180, true), libFuzzerSessionForkJobs(90, true))
	}
	t.Setenv("HACKME_HUNT_LF_FORK", "4")
	if libFuzzerSessionForkJobs(300, false) != 4 {
		t.Fatalf("in-process override=%d", libFuzzerSessionForkJobs(300, false))
	}
	if libFuzzerSessionForkJobs(300, true) != 2 {
		t.Fatalf("stdin override must stay capped at 2, got %d", libFuzzerSessionForkJobs(300, true))
	}
	t.Setenv("HACKME_HUNT_LF_FORK", "1")
	if libFuzzerSessionForkJobs(300, true) != 1 {
		t.Fatalf("stdin explicit 1=%d", libFuzzerSessionForkJobs(300, true))
	}
}

func TestLibFuzzerExecutedUnits(t *testing.T) {
	stat := "INFO: seed\nstat::number_of_executed_units: 17\nstat::new_units_added: 2\n"
	if got := libFuzzerExecutedUnits(stat); got != 17 {
		t.Fatalf("stat=%d", got)
	}
	if got := libFuzzerExecutedUnits("stat::number_of_executed_units: 0\n"); got != 0 {
		t.Fatalf("explicit zero=%d", got)
	}
	if got := libFuzzerExecutedUnits("no stats\n#12\tpulse cov: 1 ft: 1\n"); got != 0 {
		t.Fatalf("pulse line is not the fork sum, got %d", got)
	}
	// Two job logs concatenated: each prints its own final stat.
	summed := "stat::number_of_executed_units: 10\nstat::number_of_executed_units: 7\n"
	if got := libFuzzerExecutedUnits(summed); got != 17 {
		t.Fatalf("sum=%d", got)
	}
	// Fork parent reprints a cumulative sum. Later lines are larger; do not add them.
	fork := "#10: cov: 1 ft: 2 corp: 1 exec/s: 5 oom/timeout/crash: 0/0/0 time: 1s job: 0 dft_time: 0\n" +
		"#40: cov: 3 ft: 4 corp: 2 exec/s: 8 oom/timeout/crash: 0/0/0 time: 2s job: 1 dft_time: 0\n"
	if got := libFuzzerExecutedUnits(fork); got != 40 {
		t.Fatalf("fork aggregate=%d", got)
	}
	both := "stat::number_of_executed_units: 9\n#100: cov: 1 ft: 1 corp: 1 exec/s: 1 oom/timeout/crash: 0/0/0 time: 1s job: 0 dft_time: 0\n"
	if got := libFuzzerExecutedUnits(both); got != 100 {
		t.Fatalf("fork sum must win over a partial stat, got %d", got)
	}
	if libFuzzerExecutedUnits("") != 0 {
		t.Fatal("empty log must be 0")
	}
}

func TestLibFuzzerSessionExecsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	stats := filepath.Join(dir, ".hunt-run", "session_stats.json")
	if err := writeLibFuzzerSessionStats(dir, stats, "t", 180, 1, 1, "stat::number_of_executed_units: 0\n"); err != nil {
		t.Fatal(err)
	}
	if got := readLibFuzzerSessionExecs(dir); got != 0 {
		t.Fatalf("zero execs must round-trip, got %d", got)
	}
	if err := writeLibFuzzerSessionStats(dir, stats, "t", 300, 2, 4, "#2500: cov: 9 ft: 12 corp: 4 exec/s: 400 oom/timeout/crash: 0/0/0 time: 8s job: 2 dft_time: 0\n"); err != nil {
		t.Fatal(err)
	}
	if got := readLibFuzzerSessionExecs(dir); got != 2500 {
		t.Fatalf("fork execs=%d", got)
	}
	if got := readLibFuzzerSessionExecs(t.TempDir()); got != 0 {
		t.Fatalf("missing stats=%d", got)
	}
}

func TestPublishedHarnessIsLibFuzzer(t *testing.T) {
	dir := t.TempDir()
	stdin := filepath.Join(dir, "stdin.bin")
	if err := os.WriteFile(stdin, []byte("ASAN stdin target no driver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if PublishedHarnessIsLibFuzzer(stdin) {
		t.Fatal("stdin harness must stay on the exec driver")
	}
	stub := filepath.Join(dir, "stub.bin")
	if err := os.WriteFile(stub, []byte("LLVMFuzzerRunDriver HACKME_LF_STDIN_BIN"), 0o755); err != nil {
		t.Fatal(err)
	}
	if PublishedHarnessIsLibFuzzer(stub) {
		t.Fatal("subprocess driver must not be treated as the target")
	}
	real := filepath.Join(dir, "lf.bin")
	if err := os.WriteFile(real, []byte("ELF LLVMFuzzerRunDriver fuzzer main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !PublishedHarnessIsLibFuzzer(real) {
		t.Fatal("linked libFuzzer binary should run in-process")
	}
}

func TestInstallEngineSeedFilesCounted(t *testing.T) {
	dir := t.TempDir()
	seeds := [][]byte{[]byte("HWIR-wrap"), []byte("HWIR-wrap"), []byte{}, bytes.Repeat([]byte("A"), 70000)}
	n, err := InstallEngineSeedFiles(dir, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("written=%d want 1 (dup + empty + oversize dropped)", n)
	}
	got, err := countCorpusFiles(dir)
	if err != nil || got != 1 {
		t.Fatalf("corpus files=%d err=%v", got, err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	for _, e := range ents {
		if !e.IsDir() {
			name = e.Name()
		}
	}
	if !strings.HasPrefix(name, "claim-seed-") || !strings.HasSuffix(name, ".bin") {
		t.Fatalf("name=%s", name)
	}
}

func TestDedicatedLibFuzzerHarness(t *testing.T) {
	root := RepoRoot()
	if got := DedicatedLibFuzzerHarness(root, "cjson"); got == "" {
		t.Fatal("expected cjson dedicated harness")
	}
	if got := DedicatedLibFuzzerHarness(root, "nonexistent-target-xyz"); got != "" {
		t.Fatalf("unexpected harness: %s", got)
	}
}

func TestCacheHitRejectsSymlinkAndStdinStub(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "cache.bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if cachedDedicatedLibFuzzerBin(link) {
		t.Fatal("symlink must not be a cache hit")
	}
	body, err := os.ReadFile(outside)
	if err != nil || string(body) != "secret" {
		t.Fatalf("cache hit followed the symlink: %q err=%v", body, err)
	}
	stub := filepath.Join(dir, "stub.bin")
	if err := os.WriteFile(stub, []byte("from stdin_subprocess_libfuzzer.c\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cachedDedicatedLibFuzzerBin(stub) {
		t.Fatal("stdin stub must not be a cache hit")
	}
}

func TestIsSubprocessStubLibFuzzerBin(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub.bin")
	realish := filepath.Join(dir, "real.bin")
	if err := os.WriteFile(stub, []byte("xxstdin_subprocess_libfuzzer.c yy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realish, []byte("cwalk_libfuzzer LLVMFuzzerTestOneInput"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSubprocessStubLibFuzzerBin(stub) {
		t.Fatal("expected stub detection")
	}
	if isSubprocessStubLibFuzzerBin(realish) {
		t.Fatal("dedicated-style bin must not look like stub")
	}
}

// High: after harness unlinks+symlinks session.stderr, host must still read the open fd.
func TestReadOpenSessionLogIgnoresPathSymlinkSwap(t *testing.T) {
	if testing.Short() {
		// still cheap — keep on
	}
	dir := t.TempDir()
	stderrPath := filepath.Join(dir, "session.stderr")
	f, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("legit-session-output\n"); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.env")
	if err := os.WriteFile(secret, []byte("TOKEN=leak\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stderrPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, stderrPath); err != nil {
		t.Fatal(err)
	}
	got := readOpenSessionLog(f)
	if got != "legit-session-output\n" {
		t.Fatalf("fd read must keep original content, got %q", got)
	}
	// Path re-open would follow the symlink — that is the bug class we refuse.
	b, err := os.ReadFile(stderrPath)
	if err != nil || string(b) != "TOKEN=leak\n" {
		t.Fatalf("precondition: path follow should see secret, got %q err=%v", b, err)
	}
	if isSubprocessStubLibFuzzerBin(stderrPath) {
		t.Fatal("symlink cache bin must not be treated as regular stub")
	}
}

func TestLibFuzzerSeedDirPaths(t *testing.T) {
	dir := t.TempDir()
	target := "jsmn"
	if got := LibFuzzerSeedDir(dir, target); got != filepath.Join(dir, ".cache", "hunt-lf-seeds", target) {
		t.Fatalf("seed dir=%q", got)
	}
	if got := LibFuzzerImportBinPath(dir, target); got != filepath.Join(dir, ".cache", "hunt-lf-import", target+"-libfuzzer-asan") {
		t.Fatalf("bin path=%q", got)
	}
	if got := PersistentLibFuzzerCorpusDir(dir, target); got != filepath.Join(dir, "reports", "oss-cve-libfuzzer", target, "corpus") {
		t.Fatalf("persist corpus=%q", got)
	}
}

func TestCatalogTargetSegmentRejectsTraversal(t *testing.T) {
	if catalogTargetSegment("../evil") != "_" {
		t.Fatalf("traversal must collapse to _")
	}
	if catalogTargetSegment("jsmn/../x") != "_" {
		t.Fatalf("embedded sep must collapse to _")
	}
	if DedicatedLibFuzzerHarness(RepoRoot(), "../evil") != "" {
		t.Fatal("dedicated harness must reject traversal ids")
	}
	if _, _, err := BuildLibFuzzerImport(context.Background(), RepoRoot(), "../../etc"); err == nil {
		t.Fatal("BuildLibFuzzerImport must reject invalid target id")
	}
}

func TestBuildSubprocessLibFuzzer(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not available")
	}
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	root := RepoRoot()
	dir := t.TempDir()
	out := filepath.Join(dir, "subprocess-fuzzer")
	ctx := context.Background()
	if err := buildSubprocessLibFuzzer(ctx, root, out); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("missing fuzzer binary: %v", err)
	}
}

func TestRunLibFuzzerImportSessionSynthetic(t *testing.T) {
	dir := t.TempDir()
	target := "gate-jsmn"
	corpusDir := LibFuzzerImportCorpusDir(dir, target)
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seeds := [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`), []byte("x")}
	for i, b := range seeds {
		if err := os.WriteFile(filepath.Join(corpusDir, fmtSeedName(i)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := ExportLibFuzzerSeeds(dir, target, seeds)
	if err != nil || n != 3 {
		t.Fatalf("export=%d err=%v", n, err)
	}
	cfg := map[string]any{}
	merged, err := MergeLibFuzzerSeedCorpus(cfg, dir, target)
	if err != nil || merged != 3 {
		t.Fatalf("merge=%d err=%v cfg=%+v", merged, err, cfg)
	}
	if cfg["hunt_corpus_guided"] != true {
		t.Fatalf("expected guided defaults: %+v", cfg)
	}

	dir2 := t.TempDir()
	target2 := "session-import"
	corpus2 := LibFuzzerImportCorpusDir(dir2, target2)
	if err := os.MkdirAll(corpus2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corpus2, "a.bin"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-seed AFL artifact that must survive LF import merge (no wipe).
	aflDir := LibFuzzerSeedDir(dir2, target2)
	if err := os.MkdirAll(aflDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aflDir, "afl-keep.bin"), []byte("afl-keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	n2, err := ImportLibFuzzerCorpusFromSession(dir2, target2)
	if err != nil || n2 != 1 {
		t.Fatalf("session import=%d err=%v", n2, err)
	}
	if _, err := os.Stat(filepath.Join(aflDir, "afl-keep.bin")); err != nil {
		t.Fatalf("AFL seed wiped by LF import: %v", err)
	}
}

func fmtSeedName(i int) string {
	return fmt.Sprintf("seed-%02d.bin", i)
}
