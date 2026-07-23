# Security and Boundaries HackMe (MVP)

## Threat model (local node)

- The dashboard and API listen to **127.0.0.1** by default - they are not accessible from the network without explicit port forwarding. **`POST /api/tasks`** does not have separate authentication: in production, TLS, roles and limits are needed before publishing to the open network.
- Base **`data/hackme.db`** contains copies of blocks; file access = full control over the "balance" on this machine (MVP without cryptographic file protection).

## WASM sandbox

- Optional **CUDA** (`-tags cuda`) and **OpenCL** (`-tags opencl`) paths compile and execute **trusted** kernels from the repository (NVRTC / OpenCL C); trust only your assembly. Multiple GPUs - separate contexts, common atomic search `nonce`. The GPU candidate still passes **WASM `eval`** on the CPU before writing the block.
- Performed in **wazero** with a separate runtime; calls are serialized **mutex** (no parallel `eval` from different goroutines on the same instance).
- For each call `eval` a **timeout** is set (`internal/sandbox`, `WasmEvalTimeout`). Further optional: memory/instruction limit (wazero API), policy for third-party modules.
- Any future tasks from “customers” should only come as **agreed** code/bytecode (NDA, testing permission).

## Legal (reminder)

- Do not use the network for unauthorized access to third party systems.
- Token/"coin" in MVP - **local simulation**; Before a public launch, an analysis of the jurisdiction (securities, advertising of profitability, etc.) is required.

## Recommended production node settings (future)

- TLS for API, admin authentication, secrets outside the repository.
- Backup `data/hackme.db`.
- Dependency audit (`govulncheck`).
