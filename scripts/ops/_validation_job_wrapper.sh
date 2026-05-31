#!/usr/bin/env bash
# Internal: run one validation-suite job, write log + exit code file.
set +e
jlog="$1"
jexit="$2"
shift 2
"$@" >"$jlog" 2>&1
ec=$?
echo "$ec" >"$jexit"
exit "$ec"
