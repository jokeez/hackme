#define JSON_IMPLEMENTATION
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "json.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	struct json_value_s *j = json_parse(buf, n);
	(void)j;
	return 0;
}
