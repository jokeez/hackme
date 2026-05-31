/*
 * Bitcoin Core — consensus/tx_check.cpp CheckTransaction output loop + amount.h MoneyRange.
 * check(n): 1 on bad-txns-vout-negative / toolarge / txouttotal-toolarge.
 * Encodes two output values (satoshis) in n: low32 = vout0, high32 = vout1.
 */
typedef long long CAmount;

static const CAmount COIN = 100000000LL;
static const CAmount MAX_MONEY = 21000000LL * COIN;

static int MoneyRange(CAmount nValue) { return nValue >= 0 && nValue <= MAX_MONEY; }

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    CAmount v0 = (CAmount)(u & 0xffffffffu);
    CAmount v1 = (CAmount)((u >> 32) & 0xffffffffu);

    if (v0 < 0) {
        return 1;
    }
    if (v0 > MAX_MONEY) {
        return 1;
    }
    CAmount nValueOut = v0;
    if (!MoneyRange(nValueOut)) {
        return 1;
    }

    if (v1 < 0) {
        return 1;
    }
    if (v1 > MAX_MONEY) {
        return 1;
    }
    nValueOut += v1;
    if (!MoneyRange(nValueOut)) {
        return 1;
    }
  return 0;
}
