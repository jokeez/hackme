#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "lz4.h"

int main(void) {
	static char buf[65537];
	static char out[262144];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	(void)LZ4_decompress_safe(buf, out, (int)n, (int)sizeof(out));
	return 0;
}
