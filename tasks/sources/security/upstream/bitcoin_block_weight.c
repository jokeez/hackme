/*
 * Bitcoin Core — validation weight budget (policy.h MAX_BLOCK_WEIGHT).
 * check(n): 1 if computed block weight exceeds 4,000,000 WU.
 * Encodes stripped_size (low32) and witness_bytes (high32) from fuzz input n.
 * Weight = stripped * (WITNESS_SCALE_FACTOR - 1) + total_size
 *   where total_size = stripped + witness (segwit serialization model).
 */
#define WITNESS_SCALE_FACTOR 4
#define MAX_BLOCK_WEIGHT 4000000

static unsigned weight_from_parts(unsigned stripped, unsigned witness) {
    unsigned total = stripped + witness;
    return stripped * (WITNESS_SCALE_FACTOR - 1) + total;
}

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    unsigned stripped = (unsigned)(u & 0xffffffffu);
    unsigned witness = (unsigned)((u >> 32) & 0xffffffffu);
    unsigned w = weight_from_parts(stripped, witness);
    if (w > MAX_BLOCK_WEIGHT) {
        return 1;
    }
    return 0;
}
