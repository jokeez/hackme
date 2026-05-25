extern "C" int check(long long n) {
    if (n <= 0) {
        return 0;
    }
    unsigned op = (unsigned)(n & 0xff);
    unsigned claimed_len = (unsigned)((n >> 8) & 0xffff);
    if (op == 0x4c && claimed_len > 520) {
        return 1;
    }
    return 0;
}
