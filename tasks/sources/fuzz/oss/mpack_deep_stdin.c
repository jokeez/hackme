#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "mpack/mpack.h"

/* Deep v1: unpack → walk → re-encode. Process-local buffers are zeroed each run
 * so leftover pack state cannot bleed into the next Hunt exec (fresh process). */
int main(void) {
	char buf[65537];
	char out[65537];
	memset(buf, 0, sizeof(buf));
	memset(out, 0, sizeof(out));

	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}

	mpack_tree_t tree;
	memset(&tree, 0, sizeof(tree));
	mpack_tree_init_data(&tree, buf, n);
	mpack_tree_parse(&tree);
	mpack_node_t root = mpack_tree_root(&tree);
	if (mpack_tree_error(&tree) != mpack_ok) {
		(void)mpack_tree_destroy(&tree);
		memset(buf, 0, sizeof(buf));
		return 0;
	}

	mpack_writer_t writer;
	memset(&writer, 0, sizeof(writer));
	mpack_writer_init(&writer, out, sizeof(out));

	switch (mpack_node_type(root)) {
	case mpack_type_nil:
		mpack_write_nil(&writer);
		break;
	case mpack_type_bool:
		mpack_write_bool(&writer, mpack_node_bool(root));
		break;
	case mpack_type_int:
		mpack_write_i64(&writer, mpack_node_i64(root));
		break;
	case mpack_type_uint:
		mpack_write_u64(&writer, mpack_node_u64(root));
		break;
	case mpack_type_str: {
		const char *s = mpack_node_str(root);
		uint32_t len = mpack_node_data_len(root);
		if (s) {
			mpack_write_str(&writer, s, len);
		}
		break;
	}
	case mpack_type_bin: {
		const char *s = mpack_node_bin_data(root);
		uint32_t len = mpack_node_data_len(root);
		if (s) {
			mpack_write_bin(&writer, s, len);
		}
		break;
	}
	case mpack_type_array: {
		size_t count = mpack_node_array_length(root);
		if (count > 64) {
			count = 64;
		}
		mpack_start_array(&writer, (uint32_t)count);
		for (size_t i = 0; i < count; i++) {
			mpack_node_t child = mpack_node_array_at(root, i);
			mpack_write_u8(&writer, (uint8_t)mpack_node_type(child));
		}
		mpack_finish_array(&writer);
		break;
	}
	case mpack_type_map: {
		size_t count = mpack_node_map_count(root);
		if (count > 64) {
			count = 64;
		}
		mpack_start_map(&writer, (uint32_t)count);
		for (size_t i = 0; i < count; i++) {
			mpack_node_t key = mpack_node_map_key_at(root, i);
			mpack_node_t val = mpack_node_map_value_at(root, i);
			mpack_write_u8(&writer, (uint8_t)mpack_node_type(key));
			mpack_write_u8(&writer, (uint8_t)mpack_node_type(val));
		}
		mpack_finish_map(&writer);
		break;
	}
	default:
		mpack_write_nil(&writer);
		break;
	}

	(void)mpack_writer_destroy(&writer);
	(void)mpack_tree_destroy(&tree);
	memset(buf, 0, sizeof(buf));
	memset(out, 0, sizeof(out));
	return 0;
}
