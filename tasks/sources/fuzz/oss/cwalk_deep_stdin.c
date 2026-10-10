#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cwalk.h"

/* Deep v1: normalize / join / get_absolute sequences from split input. */
int main(void) {
	static char buf[65537];
	static char a[32768];
	static char b[32768];
	static char out[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';

	/* Split on first NUL or 0x1f unit separator into two path halves. */
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
	return 0;
}
