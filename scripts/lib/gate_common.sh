#!/usr/bin/env bash

# Shared helpers for gate scripts.
# shellcheck shell=bash

gate_require_cmd() {
  local prefix="${1:?prefix required}"
  local cmd="${2:?command required}"
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "[${prefix}] missing command: ${cmd}" >&2
    exit 1
  }
}

gate_init_results_file() {
  local file_path="${1:?results file path required}"
  : >"$file_path"
}

gate_record_result_jsonl() {
  local results_file="${1:?results file required}"
  local id="${2:?id required}"
  local verdict="${3:?verdict required}"
  local detail="${4:-}"
  local log_path="${5:-}"
  local required="${6:-}"
  jq -nc \
    --arg id "$id" \
    --arg verdict "$verdict" \
    --arg detail "$detail" \
    --arg log "$log_path" \
    --arg required "$required" \
    '{
      id:$id,
      verdict:$verdict,
      detail:$detail,
      log:$log
    } + (if $required == "" then {} else {required: ($required=="1")} end)' >>"$results_file"
}

gate_run_case() {
  local prefix="${1:?prefix required}"
  local results_file="${2:?results file required}"
  local out_dir="${3:?out dir required}"
  local id="${4:?id required}"
  local detail="${5:-}"
  local required="${6:-}"
  shift 6
  local log_path="$out_dir/${id}.log"
  if "$@" >"$log_path" 2>&1; then
    echo "[${prefix}] PASS ${id}"
    gate_record_result_jsonl "$results_file" "$id" "pass" "$detail" "$log_path" "$required"
  else
    echo "[${prefix}] FAIL ${id} (see ${log_path})"
    gate_record_result_jsonl "$results_file" "$id" "fail" "$detail" "$log_path" "$required"
  fi
}
