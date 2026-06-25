#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define PRS_IMPLEMENTATION
#include "prs.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	prs_context_t ctx;
	prs_token_t tok;
	prs_init(&ctx, buf);
	int ntok = 0;
	while (prs_parse(&ctx, &tok) && ntok++ < 100000) {
	}
	return 0;
}
