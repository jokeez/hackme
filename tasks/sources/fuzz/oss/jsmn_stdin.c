#define JSMN_STATIC
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "jsmn.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	jsmn_parser parser;
	jsmntok_t tokens[512];
	jsmn_init(&parser);
	(void)jsmn_parse(&parser, buf, n, tokens, 512);
	return 0;
}
