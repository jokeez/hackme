#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "parson.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	JSON_Value *v = json_parse_string(buf);
	if (v) {
		json_value_free(v);
	}
	return 0;
}
