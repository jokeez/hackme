#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <uriparser/Uri.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	UriUriA uri;
	const char *error = NULL;
	if (uriParseSingleUriA(&uri, buf, &error) == URI_SUCCESS) {
		uriFreeUriMembersA(&uri);
	}
	return 0;
}
