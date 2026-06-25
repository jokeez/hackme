/* HackMe OSS CVE fuzz — shadow system expat_config.h so we link getrandom, not arc4random_buf. */
#ifndef EXPAT_CONFIG_H
#define EXPAT_CONFIG_H 1

#define BYTEORDER 1234
#define HAVE_GETRANDOM 1
#define HAVE_SYSCALL_GETRANDOM 1
#define XML_CONTEXT_BYTES 1024
#define XML_DTD 1
#define XML_GE 1
#define XML_NS 1

#endif
