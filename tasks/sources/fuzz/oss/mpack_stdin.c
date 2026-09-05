#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "mpack.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	mpack_tree_t tree;
	mpack_tree_init_data(&tree, buf, n);
	mpack_tree_parse(&tree);
	(void)mpack_tree_destroy(&tree);
	return 0;
}
