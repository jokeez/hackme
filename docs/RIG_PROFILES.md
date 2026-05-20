# Rig profiles (GPU pool tuning)

Built-in profiles tune **pool worker** env (`hackme.env`) — not BIOS clocks directly.

## API

| Method | Path | Auth |
|--------|------|------|
| GET | `/api/hardware/rig-profiles` | — |
| GET | `/api/hardware/rig-profiles/detect` | — |
| POST | `/api/hardware/rig-profiles/apply` | admin |

Apply merges profile keys into `hackme.env`, then **stop/start** pool worker from the Mining tab.

## AMD RX 580 2048SP (Yaroslav / Chinese refresh)

Profile id: `amd_rx580_2048sp`

| Env | Value | Why |
|-----|-------|-----|
| `HACKME_GPU_BACKEND` | `opencl` (Linux) / `auto` (Windows until OpenCL binary ships) | Polaris OpenCL |
| `HACKME_WORKER_BATCH_SIZE` | `1048576` | Smaller batches for 2048SP memory |
| `GPU_CHUNK` | `524288` | Shorter GPU search slices |
| `SEARCH_TIMEOUT_MS` | `4500` | Avoid timeout on slow kernels |
| `HACKME_WORKER_CLAIM_COOLDOWN_MS` | `150` | Fewer 429s on sub-1 GH/s rigs |
| `HACKME_GPU_TEMP_PAUSE_C` | `78` | Thermal hold |
| `HACKME_CUDA_CALIBRATE_GHS` | `0.12` | Submit fallback floor |

**Manual OC (vendor tools):** core 1150–1200 MHz, mem 2000–2100 MHz effective, power −5…−8%.  
After stable OC, switch to `amd_rx580_2048sp_turbo` in the dashboard.

## Verify live metrics

```bash
bash scripts/ops/verify_pool_dashboard.sh
curl -sS http://127.0.0.1:8080/api/hardware/rig-profiles/detect | jq .
```

Mining tab shows **Difficulty (M)**, **GH/s**, and rig profile status updating on poll.
