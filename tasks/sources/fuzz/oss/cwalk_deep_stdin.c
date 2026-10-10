#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cwalk.h"

/* Deep v1: join/normalize sequences. Stack buffers zeroed each process run. */
int main(void) {
	char buf[65537];
	char a[32768];
	char b[32768];
	char out[65537];
	memset(buf, 0, sizeof(buf));
	memset(a, 0, sizeof(a));
	memset(b, 0, sizeof(b));
	memset(out, 0, sizeof(out));

	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';

	size_t split = n;
	for (size_t i = 0; i < n; i++) {
		if (buf[i] == '\0' || (unsigned char)buf[i] == 0x1f) {
			split = i;
			break;
		}
	}
	size_t alen = split < sizeof(a) - 1 ? split : sizeof(a) - 1;
	memcpy(a, buf, alen);
	a[alen] = '\0';
	size_t boff = split < n ? split + 1 : n;
	size_t blen = n > boff ? n - boff : 0;
	if (blen >= sizeof(b)) {
		blen = sizeof(b) - 1;
	}
	memcpy(b, buf + boff, blen);
	b[blen] = '\0';

	(void)cwk_path_normalize(a, out, sizeof(out));
	(void)cwk_path_get_absolute("/", a, out, sizeof(out));
	if (blen > 0) {
		(void)cwk_path_join(a, b, out, sizeof(out));
		(void)cwk_path_normalize(out, out, sizeof(out));
		(void)cwk_path_get_absolute(a, b, out, sizeof(out));
	}

	memset(buf, 0, sizeof(buf));
	memset(a, 0, sizeof(a));
	memset(b, 0, sizeof(b));
	memset(out, 0, sizeof(out));
	return 0;
}
