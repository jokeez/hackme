/* Constants from Bitcoin Core script.h (MAX_SCRIPT_ELEMENT_SIZE, MAX_OPCODE, opcodes). */
#ifndef BITCOIN_SCRIPT_COMMON_H
#define BITCOIN_SCRIPT_COMMON_H

#define MAX_SCRIPT_ELEMENT_SIZE 520u
#define MAX_OPCODE 0xb9u /* OP_NOP10 */
#define OP_PUSHDATA1 0x4cu
#define OP_PUSHDATA2 0x4du
#define OP_PUSHDATA4 0x4eu

typedef unsigned char opcode_t;

/* Ported from script.cpp GetScriptOp — no std::vector; returns push payload size in *push_len. */
static int bitcoin_GetScriptOp(const unsigned char **ppc, const unsigned char *end,
                               opcode_t *opcode_out, unsigned int *push_len) {
    *opcode_out = 0xff;
    if (push_len) {
        *push_len = 0;
    }
    const unsigned char *pc = *ppc;
    if (pc >= end) {
        return 0;
    }
    if (end - pc < 1) {
        return 0;
    }
    unsigned int opcode = *pc++;
    if (opcode <= OP_PUSHDATA4) {
        unsigned int nSize = 0;
        if (opcode < OP_PUSHDATA1) {
            nSize = opcode;
        } else if (opcode == OP_PUSHDATA1) {
            if (end - pc < 1) {
                return 0;
            }
            nSize = *pc++;
        } else if (opcode == OP_PUSHDATA2) {
            if (end - pc < 2) {
                return 0;
            }
            nSize = (unsigned int)pc[0] | ((unsigned int)pc[1] << 8);
            pc += 2;
        } else if (opcode == OP_PUSHDATA4) {
            if (end - pc < 4) {
                return 0;
            }
            nSize = (unsigned int)pc[0] | ((unsigned int)pc[1] << 8) |
                    ((unsigned int)pc[2] << 16) | ((unsigned int)pc[3] << 24);
            pc += 4;
        }
        if (end - pc < 0 || (unsigned int)(end - pc) < nSize) {
            return 0;
        }
        if (push_len) {
            *push_len = nSize;
        }
        pc += nSize;
    }
    *opcode_out = (opcode_t)opcode;
    *ppc = pc;
    return 1;
}

/* Ported from CScript::HasValidOps() in script.cpp */
static int bitcoin_HasValidOps(const unsigned char *script, unsigned int len) {
    const unsigned char *it = script;
    const unsigned char *end = script + len;
    while (it < end) {
        opcode_t opcode;
        unsigned int item_size = 0;
        if (!bitcoin_GetScriptOp(&it, end, &opcode, &item_size) || opcode > MAX_OPCODE ||
            item_size > MAX_SCRIPT_ELEMENT_SIZE) {
            return 0;
        }
    }
    return 1;
}

#endif
