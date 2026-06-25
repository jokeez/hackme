#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CJ5_IMPLEMENT
#include "cj5.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	cj5_token tokens[4096];
	(void)cj5_parse(buf, n, tokens, 4096);
	return 0;
}
