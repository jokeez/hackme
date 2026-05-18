extern "C" int check(long long n) {
    // Same gate as Rust version for deterministic cross-language parity.
    return (n % 997LL == 0) ? 1 : 0;
}
