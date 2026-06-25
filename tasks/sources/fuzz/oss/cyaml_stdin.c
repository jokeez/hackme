#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cyaml.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	cyaml_doc_t *doc = cyaml_parse(buf, n, NULL, NULL);
	if (doc) {
		cyaml_free(doc);
	}
	return 0;
}
