# L1 Crypto Stack v3 — official corpus + upstream fidelity

**Report:** https://hackme.tech/reports/l1-crypto-stack-v3.html

## Three proof layers

1. **Official fuzz corpora** — `bitcoin-core/qa-assets` (`fuzz_corpora/eval_script`, `fuzz_corpora/tx`) probed through HackMe WASM ports.
2. **Pinned upstream sources** — `upstream/pins.json` + `scripts/ops/fetch_upstream_pins.sh` + `verify_upstream_fidelity.sh`.
3. **HackMe golden** — `go run ./tools/hackme_order_gate_golden` (20k inputs, WASM == Go).

## One command

```bash
bash scripts/ops/run_l1_crypto_stack_v3_research.sh
bash scripts/ops/l1_crypto_stack_v3_gate.sh
bash scripts/ops/deploy_hackme_site.sh
```

## Marketing copy

- Announce: https://hackme.tech/news.html · https://t.me/hackme_tech

## Native Core fuzz (optional next step)

Build on machine with cmake+clang fuzzer:

```bash
git clone https://github.com/bitcoin/bitcoin
cd bitcoin && cmake --preset=libfuzzer && cmake --build build_fuzz
FUZZ=eval_script build_fuzz/bin/fuzz
```

v3 report already uses **the same seed files** as that harness (qa-assets).
