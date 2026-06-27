#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "quickjs.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	JSRuntime *rt = JS_NewRuntime();
	if (!rt) {
		return 0;
	}
	JSContext *ctx = JS_NewContext(rt);
	if (!ctx) {
		JS_FreeRuntime(rt);
		return 0;
	}
	(void)JS_Eval(ctx, buf, n, "stdin", JS_EVAL_TYPE_GLOBAL);
	JS_FreeContext(ctx);
	JS_FreeRuntime(rt);
	return 0;
}
