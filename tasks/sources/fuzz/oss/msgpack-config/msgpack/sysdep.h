#ifndef MSGPACK_SYSDEP_H
#define MSGPACK_SYSDEP_H
#include <stdlib.h>
#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>
#define MSGPACK_ENDIAN_LITTLE_BYTE 1
#define MSGPACK_ENDIAN_BIG_BYTE 0
#define _msgpack_be16(x) __builtin_bswap16((uint16_t)(x))
#define _msgpack_be32(x) __builtin_bswap32((uint32_t)(x))
#define _msgpack_be64(x) __builtin_bswap64((uint64_t)(x))
typedef unsigned int _msgpack_atomic_counter_t;
#define _msgpack_sync_decr_and_fetch(ptr) __sync_sub_and_fetch(ptr, 1)
#define _msgpack_sync_incr_and_fetch(ptr) __sync_add_and_fetch(ptr, 1)
#define MSGPACK_DLLEXPORT
#endif
