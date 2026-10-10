#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "mpack/mpack.h"

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size == 0 || size > 65536) {
		return 0;
	}
	mpack_tree_t tree;
	mpack_tree_init_data(&tree, (const char *)data, size);
	mpack_tree_parse(&tree);
	(void)mpack_tree_destroy(&tree);
	return 0;
}
