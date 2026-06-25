#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "cfgpack/msgpack.h"

int main(void) {
	static uint8_t buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	cfgpack_reader_t r;
	cfgpack_reader_init(&r, buf, n);
	while (r.pos < r.len) {
		uint64_t u;
		int64_t i;
		float f;
		double d;
		const uint8_t *s;
		uint32_t slen;
		uint32_t map_len;
		size_t before = r.pos;
		if (cfgpack_msgpack_decode_uint64(&r, &u) == CFGPACK_OK) {
			continue;
		}
		r.pos = before;
		if (cfgpack_msgpack_decode_int64(&r, &i) == CFGPACK_OK) {
			continue;
		}
		r.pos = before;
		if (cfgpack_msgpack_decode_f32(&r, &f) == CFGPACK_OK) {
			continue;
		}
		r.pos = before;
		if (cfgpack_msgpack_decode_f64(&r, &d) == CFGPACK_OK) {
			continue;
		}
		r.pos = before;
		if (cfgpack_msgpack_decode_str(&r, &s, &slen) == CFGPACK_OK) {
			continue;
		}
		r.pos = before;
		if (cfgpack_msgpack_decode_map_header(&r, &map_len) == CFGPACK_OK) {
			continue;
		}
		r.pos++;
	}
	return 0;
}
