#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "miniz.h"

int main(void) {
	static unsigned char buf[65537];
	static unsigned char out[131072];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n < 2) {
		return 0;
	}
	mz_ulong out_len = (mz_ulong)sizeof(out);
	(void)mz_uncompress(out, &out_len, buf, (mz_ulong)n);

	mz_stream stream;
	memset(&stream, 0, sizeof(stream));
	if (mz_inflateInit(&stream) == MZ_OK) {
		stream.next_in = buf;
		stream.avail_in = (unsigned int)n;
		stream.next_out = out;
		stream.avail_out = (unsigned int)sizeof(out);
		(void)mz_inflate(&stream, MZ_FINISH);
		mz_inflateEnd(&stream);
	}
	return 0;
}
