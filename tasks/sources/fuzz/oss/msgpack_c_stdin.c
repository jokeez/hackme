#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <msgpack.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	msgpack_unpacked result;
	msgpack_unpacked_init(&result);
	size_t off = 0;
	while (off < n) {
		if (msgpack_unpack_next(&result, buf, n, &off) != MSGPACK_UNPACK_SUCCESS) {
			break;
		}
		msgpack_unpacked_destroy(&result);
		msgpack_unpacked_init(&result);
	}
	msgpack_unpacked_destroy(&result);
	return 0;
}
