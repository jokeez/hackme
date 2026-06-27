#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "mxml.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	mxml_options_t *opts = mxmlOptionsNew();
	if (!opts) {
		return 0;
	}
	mxml_node_t *tree = mxmlLoadString(NULL, opts, buf);
	if (tree) {
		mxmlDelete(tree);
	}
	mxmlOptionsDelete(opts);
	return 0;
}
