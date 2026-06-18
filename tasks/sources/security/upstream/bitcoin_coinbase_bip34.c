/*
 * Bitcoin Core — BIP34 coinbase height push (validation.cpp ConnectBlock).
 * check(n): 1 if block_height >= 1 and coinbase script does not start with
 *           push of block height (simplified 4-byte height from low32 of n,
 *           coinbase prefix from high32 bits as 4-byte script fragment).
 */
#define BIP34_HEIGHT 1

static int coinbase_has_height_push(unsigned height, unsigned prefix) {
    /* Expect push opcode 1-4 bytes then height LE in coinbase start */
    unsigned char h0 = (unsigned char)(height & 0xff);
    unsigned char h1 = (unsigned char)((height >> 8) & 0xff);
    unsigned char h2 = (unsigned char)((height >> 16) & 0xff);
    unsigned char h3 = (unsigned char)((height >> 24) & 0xff);
    unsigned char p0 = (unsigned char)(prefix & 0xff);
    unsigned char p1 = (unsigned char)((prefix >> 8) & 0xff);
    unsigned char p2 = (unsigned char)((prefix >> 16) & 0xff);
    unsigned char p3 = (unsigned char)((prefix >> 24) & 0xff);
    if (height < BIP34_HEIGHT) {
        return 1;
    }
    if (p0 >= 1 && p0 <= 4) {
        if (p0 == 1 && p1 == h0) return 0;
        if (p0 == 2 && p1 == h0 && p2 == h1) return 0;
        if (p0 == 3 && p1 == h0 && p2 == h1 && p3 == h2) return 0;
    }
    return 1;
}

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    unsigned height = (unsigned)(u & 0xffffffffu);
    unsigned prefix = (unsigned)((u >> 32) & 0xffffffffu);
    return coinbase_has_height_push(height, prefix);
}
