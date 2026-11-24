package hunt

import "testing"

// The execs field is the operator's throughput signal for a lease. Progress lines are
// tab-separated ("#2\tINITED cov: ..."), and a fork run leaves nothing else in the
// parent's stderr, so parsing them has to work on the real shape.
func TestLibFuzzerExecutedUnitsReadsRealProgressLines(t *testing.T) {
	stderr := "INFO: Seed: 1\n" +
		"#2\tINITED cov: 4 ft: 5 corp: 1/1b exec/s: 0 rss: 32Mb\n" +
		"#131\tNEW    cov: 14 ft: 16 corp: 5/14b lim: 4 exec/s: 0 rss: 33Mb L: 3/4\n" +
		"#1377\tREDUCE cov: 14 ft: 18 corp: 6/18b lim: 4 exec/s: 0 rss: 33Mb\n" +
		"#1024\tpulse  cov: 14 ft: 18 corp: 6/18b exec/s: 512 rss: 33Mb\n"
	if got := libFuzzerExecutedUnits(stderr); got != 1377 {
		t.Fatalf("execs = %d, want 1377 (highest unit number on a cov: line)", got)
	}
}

func TestLibFuzzerExecutedUnitsPrefersFinalStats(t *testing.T) {
	stderr := "#8\tNEW    cov: 3 ft: 4 corp: 2/2b\n" +
		"stat::number_of_executed_units: 90210\n"
	if got := libFuzzerExecutedUnits(stderr); got != 90210 {
		t.Fatalf("execs = %d, want the final stat 90210", got)
	}
}

func TestLibFuzzerExecutedUnitsSumsForkJobs(t *testing.T) {
	stderr := "stat::number_of_executed_units: 1000\nstat::number_of_executed_units: 2500\n"
	if got := libFuzzerExecutedUnits(stderr); got != 3500 {
		t.Fatalf("execs = %d, want 3500 (per-job stats summed)", got)
	}
}

func TestLibFuzzerExecutedUnitsIgnoresNoise(t *testing.T) {
	for _, stderr := range []string{
		"",
		"INFO: Running with entropic power schedule\n",
		"#not-a-number\tNEW cov: 1\n",
		"#12\tsomething without coverage\n",
		"==1234==ERROR: AddressSanitizer: heap-buffer-overflow\n",
	} {
		if got := libFuzzerExecutedUnits(stderr); got != 0 {
			t.Errorf("execs = %d for %q, want 0", got, stderr)
		}
	}
}
