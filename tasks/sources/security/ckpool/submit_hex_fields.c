/* Property guard: ckpool stratifier.c parse_submit hex field rules.
 * Returns 1 when nonce2/ntime/nonce would fail validhex or size checks. */
typedef unsigned long long u64;

static int bad_hex(u64 seed) {
  u64 len = (seed & 0x1fu) + 1u;
  u64 odd = (seed >> 5) & 1u;
  u64 empty = (seed >> 6) & 1u;
  if (empty) return 1;
  if (odd && (len & 1u)) return 1;
  return 0;
}

static int bad_nonce(u64 seed) {
  u64 len = (seed >> 8) & 0x1fu;
  if (len < 8u) return 1;
  return bad_hex(seed >> 12);
}

static int bad_nonce2_len(u64 seed, int want) {
  int got = (int)((seed >> 16) & 0x3fu);
  if (got != want) return 1;
  return bad_hex(seed);
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 seed = (u64)n;
  int field = (int)(seed & 7u);
  int en2_hex = (int)(((seed >> 20) & 0xfu) + 1u) * 2;
  switch (field) {
    case 0:
      return bad_nonce2_len(seed >> 4, en2_hex);
    case 1:
      return bad_hex(seed >> 10); /* ntime */
    case 2:
      return bad_nonce(seed);
    case 3:
      return bad_nonce2_len(seed >> 14, 8);
    case 4:
      return ((seed >> 24) & 0xffu) == 0u ? 1 : 0;
    default:
      return bad_nonce(seed) | bad_hex(seed >> 28);
  }
}
