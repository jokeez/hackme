#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "duktape.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	duk_context *ctx = duk_create_heap_default();
	if (!ctx) {
		return 0;
	}
	duk_push_lstring(ctx, buf, n);
	(void)duk_pcompile(ctx, DUK_COMPILE_EVAL);
	duk_pop(ctx);
	duk_destroy_heap(ctx);
	return 0;
}
