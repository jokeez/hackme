/*
 * go-ethereum — core/state_transition.go:
 *   value, overflow := uint256.FromBig(tx.Value()); if overflow { ... }
 * Probe: low 64b = tx value, high 32b of u = balance slice; __builtin_add_overflow on uint64.
 */
__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    unsigned long long txv = u;
    unsigned long long bal = u >> 32;
    unsigned long long sum;
    if (__builtin_add_overflow(txv, bal, &sum)) {
        return 1;
    }
    return 0;
}
