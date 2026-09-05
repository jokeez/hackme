#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "frozen.h"

static void walk_cb(void *data, const char *name, size_t name_len, const char *path,
		    const struct json_token *token) {
	(void)data;
	(void)name;
	(void)name_len;
	(void)path;
	(void)token;
}

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	(void)json_walk(buf, (int)n, walk_cb, NULL);
	return 0;
}
