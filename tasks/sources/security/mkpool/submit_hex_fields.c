/* Property guard: mining.submit hex field rules (client_session.cpp handle_submit).
 * Returns 1 when ntime/nonce2/nonce would be rejected as bad-hex or bad-size. */
typedef unsigned long long u64;

static int valid_hex_len(u64 seed, int want_len) {
  int len = (int)(seed % 32u) + 1;
  int odd = (int)((seed >> 6) & 1u);
  if (len != want_len) return 1;
  if (odd && (len & 1)) return 1;
  return 0;
}

static int bad_nonce_size(u64 seed) {
  int len = (int)(seed % 16u);
  return len != 8 ? 1 : 0;
}

static int bad_en2_size(u64 seed, int en2_hex) {
  int got = (int)(seed % 20u);
  return got != en2_hex ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 seed = (u64)n;
  int field = (int)(seed & 7u);
  int en2_hex = (int)(((seed >> 12) & 0xfu) + 2u) * 2; /* 4..32 even */
  switch (field) {
    case 0:
      return valid_hex_len(seed >> 4, 8); /* ntime */
    case 1:
      return bad_nonce_size(seed >> 5);
    case 2:
      return bad_en2_size(seed >> 9, en2_hex);
    case 3:
      /* Zcash solution size: expect 2688 hex chars */
      {
        int sz = (int)((seed >> 16) % 4000u);
        return sz != 2688 ? 1 : 0;
      }
    case 4:
      /* empty hex invalid */
      return ((seed >> 20) & 0xfu) == 0u ? 1 : 0;
    default:
      return valid_hex_len(seed >> 24, 8) | bad_nonce_size(seed >> 28);
  }
}
