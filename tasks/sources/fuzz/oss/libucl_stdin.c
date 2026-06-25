#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ucl.h"

int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	struct ucl_parser *parser = ucl_parser_new(0);
	if (!parser) {
		return 1;
	}
	(void)ucl_parser_add_chunk(parser, buf, n);
	ucl_object_t *obj = ucl_parser_get_object(parser);
	if (obj) {
		ucl_object_unref(obj);
	}
	ucl_parser_free(parser);
	return 0;
}
