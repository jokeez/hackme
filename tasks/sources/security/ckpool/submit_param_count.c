/* Property guard: ckpool parse_submit params array minimum size (5 fields).
 * Returns 1 when params would hit SE_INVALID_SIZE or missing username/job. */
typedef unsigned long long u64;

static int too_few_params(u64 n) {
  u64 sz = n & 0xfu;
  return sz < 5u ? 1 : 0;
}

static int empty_string(u64 flag) {
  return (flag & 1u) ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 seed = (u64)n;
  int op = (int)(seed & 3u);
  switch (op) {
    case 0:
      return too_few_params(seed >> 4);
    case 1:
      return empty_string(seed >> 8); /* workername */
    case 2:
      return empty_string(seed >> 12); /* job_id */
    default:
      return too_few_params(seed >> 4) | empty_string(seed >> 16);
  }
}
