/*
 * Bitcoin Core — SegWit witness stack push cap (interpreter.cpp, VerifyWitnessProgram).
 * check(n): 1 when any witness stack element size > MAX_SCRIPT_ELEMENT_SIZE.
 *
 * Port of:
 *   for (const valtype& elem : stack)
 *       if (elem.size() > MAX_SCRIPT_ELEMENT_SIZE)
 *           return SCRIPT_ERR_PUSH_SIZE;
 *
 * Models up to 4 witness element sizes (16-bit lanes) packed in n.
 */
#include "bitcoin_script_common.h"

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    for (int i = 0; i < 4; i++) {
        unsigned int elem_size = (unsigned int)((u >> (16 * i)) & 0xffffu);
        if (elem_size > MAX_SCRIPT_ELEMENT_SIZE) {
            return 1; /* SCRIPT_ERR_PUSH_SIZE — witness stack */
        }
    }
    return 0;
}
