/* Property guard: ckpool connector.c parse_client_msg — MAX_MSGSIZE 1024.
 * Returns 1 on oversize line or slowloris growth without newline. */
typedef unsigned long long u64;

#define MAX_MSGSIZE 1024u

static int oversize(u64 claimed) {
  return claimed > (u64)MAX_MSGSIZE ? 1 : 0;
}

static int partial_no_nl(u64 buf_sz, u64 has_nl) {
  if (buf_sz == 0) return 0;
  if (has_nl & 1u) return 0;
  return buf_sz > (u64)(MAX_MSGSIZE - 32u) ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 buf_sz = (u64)(n & 0x3ffu);
  u64 has_nl = (u64)((n >> 10) & 1u);
  u64 inflate = (u64)((n >> 12) & 0xffu);
  u64 total = buf_sz + inflate * 64u;
  if (oversize(total)) return 1;
  return partial_no_nl(total, has_nl);
}
