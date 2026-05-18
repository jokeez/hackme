# Security Task Pack (copy/paste runbook)

This pack adds 3 synthetic security-style gates in both Rust and C++:

- `bounds_guard`
- `overflow_guard`
- `state_transition_guard`

Each gate is built as `check(i64) -> i32` and submitted as an order manifest.

## 1) Build artifacts + manifests

```bash
bash scripts/build_security_task_pack.sh
```

Outputs:

- Artifacts: `tasks/artifacts/security/*.wasm`
- Manifests: `tasks/manifests/security/*.json`

## 2) Submit all manifests

```bash
bash scripts/submit_security_task_pack.sh
```

Optional custom API base:

```bash
BASE=http://127.0.0.1:8080 bash scripts/submit_security_task_pack.sh
```

## 3) Verify progress

```bash
curl -s http://127.0.0.1:8080/api/tasks | jq '.tasks[] | select(.id | test("order-(rust|cpp)-.*-001")) | {id,status,target_solves,progress_count,progress_pct}'
```

Expected:

- orders are accepted (`ok: true`) if artifacts/hashes are valid
- status transitions `open -> completed`
- `progress_count == target_solves` on completion

## 4) Optional negative checks

- change one char in `artifact_hash` -> expect validation error
- submit duplicate `id` -> expect duplicate id error
- reduce reward below fairness minimum -> expect fairness error
