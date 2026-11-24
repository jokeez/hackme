/*
 * asanlab — a deliberately buggy parser used to prove the Dig/Hunt pipeline end to end.
 *
 * Every bug is reachable only through the parsed input, never on a fixed code path, so a
 * fuzzer has to discover it. Each one is a distinct sanitizer class, which is what makes
 * this useful for crash bucketing: one bug must collapse to one finding family no matter
 * how many different inputs reach it.
 *
 * Wire format: "AL" magic, one opcode byte, then opcode-specific payload.
 *   byte 0-1 : 'A' 'L'
 *   byte 2   : opcode (see ASANLAB_OP_*)
 *   byte 3.. : payload
 * Anything that does not start with the magic is parsed as a plain record list and is
 * expected to be clean — the fuzzer must earn the crashes.
 */
#ifndef ASANLAB_H
#define ASANLAB_H

#include <stddef.h>
#include <stdint.h>

#define ASANLAB_OP_RECORDS 0x00      /* clean path: parse length-prefixed records */
#define ASANLAB_OP_HEAP_READ 0x11    /* heap-buffer-overflow READ  */
#define ASANLAB_OP_HEAP_WRITE 0x12   /* heap-buffer-overflow WRITE */
#define ASANLAB_OP_USE_AFTER_FREE 0x13
#define ASANLAB_OP_DOUBLE_FREE 0x14
#define ASANLAB_OP_STACK_OVERFLOW 0x15
#define ASANLAB_OP_GLOBAL_OVERFLOW 0x16
#define ASANLAB_OP_LEAK 0x17
#define ASANLAB_OP_NULL_DEREF 0x18
#define ASANLAB_OP_SIGNED_OVERFLOW 0x19 /* UBSan: signed integer overflow */
#define ASANLAB_OP_SHIFT_OOB 0x1a       /* UBSan: shift exponent too large */
#define ASANLAB_OP_DIV_ZERO 0x1b        /* UBSan: integer division by zero */

/* Returns the number of records consumed, or -1 on malformed input. Crashes under a
 * sanitizer when the input selects a bug opcode and satisfies its guard. */
int asanlab_parse(const uint8_t *data, size_t size);

/* Version string, so a harness can assert it linked the library it thinks it did. */
const char *asanlab_version(void);

#endif /* ASANLAB_H */
