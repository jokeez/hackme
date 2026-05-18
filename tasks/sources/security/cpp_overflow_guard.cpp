extern "C" int check(long long n) {
    // Same predicate as rust_overflow_guard.rs
    unsigned long long u = (unsigned long long)n;
    unsigned long long x = u * 0x9E3779B97F4A7C15ULL;
    return (((x >> 56) == 0ULL) && ((x & 0x3FFULL) == 0x155ULL)) ? 1 : 0;
}
