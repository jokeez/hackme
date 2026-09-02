#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "microtar.h"

typedef struct {
	const unsigned char *data;
	unsigned size;
} mem_stream_t;

static int mem_read(mtar_t *tar, void *data, unsigned size) {
	mem_stream_t *ms = (mem_stream_t *)tar->stream;
	if (tar->pos + size > ms->size) {
		return MTAR_EREADFAIL;
	}
	memcpy(data, ms->data + tar->pos, size);
	tar->pos += size;
	return MTAR_ESUCCESS;
}

static int mem_seek(mtar_t *tar, unsigned offset) {
	mem_stream_t *ms = (mem_stream_t *)tar->stream;
	if (offset > ms->size) {
		return MTAR_ESEEKFAIL;
	}
	tar->pos = offset;
	return MTAR_ESUCCESS;
}

static int mem_close(mtar_t *tar) {
	(void)tar;
	return MTAR_ESUCCESS;
}

static int mem_write(mtar_t *tar, const void *data, unsigned size) {
	(void)tar;
	(void)data;
	(void)size;
	return MTAR_EWRITEFAIL;
}

int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	mem_stream_t ms = { buf, (unsigned)n };
	mtar_t tar;
	memset(&tar, 0, sizeof(tar));
	tar.read = mem_read;
	tar.write = mem_write;
	tar.seek = mem_seek;
	tar.close = mem_close;
	tar.stream = &ms;

	mtar_header_t h;
	for (unsigned steps = 0; steps < 10000; steps++) {
		int err = mtar_read_header(&tar, &h);
		if (err != MTAR_ESUCCESS) {
			break;
		}
		err = mtar_next(&tar);
		if (err != MTAR_ESUCCESS) {
			break;
		}
	}
	return 0;
}
