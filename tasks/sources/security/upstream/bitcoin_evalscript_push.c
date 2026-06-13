/*
 * Bitcoin Core — EvalScript push size (interpreter.cpp, SCRIPT_ERR_PUSH_SIZE).
 * check(n): 1 when vchPushValue.size() > MAX_SCRIPT_ELEMENT_SIZE after GetOp.
 *
 * Port of:
 *   if (!script.GetOp(pc, opcode, vchPushValue))
 *       return SCRIPT_ERR_BAD_OPCODE;
 *   if (vchPushValue.size() > MAX_SCRIPT_ELEMENT_SIZE)
 *       return SCRIPT_ERR_PUSH_SIZE;
 *
 * Script bytes = 8 LE bytes from n; walks all ops like the EvalScript loop.
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
            return 1; /* SCRIPT_ERR_BAD_OPCODE */
        }
        if (push_len > MAX_SCRIPT_ELEMENT_SIZE) {
            return 1; /* SCRIPT_ERR_PUSH_SIZE */
        }
    }
    return 0;
}
