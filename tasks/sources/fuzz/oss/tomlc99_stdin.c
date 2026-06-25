#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "toml.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	char err[256];
	toml_table_t *tab = toml_parse(buf, err, sizeof(err));
	if (tab) {
		toml_free(tab);
	}
	return 0;
}
