#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "json-dom.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	VALUE root;
	if (json_dom_parse(buf, n, NULL, 0, &root, NULL) == 0) {
		value_fini(&root);
	}
	return 0;
}
