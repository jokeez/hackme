#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "confini.h"

static int lci_cb(IniDispatch *dispatch, void *user) {
	(void)dispatch;
	(void)user;
	return 1;
}

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	char cache[65538];
	memcpy(cache, buf, n);
	cache[n] = '\0';
	IniFormat fmt = INI_DEFAULT_FORMAT;
	(void)strip_ini_cache(cache, n, fmt, NULL, lci_cb, NULL);
	return 0;
}
