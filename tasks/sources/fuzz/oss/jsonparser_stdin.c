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
	json_value *value = json_parse(buf, n);
	if (value != NULL) {
		json_value_free(value);
	}
	return 0;
}
