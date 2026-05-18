// PoH lock kernel: eval(n)=n*7+13; record smallest n with eval % mod == 0.
// Mirror of kernels/poh_search.cu — compiled at runtime via NVRTC when building with -tags cuda.

extern "C" __global__ void poh_search(
    unsigned long long base,
    unsigned long long count,
    unsigned long long mod,
    unsigned long long* out_nonce)
{
    size_t tid = (size_t)blockIdx.x * (size_t)blockDim.x + (size_t)threadIdx.x;
    if (tid >= (size_t)count) {
        return;
    }
    unsigned long long n = base + (unsigned long long)tid;
    unsigned long long v = n * 7ULL + 13ULL;
    if (mod == 0ULL) {
        return;
    }
    if ((v % mod) != 0ULL) {
        return;
    }
    atomicMin(out_nonce, n);
}
