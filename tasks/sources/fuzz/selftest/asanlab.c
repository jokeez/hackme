#include "asanlab.h"

#include <stdlib.h>
#include <string.h>

/* Global victim for the global-buffer-overflow case. */
static unsigned char g_table[16] = {0};

/* LLVM removes an allocation whose result is never observed, which would quietly
 * delete the double-free and the leak before the sanitizer ever sees them. Parking
 * every pointer and result in volatile sinks keeps the bugs in the binary. */
static void *volatile g_sink_ptr;
static volatile int g_sink_int;

/* A leak the optimiser cannot delete and the leak checker still sees as lost: the
 * pointer escapes to a volatile global (so the allocation must stay) and is then
 * dropped (so nothing reaches it at exit). */
__attribute__((noinline)) static size_t asanlab_leak_alloc(size_t want) {
	unsigned char *b = (unsigned char *)malloc(want);
	if (!b) {
		return 0;
	}
	memset(b, 7, want);
	g_sink_ptr = b;
	g_sink_ptr = NULL;
	return want;
}

const char *asanlab_version(void) {
	return "asanlab/1";
}

/* Clean path: a list of length-prefixed records. Deliberately correct — this is the
 * code a fuzzer spends most of its time in, so a finding here would be a real bug. */
static int parse_records(const uint8_t *data, size_t size) {
	size_t off = 0;
	int n = 0;
	while (off < size) {
		size_t len = data[off];
		off++;
		if (len == 0) {
			continue;
		}
		if (off + len > size) {
			return -1; /* truncated record */
		}
		/* Fold the record into a checksum so the work is not optimised away. */
		unsigned acc = 0;
		for (size_t i = 0; i < len; i++) {
			acc = acc * 31u + data[off + i];
		}
		g_table[acc % (sizeof(g_table) - 1)] ^= (unsigned char)(acc & 0xff);
		off += len;
		n++;
		if (n > 4096) {
			break;
		}
	}
	return n;
}

int asanlab_parse(const uint8_t *data, size_t size) {
	if (data == NULL || size < 3) {
		return -1;
	}
	if (data[0] != 'A' || data[1] != 'L') {
		return parse_records(data, size);
	}
	const uint8_t op = data[2];
	const uint8_t *p = data + 3;
	const size_t n = size - 3;

	switch (op) {
	case ASANLAB_OP_HEAP_READ: {
		/* Guarded by a magic byte so random input does not stumble in immediately. */
		if (n < 2 || p[0] != 0x7f) {
			return 0;
		}
		unsigned char *buf = (unsigned char *)malloc(8);
		if (!buf) {
			return -1;
		}
		memset(buf, 0, 8);
		size_t idx = 8 + (p[1] & 0x07); /* one-past-the-end read */
		int v = buf[idx];
		free(buf);
		return v;
	}
	case ASANLAB_OP_HEAP_WRITE: {
		if (n < 2 || p[0] != 0x7e) {
			return 0;
		}
		unsigned char *buf = (unsigned char *)malloc(8);
		if (!buf) {
			return -1;
		}
		g_sink_ptr = buf;
		size_t idx = 8 + (p[1] & 0x07);
		buf[idx] = p[1];        /* off-by-N write */
		g_sink_int = buf[idx];  /* read it back so the store is not dead */
		free(buf);
		return g_sink_int;
	}
	case ASANLAB_OP_USE_AFTER_FREE: {
		if (n < 1 || p[0] != 0x55) {
			return 0;
		}
		unsigned char *buf = (unsigned char *)malloc(16);
		if (!buf) {
			return -1;
		}
		memset(buf, 1, 16);
		g_sink_ptr = buf;
		free(buf);
		return ((unsigned char *)g_sink_ptr)[3]; /* read after free */
	}
	case ASANLAB_OP_DOUBLE_FREE: {
		if (n < 1 || p[0] != 0x44) {
			return 0;
		}
		void *buf = malloc(24);
		if (!buf) {
			return -1;
		}
		memset(buf, 9, 24);
		g_sink_ptr = buf;
		free(buf);
		free(g_sink_ptr); /* same pointer, freed twice */
		return 2;
	}
	case ASANLAB_OP_STACK_OVERFLOW: {
		if (n < 2 || p[0] != 0x33) {
			return 0;
		}
		unsigned char frame[12];
		memset(frame, 0, sizeof(frame));
		size_t idx = sizeof(frame) + (p[1] & 0x03);
		frame[idx] = p[1]; /* past the end of the stack object */
		return frame[0];
	}
	case ASANLAB_OP_GLOBAL_OVERFLOW: {
		if (n < 2 || p[0] != 0x22) {
			return 0;
		}
		size_t idx = sizeof(g_table) + (p[1] & 0x03);
		g_table[idx] = p[1]; /* past the end of a global */
		return 3;
	}
	case ASANLAB_OP_LEAK: {
		if (n < 1 || p[0] != 0x11) {
			return 0;
		}
		if (asanlab_leak_alloc(321) == 0) {
			return -1;
		}
		return 4;
	}
	case ASANLAB_OP_NULL_DEREF: {
		if (n < 1 || p[0] != 0x00) {
			return 0;
		}
		volatile unsigned char *q = NULL;
		return *q; /* SEGV on unknown address */
	}
	case ASANLAB_OP_SIGNED_OVERFLOW: {
		if (n < 1 || p[0] != 0x66) {
			return 0;
		}
		int32_t v = 2147483600;
		int32_t add = 100 + (int32_t)(p[0] & 0x0f);
		return (int)(v + add); /* signed overflow */
	}
	case ASANLAB_OP_SHIFT_OOB: {
		if (n < 2 || p[0] != 0x77) {
			return 0;
		}
		uint32_t v = 1u;
		int sh = 32 + (p[1] & 0x1f);
		return (int)(v << sh); /* shift exponent >= width */
	}
	case ASANLAB_OP_DIV_ZERO: {
		/* The divisor comes from the input and must be zero: keeps the division a
		 * runtime fault instead of something the compiler folds at build time. */
		if (n < 2 || p[0] != 0x88 || p[1] != 0x00) {
			return 0;
		}
		int32_t d = (int32_t)p[1];
		int32_t num = 1000 + (int32_t)n;
		return (int)(num / d);
	}
	case ASANLAB_OP_RECORDS:
	default:
		return parse_records(p, n);
	}
}
