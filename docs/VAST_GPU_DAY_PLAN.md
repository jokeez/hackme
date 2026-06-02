# GPU matrix day — Vast.ai (real-user simulation)

Goal: many **different GPU models** today; each run behaves like a **new remote miner** — pool + **UI on hackme.tech** + metrics.

Pack: `dist/vast-gpu-matrix-*.tar.gz` · scripts in `scripts/vast/`

---

## Phase order (today)

| Phase | GPUs | Instance | Duration | WORKER_ID pattern |
|-------|------|----------|----------|-------------------|
| **A** | 2× RTX 5090 (or best available) | 1 instance, 2 GPUs OR 2×1 GPU | 45–60 min each | `vast-5090-a`, `vast-5090-b` |
| **B** | RTX 40xx | 4080/4090/4060 | 30–45 min | `vast-4090-01`, … |
| **C** | RTX 30xx | 3090/3080 | 30 min | `vast-3090-01`, … |
| **D** | RTX 20xx / T4 | 2080 Ti, T4 | 20–30 min | `vast-t4-01`, … |
| **E** | **6 GPU fleet** (one box) | 1× 6-GPU offer | 45–60 min | `vast-fleet-6g` (autostart spawns `-gpu0` …) |

Skip models that cost too much on $10 budget — log `SKIP` in sheet.

---

## Per-run checklist (every GPU)

### On Vast (SSH)

```bash
cd vast-gpu-matrix-*/
sed -i 's/^WORKER_ID=.*/WORKER_ID=vast-RTX4090-01/' env.vast   # unique
export RUN_SECONDS=2700   # 45 min, or 3600 for 5090 pair test

bash scripts/00_inventory.sh
bash scripts/01_run_pool_worker.sh
bash scripts/03_ui_snapshot.sh      # coordinator JSON for this worker
bash scripts/02_collect_report.sh
```

**6-GPU fleet** (one instance):

```bash
sed -i 's/^WORKER_ID=.*/WORKER_ID=vast-fleet-6g/' env.vast
bash scripts/00_inventory.sh
bash scripts/01_run_fleet.sh
bash scripts/03_ui_snapshot.sh
bash scripts/02_collect_report.sh
```

### On your PC (UI — mandatory)

**Два экрана проверяем каждый прогон:**

1. **Локальный дашборд** (node на `:8080` должен быть запущен):  
   - [http://127.0.0.1:8080/#mining](http://127.0.0.1:8080/#mining) — воркеры, GH/s, fleet (**если GPU >1 — все слоты / строки**)  
   - [http://127.0.0.1:8080/#hardware](http://127.0.0.1:8080/#hardware) — GPU cards, tune hints (локальная машина; на Vast — только API probe)  
   - [http://127.0.0.1:8080/#chain](http://127.0.0.1:8080/#chain) — высота, блоки, сложность (`pool_target_mod`, canonical tip)

2. **Prod** (удалённые Vast-воркеры всегда здесь):  
   - [https://hackme.tech/dashboard.html#mining](https://hackme.tech/dashboard.html#mining)  
   - [MPS HMC](https://miningpoolstats.app/coins/HMC)

Авто-probe локальных API (без скриншота):

```bash
bash scripts/vast/local_dashboard_probe.sh vast-5090-01
```

Within **5–10 min** after worker start, confirm:

| UI element | Pass if |
|------------|---------|
| **#mining → Active workers** | Row(s) with `WORKER_ID`; **2+ GPU fleet** → несколько строк (`-gpu0`, `-gpu1`, …) |
| **#mining pool GH / difficulty** | `target_mod`, pool GH/s не «—» |
| **Hive → Fleet** | Все активные worker id видны |
| **#hardware** (local) | Карточки GPU на **твоём PC**; на Vast — см. inventory log |
| **#chain** | tip растёт, блоки/lead miner по мере нахождений |
| **Coordinator vs UI sync** | `03_ui_snapshot` + `local_dashboard_probe` — worker found |
| **MPS** | workers + pool GH/s (задержка 1–5 min) |

Screenshot each model → `reports/vast-ui-screenshots/` (gitignored).

### Sync / adaptivity (what we validate)

| Metric | Where |
|--------|--------|
| **Backend** | `00_inventory` → `cuda` on NVIDIA |
| **Calibration GH/s** | worker.log lines with GH/s / calibrat |
| **Coordinator sync** | `03_ui_snapshot` → worker in `/api/work/stats` |
| **UI sync** | dashboard row GH/s ≈ same order as log (not exact, same ballpark) |
| **Submits** | log: submit ok / found / accepted |
| **gputune** | optional: power hints in log if telemetry works |

**Adaptivity** = worker starts without manual `HACKME_CUDA_CALIBRATE_GHS`; batch 4M; no crash 15+ min.

---

## Budget (~$10)

- 5090 ~$1/h × 2 GPUs × 1h ≈ $2  
- 4–5 cheaper runs 20–30 min ≈ $3–4  
- 6-GPU instance 45 min — check price before rent  
- **Destroy** instance after each run

---

## Log matrix

Edit `GPU_MATRIX_SHEET.csv` in pack (or copy to PC):

`instance_id, gpu_name, driver, compute_cap, vram_gb, worker_id, backend, ghs_log_peak, ghs_ui, pool_stats_ok, ui_workers_row, ui_pass, notes`

---

## When a run fails

| Symptom | Action |
|---------|--------|
| CUDA init fail | Other Vast template (CUDA 12+, driver 550+) |
| 401 claim | Check `COORD_TOKEN` in env.vast |
| GH/s in log but UI 0 | Wait 2 min, Refresh; check prod URL not localhost |
| Worker in UI, log empty | Network to hackme.tech; extend RUN_SECONDS |
| 6 GPU only 1 worker | Use `01_run_fleet.sh`, not single `01_run_pool_worker.sh` |

---

## After today

- Tarball all `reports/vast-session-*` from each instance  
- Short summary: which architectures PASS (50/40/30/20)  
- Fix regressions before public miner push  

Rebuild pack: `bash scripts/ops/pack_vast_gpu_matrix.sh --include-token`
