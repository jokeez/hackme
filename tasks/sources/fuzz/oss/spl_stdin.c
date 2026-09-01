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
	buf[n] = '\0';
	json_init();
	parse_result pr = json_parse(buf);
	parse_result_free(pr);
	json_free();
	return 0;
}
