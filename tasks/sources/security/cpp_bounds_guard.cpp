extern "C" int check(long long n) {
    // Same predicate as rust_bounds_guard.rs
    return (n >= 10000000LL && n <= 40000000LL && (n % 97LL) == 0LL) ? 1 : 0;
}
