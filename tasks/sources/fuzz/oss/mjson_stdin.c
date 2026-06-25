#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "mjson.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	const char *tok;
	int len;
	(void)mjson_find(buf, (int)n, "$", &tok, &len);
	double num;
	(void)mjson_get_number(buf, (int)n, "$", &num);
	int off = 0;
	int koff, klen, voff, vlen, vtype;
	while (mjson_next(buf, (int)n, off, &koff, &klen, &voff, &vlen, &vtype)) {
		off = voff + vlen;
	}
	return 0;
}
