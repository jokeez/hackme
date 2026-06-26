#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <libxml/parser.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	xmlInitParser();
	(void)xmlReadMemory(buf, (int)n, "stdin", NULL, XML_PARSE_RECOVER | XML_PARSE_NOENT);
	xmlCleanupParser();
	return 0;
}
