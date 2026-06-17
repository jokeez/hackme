/*
 * Bitcoin Core — EvalScript stack depth (interpreter.cpp, SCRIPT_ERR_STACK_SIZE).
 * check(n): 1 when main stack + alt stack exceed MAX_STACK_SIZE during evaluation.
 *
 * Port of:
 *   if (stack.size() + altstack.size() > MAX_STACK_SIZE)
 *       return set_error(serror, SCRIPT_ERR_STACK_SIZE);
 *
 * Low 12 bits = simulated main-stack depth; next 12 bits = alt-stack depth.
 * Upper script bytes walked: each push opcode adds one stack element (EvalScript push path).
 */
#include "bitcoin_script_common.h"

#define MAX_STACK_SIZE 1000u

static void script_from_i64(long long n, unsigned char out[6]) {
    unsigned long long u = (unsigned long long)n;
    for (int i = 0; i < 6; i++) {
        out[i] = (unsigned char)((u >> (8 * (i + 3))) & 0xff);
    }
}

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    unsigned int main_sz = (unsigned int)(u & 0xfffu);
    unsigned int alt_sz = (unsigned int)((u >> 12) & 0xfffu);
    unsigned int stack_depth = main_sz + alt_sz;

    unsigned char script[6];
    script_from_i64(n, script);
    const unsigned char *pc = script;
    const unsigned char *end = script + 6;

    while (pc < end) {
        opcode_t opcode;
        unsigned int push_len = 0;
        if (!bitcoin_GetScriptOp(&pc, end, &opcode, &push_len)) {
            return 1; /* SCRIPT_ERR_BAD_OPCODE */
        }
        if (opcode <= OP_PUSHDATA4) {
            stack_depth++;
            if (stack_depth > MAX_STACK_SIZE) {
                return 1; /* SCRIPT_ERR_STACK_SIZE */
            }
        }
    }

    if (stack_depth > MAX_STACK_SIZE) {
        return 1; /* SCRIPT_ERR_STACK_SIZE */
    }
    return 0;
}
