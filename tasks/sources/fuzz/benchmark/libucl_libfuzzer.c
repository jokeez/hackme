#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "ucl.h"

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size > 65536) {
		size = 65536;
	}
	struct ucl_parser *parser = ucl_parser_new(0);
	if (!parser) {
		return 0;
	}
	(void)ucl_parser_add_chunk(parser, (const unsigned char *)data, size);
	ucl_object_t *obj = ucl_parser_get_object(parser);
	if (obj) {
		ucl_object_unref(obj);
	}
	ucl_parser_free(parser);
	return 0;
}
