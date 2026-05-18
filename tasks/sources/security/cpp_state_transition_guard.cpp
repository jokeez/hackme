extern "C" int check(long long n) {
    // Same predicate as rust_state_transition_guard.rs
    unsigned long long u = (unsigned long long)n;
    unsigned long long from = (u >> 3) & 0x7ULL;
    unsigned long long to = (u >> 9) & 0x7ULL;
    unsigned long long token = u & 0xFFULL;
    return (from == 1ULL && to == 4ULL && token == 0xA5ULL) ? 1 : 0;
}
