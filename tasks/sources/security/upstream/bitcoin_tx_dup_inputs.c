/*
 * Bitcoin Core — CheckTransaction duplicate inputs (tx_check.cpp, CVE-2018-17144).
 * check(n): 1 on bad-txns-inputs-duplicate or bad-txns-prevout-null (simplified).
 *
 * Eight prevout key bytes from n (LE). Mirrors vInOutPoints insert + IsNull() checks.
 */
__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    unsigned char keys[8];
    for (int i = 0; i < 8; i++) {
        keys[i] = (unsigned char)((u >> (8 * i)) & 0xffu);
    }
    for (int i = 0; i < 8; i++) {
        if (keys[i] == 0u) {
            return 1; /* bad-txns-prevout-null / empty vin slot */
        }
        for (int j = 0; j < i; j++) {
            if (keys[i] == keys[j]) {
                return 1; /* bad-txns-inputs-duplicate */
            }
        }
    }
    return 0;
}
