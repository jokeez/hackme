#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zlib.h>

int main(void) {
	static unsigned char buf[65537];
	static unsigned char out[131072];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	uLongf out_len = (uLongf)sizeof(out);
	(void)uncompress(out, &out_len, buf, (uLong)n);

	z_stream stream;
	memset(&stream, 0, sizeof(stream));
	if (inflateInit(&stream) == Z_OK) {
		stream.next_in = buf;
		stream.avail_in = (unsigned int)n;
		stream.next_out = out;
		stream.avail_out = (unsigned int)sizeof(out);
		(void)inflate(&stream, Z_FINISH);
		inflateEnd(&stream);
	}
	return 0;
}
