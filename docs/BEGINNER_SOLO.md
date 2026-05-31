# Beginner solo mode — removed

**`HACKME_BEGINNER_SOLO` is no longer supported.** The binary exits with a fatal error if it is set.

Use **pool / worker mining** instead:

- Set **`HACKME_POOL_COORDINATOR_URL`** (and coordinator token as required by your deployment).
- Start the external worker loop or use **`POST /api/worker/start`** from the dashboard (Mining tab).

**Local WASM PoH** via **`POST /api/mining/start`** exists only on processes explicitly marked as the chain command node with **`HACKME_CHAIN_LEADER_LOCAL_POH=1`**. Typical desktop / pool participants do **not** set this; they mine through the worker.

Windows quick start: **`scripts/release/windows/start_hackme_dashboard.bat`**.

See **`README.md`** (worker-mode section) and **`docs/API.md`** (`/api/worker/start`, `/api/mining/start`).

---

*The text below described the old behaviour and is kept only as historical reference.*

<details>
<summary>Historical (obsolete)</summary>

Previously, `HACKME_BEGINNER_SOLO=1` with loopback bind could auto-create genesis and start local PoH without using the worker. That path was removed in favour of coordinator-backed mining.

</details>
