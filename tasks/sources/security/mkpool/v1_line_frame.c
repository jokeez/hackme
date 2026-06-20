/* Property guard: Stratum V1 line framing (client_session.cpp on_read).
 * Returns 1 on oversize buffer (>1 MiB) or line without newline boundary. */
typedef unsigned long long u64;

#define V1_MAX_BUF (1u << 20)

static int oversize(u64 claimed) {
  return claimed > (u64)V1_MAX_BUF ? 1 : 0;
}

static int partial_line_no_nl(u64 buf_sz, u64 has_nl) {
  if (buf_sz == 0) return 0;
  if (has_nl & 1u) return 0;
  /* partial frame: grows until cap */
  return buf_sz > (u64)(V1_MAX_BUF - 1024u) ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 buf_sz = (u64)(n & 0xfffffu);
  u64 has_nl = (u64)((n >> 20) & 1u);
  u64 inflate = (u64)((n >> 24) & 0xffu);
  u64 total = buf_sz + inflate * 8192u;
  if (oversize(total)) return 1;
  return partial_line_no_nl(total, has_nl);
}
