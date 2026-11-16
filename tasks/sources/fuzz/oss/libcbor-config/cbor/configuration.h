#ifndef LIBCBOR_CONFIGURATION_H
#define LIBCBOR_CONFIGURATION_H

#define CBOR_MAJOR_VERSION 0
#define CBOR_MINOR_VERSION 14
#define CBOR_PATCH_VERSION 0

#define CBOR_BUFFER_GROWTH 2
#define CBOR_MAX_STACK_SIZE 2048
/* Upstream master added this (PJK/libcbor); stub must define it for clang Hunt builds. */
#define CBOR_MAX_UNCHECKED_DECLARED_SIZE 16
#define CBOR_PRETTY_PRINTER 1

#define CBOR_RESTRICT_SPECIFIER restrict

#endif
