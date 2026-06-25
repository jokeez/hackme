#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ini.h"

static int inih_cb(void *user, const char *section, const char *name, const char *value) {
	(void)user;
	(void)section;
	(void)name;
	(void)value;
	return 1;
}

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	(void)ini_parse_string(buf, inih_cb, NULL);
	return 0;
}
