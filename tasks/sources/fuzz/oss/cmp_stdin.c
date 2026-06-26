#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cmp.h"

static size_t cmp_buf_pos;
static size_t cmp_buf_len;
static const uint8_t *cmp_buf;

static bool cmp_read_cb(cmp_ctx_t *ctx, void *data, size_t limit) {
	(void)ctx;
	if (cmp_buf_pos + limit > cmp_buf_len) {
		limit = cmp_buf_len - cmp_buf_pos;
	}
	if (limit == 0) {
		return false;
	}
	memcpy(data, cmp_buf + cmp_buf_pos, limit);
	cmp_buf_pos += limit;
	return true;
}

static bool cmp_skip_cb(cmp_ctx_t *ctx, size_t count) {
	(void)ctx;
	if (cmp_buf_pos + count > cmp_buf_len) {
		count = cmp_buf_len - cmp_buf_pos;
	}
	cmp_buf_pos += count;
	return true;
}

static size_t cmp_write_cb(cmp_ctx_t *ctx, const void *data, size_t count) {
	(void)ctx;
	(void)data;
	(void)count;
	return count;
}

int main(void) {
	static uint8_t buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	cmp_buf = buf;
	cmp_buf_len = n;
	cmp_buf_pos = 0;
	cmp_ctx_t ctx;
	cmp_init(&ctx, NULL, cmp_read_cb, cmp_skip_cb, cmp_write_cb);
	for (int i = 0; i < 10000; i++) {
		cmp_object_t obj;
		if (!cmp_read_object(&ctx, &obj)) {
			break;
		}
	}
	return 0;
}
