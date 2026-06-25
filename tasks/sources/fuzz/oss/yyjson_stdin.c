#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "yyjson.h"

int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	yyjson_doc *doc = yyjson_read((const char *)buf, n, 0);
	if (doc) {
		yyjson_doc_free(doc);
	}
	return 0;
}
