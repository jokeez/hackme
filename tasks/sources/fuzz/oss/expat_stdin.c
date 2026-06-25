#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <expat.h>

static void XMLCALL noop_handler(void *user, const char *s, int len) {
	(void)user;
	(void)s;
	(void)len;
}

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	XML_Parser parser = XML_ParserCreate(NULL);
	if (!parser) {
		return 1;
	}
	XML_SetCharacterDataHandler(parser, noop_handler);
	(void)XML_Parse(parser, buf, (int)n, 1);
	XML_ParserFree(parser);
	return 0;
}
