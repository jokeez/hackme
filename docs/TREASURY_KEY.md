# Treasury key (DevFeeAddress)

Consensus treasury address: **`HMC-719006d93916ad52`** (`internal/chain/economics.go`, field `DevFeeAddress`). It uses genesis mint (50,000 HMC) and shares of network/platform commissions for `policy_hash` in `/api/status`.

## Where is the private key

- **Not** in git. The operator stores a **32-byte Ed25519 seed** (64 hex), from which the same `HMC-…` as the node is derived: `sha256(pubkey_ed25519)` → first 16 hex → prefix `HMC-`.
- Recommended local file (directory `.secrets/` already in `.gitignore`): **`.secrets/hackme_treasury_ed25519_seed.hex`** - one line, hex only, rights `600`.

## Changing the treasury before launching the network

1. Generate a new pair (random seed):
   `go run ./tools/gen_treasury_key`  
In the output: `NEW_DEV_FEE_ADDRESS`, `NEW_TREASURY_SEED_FILE` (seed is written only to the 0600 file, not to stdout), `NEW_POLICY_HASH`.
2. Substitute the new address in `DevFeeAddress` into `internal/chain/economics.go`.
3. Update expected `policy_hash` to `internal/chain/economics_test.go` (`TestLockedPolicyHash`).
4. `go test ./internal/chain ./...` and changes to README / `docs/API.md` for the new address.
5. **New chain:** empty `data/`, new `POST /api/genesis`; all nodes are **one** build and one `policy_hash`, otherwise P2P will cut off the peer.

## Expenses from the treasury (exchange, liquidity)

Sign the usual `transfer_v1` from the treasury key (same format as the node: seed → pubkey → signature). Exchange deposit - field `to` in translation.

## Deploy to VPS (operator)

I **don't connect** to your VPS via SSH - you do. Brief order:

1. **Build** the same commit as on all nodes (locally you can already take `dist/hackme-node-linux-amd64` or rebuild on the server: `go build -trimpath -o /opt/hackme/hackme-node .`).
2. **Stop** the service, **save** the old `data/`, when changing `policy_hash` - **new** directory `data/` (or conscious reseed).
3. **Copy** the binary + `scripts/ops/systemd/hackme-node.service`, variables from `.env.vps` (see `scripts/ops/vps_bootstrap.sh`), **do not** put the treasury seed in git - only on the server in a protected file (analogous to `.secrets/hackme_treasury_ed25519_seed.hex`, `chmod 600`).
4. **Start** the node, **`POST /api/genesis`** with the admin token once - after that in `GET /api/status` → `economics.dev_fee_address` there will be **`HMC-719006d93916ad52`**, mint 50k will go to this address.
5. Windows: For desktop use `dist/hackme.exe` or full zip from `scripts/release/make_release_bundle.sh` if you need an installer.
