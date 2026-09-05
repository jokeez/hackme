#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "zip.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	struct zip_t *z = zip_stream_open(buf, n, 0, 'r');
	if (z) {
		zip_stream_close(z);
	}
	return 0;
}
