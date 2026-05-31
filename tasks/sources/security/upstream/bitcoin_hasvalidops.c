/*
 * Bitcoin Core — CScript::HasValidOps() (script.cpp).
 * check(n): 1 when HasValidOps would return false.
 */
#include "bitcoin_script_common.h"

static void script_from_i64(long long n, unsigned char out[8]) {
    unsigned long long u = (unsigned long long)n;
    for (int i = 0; i < 8; i++) {
        out[i] = (unsigned char)((u >> (8 * i)) & 0xff);
    }
}

__attribute__((export_name("check"))) int check(long long n) {
    unsigned char script[8];
    script_from_i64(n, script);
    return bitcoin_HasValidOps(script, 8) ? 0 : 1;
}
