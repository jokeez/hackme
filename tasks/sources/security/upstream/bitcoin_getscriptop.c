/*
 * Bitcoin Core — script.cpp GetScriptOp + MAX_SCRIPT_ELEMENT_SIZE (script.h).
 * check(n): 1 if parse fails or push element > 520 bytes (consensus-class signal).
 * Script bytes = 8 LE bytes from n.
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
    const unsigned char *pc = script;
    const unsigned char *end = script + 8;
    while (pc < end) {
        opcode_t opcode;
        unsigned int push_len = 0;
        if (!bitcoin_GetScriptOp(&pc, end, &opcode, &push_len)) {
            return 1;
        }
        if (opcode <= OP_PUSHDATA4 && push_len > MAX_SCRIPT_ELEMENT_SIZE) {
            return 1;
        }
    }
    return 0;
}
