#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "tomlc17.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	toml_result_t r = toml_parse(buf, n);
	toml_free(r);
	return 0;
}
