/*
 * Bitcoin Core — EvalScript opcode budget (interpreter.cpp, SCRIPT_ERR_OP_COUNT).
 * check(n): 1 when nOpCount > MAX_OPS_PER_SCRIPT during script evaluation.
 *
 * Port of:
 *   if (opcode > OP_16 && ++nOpCount > MAX_OPS_PER_SCRIPT)
 *       return SCRIPT_ERR_OP_COUNT;
 *
 * Low byte of n = repeat count of OP_NOP (0x61, counts toward budget).
 * Upper bytes = mini script walked via GetScriptOp (same as EvalScript loop).
 */
#include "bitcoin_script_common.h"

#define MAX_OPS_PER_SCRIPT 201u
#define OP_16 0x60u
#define OP_NOP 0x61u

static void script_tail_from_i64(long long n, unsigned char out[6]) {
    unsigned long long u = (unsigned long long)n;
    for (int i = 0; i < 6; i++) {
        out[i] = (unsigned char)((u >> (8 * (i + 1))) & 0xff);
    }
}

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    unsigned int repeat = (unsigned int)(u & 0xffu);
    unsigned int nOpCount = 0;

    for (unsigned int i = 0; i < repeat; i++) {
        opcode_t opcode = OP_NOP;
        if (opcode > OP_16 && ++nOpCount > MAX_OPS_PER_SCRIPT) {
            return 1; /* SCRIPT_ERR_OP_COUNT */
        }
    }

    unsigned char script[6];
    script_tail_from_i64(n, script);
    const unsigned char *pc = script;
    const unsigned char *end = script + 6;

    while (pc < end) {
        opcode_t opcode;
        unsigned int push_len = 0;
        if (!bitcoin_GetScriptOp(&pc, end, &opcode, &push_len)) {
            return 1; /* SCRIPT_ERR_BAD_OPCODE */
        }
        if (opcode > OP_16 && ++nOpCount > MAX_OPS_PER_SCRIPT) {
            return 1; /* SCRIPT_ERR_OP_COUNT */
        }
    }
    return 0;
}
