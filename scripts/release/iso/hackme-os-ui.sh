#!/usr/bin/env bash
# HackMe OS terminal UI — shared colors, frames, ASCII, status panels.
# Source:  source /opt/hackme/scripts/release/iso/hackme-os-ui.sh
set -euo pipefail

HACKME_UI_VERSION="${HACKME_UI_VERSION:-0.1.0-rc11g}"
HACKME_UI_NEON="${HACKME_UI_NEON:-#00ff66}"

if [[ -t 1 ]] && [[ "${NO_COLOR:-}" != "1" ]]; then
  HM_RST=$'\033[0m'
  HM_BOLD=$'\033[1m'
  HM_DIM=$'\033[2m'
  HM_NEON=$'\033[38;2;0;255;102m'
  HM_MATRIX=$'\033[38;2;57;255;20m'
  HM_WARN=$'\033[38;5;214m'
  HM_CRIT=$'\033[1;38;5;196m'
  HM_CYAN=$'\033[38;5;51m'
  HM_WHITE=$'\033[97m'
else
  HM_RST= HM_BOLD= HM_DIM= HM_NEON= HM_MATRIX= HM_WARN= HM_CRIT= HM_CYAN= HM_WHITE=
fi

hackme_ui_cols() {
  local c=80
  if command -v tput >/dev/null 2>&1; then
    c="$(tput cols 2>/dev/null || echo 80)"
  fi
  [[ "$c" -ge 40 && "$c" -le 240 ]] || c=80
  printf '%s' "$c"
}

hackme_ui_repeat() {
  local ch="$1" n="$2"
  printf '%*s' "$n" '' | tr ' ' "$ch"
}

hackme_ui_center() {
  local text="$1" width="$2"
  local len="${#text}"
  local pad=$(( (width - len) / 2 ))
  (( pad < 0 )) && pad=0
  printf '%*s%s\n' "$pad" '' "$text"
}

hackme_ui_frame_open() {
  local title="${1:-HACKME OS}"
  local width
  width="$(hackme_ui_cols)"
  local bar
  bar="$(hackme_ui_repeat '#' "$width")"
  local eq
  eq="$(hackme_ui_repeat '=' "$width")"
  printf '%s%s%s\n' "$HM_NEON" "$bar" "$HM_RST"
  printf '%s%s%s\n' "$HM_MATRIX" "$eq" "$HM_RST"
  hackme_ui_center "${HM_BOLD}${HM_WHITE}${title}${HM_RST}" "$width"
  printf '%s%s%s\n' "$HM_MATRIX" "$eq" "$HM_RST"
}

hackme_ui_frame_line() {
  local text="$1"
  local width
  width="$(hackme_ui_cols)"
  local inner=$((width - 4))
  (( inner < 20 )) && inner=20
  local trimmed="${text:0:$inner}"
  printf '║ %-*s ║\n' "$inner" "$trimmed"
}

hackme_ui_frame_close() {
  local width
  width="$(hackme_ui_cols)"
  printf '%s%s%s\n' "$HM_NEON" "$(hackme_ui_repeat '#' "$width")" "$HM_RST"
}

hackme_ui_ascii_logo() {
  local ver="${1:-$HACKME_UI_VERSION}"
  cat <<LOGO
${HM_NEON}██╗  ██╗ █████╗  ██████╗██╗  ██╗███╗   ███╗███████╗     ██████╗ ███████╗${HM_RST}
${HM_NEON}██║  ██║██╔══██╗██╔════╝██║ ██╔╝████╗ ████║██╔════╝    ██╔═══██╗██╔════╝${HM_RST}
${HM_NEON}███████║███████║██║     █████╔╝ ██╔████╔██║█████╗      ██║   ██║███████╗${HM_RST}
${HM_NEON}██╔══██║██╔══██║██║     ██╔═██╗ ██║╚██╔╝██║██╔══╝      ██║   ██║╚════██║${HM_RST}
${HM_NEON}██║  ██║██║  ██║╚██████╗██║  ██╗██║ ╚═╝ ██║███████╗    ╚██████╔╝███████║${HM_RST}
${HM_NEON}╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝╚═╝     ╚═╝╚══════╝     ╚═════╝ ╚══════╝${HM_RST}
${HM_MATRIX}                    v${ver} · ZERO-KNOWLEDGE START · ${HM_NEON}hackme.tech${HM_RST}
LOGO
}

hackme_ui_zk_tty_banner() {
  local wallet="$1"
  local phrase="$2"
  local pool="${3:-https://hackme.tech/pool/coordinator}"
  local ver="${4:-$HACKME_UI_VERSION}"

  hackme_ui_frame_open "HACKME OS — ZERO-KNOWLEDGE START"
  hackme_ui_ascii_logo "$ver"
  echo ""
  hackme_ui_frame_line "${HM_CRIT}>> [CRITICAL SECURITY WARNING] <<${HM_RST}"
  hackme_ui_frame_line "Write recovery phrase on PAPER. Never share photos online."
  hackme_ui_frame_line "Anyone with phrase OWNS your HMC payout address."
  echo ""
  hackme_ui_frame_line "${HM_BOLD}${HM_CYAN}>> PAYOUT WALLET (HMC)${HM_RST}"
  hackme_ui_frame_line "  ${HM_NEON}${wallet}${HM_RST}"
  echo ""
  hackme_ui_frame_line "${HM_BOLD}${HM_WARN}>> RECOVERY PHRASE (24 words)${HM_RST}"
  if [[ -n "$phrase" ]]; then
    local n=0 w
    for w in $phrase; do
      n=$((n + 1))
      hackme_ui_frame_line "$(printf '  >> %2d) %s' "$n" "$w")"
    done
  else
    hackme_ui_frame_line "  (unavailable — check /var/lib/hackme/recovery.phrase)"
  fi
  echo ""
  hackme_ui_frame_line "${HM_DIM}>> POOL COORDINATOR${HM_RST}"
  hackme_ui_frame_line "  ${pool}"
  hackme_ui_frame_line "${HM_DIM}>> COMMANDS${HM_RST}"
  hackme_ui_frame_line "  hackme-os-status  ·  hackme-show-wallet  ·  journalctl -u hackme-miner-worker -f"
  hackme_ui_frame_close
  echo ""
}

hackme_ui_box_header() {
  local title="$1"
  echo "${HM_NEON}╔══════════════════════════════════════════════════════════════════════════╗${HM_RST}"
  printf "${HM_NEON}║${HM_RST} %-72s ${HM_NEON}║${HM_RST}\n" "$title"
  echo "${HM_NEON}╠══════════════════════════════════════════════════════════════════════════╣${HM_RST}"
}

hackme_ui_box_row() {
  local label="$1"
  local value="$2"
  printf "${HM_NEON}║${HM_RST} %-20s ${HM_MATRIX}%s${HM_RST}\n" "$label:" "$value"
}

hackme_ui_box_footer() {
  echo "${HM_NEON}╚══════════════════════════════════════════════════════════════════════════╝${HM_RST}"
}

hackme_ui_gpu_metrics() {
  # Prints lines: temp|fan|name — best-effort RX 580 / NVIDIA
  local temp="—" fan="—" name="—" util="—"
  if command -v nvidia-smi >/dev/null 2>&1; then
    read -r name util temp fan < <(nvidia-smi --query-gpu=name,utilization.gpu,temperature.gpu,fan.speed \
      --format=csv,noheader,nounits 2>/dev/null | head -1 | tr ',' ' ')
    temp="${temp:-—}°C"
    fan="${fan:-—}%"
    util="${util:-—}%"
  elif command -v sensors >/dev/null 2>&1; then
    local st
    st="$(sensors 2>/dev/null | grep -iE 'edge:|junction:|temp1:' | head -1 | awk '{print $2}')"
    [[ -n "$st" ]] && temp="$st"
    name="AMD GPU (amdgpu)"
    for pwm in /sys/class/drm/card*/device/hwmon/hwmon*/pwm1; do
      [[ -f "$pwm" ]] || continue
      local en max cur
      en="$(cat "${pwm/%pwm1/pwm1_enable}" 2>/dev/null || echo 0)"
      max="$(cat "${pwm/%pwm1/pwm1_max}" 2>/dev/null || echo 255)"
      cur="$(cat "$pwm" 2>/dev/null || echo 0)"
      if [[ "$max" -gt 0 ]]; then
        fan="$(( cur * 100 / max ))%"
      fi
      break
    done
    if [[ -f /sys/class/drm/card0/device/product_name ]]; then
      name="$(cat /sys/class/drm/card0/device/product_name 2>/dev/null | tr -d '\n')"
    fi
  fi
  printf '%s|%s|%s|%s\n' "$temp" "$fan" "$name" "$util"
}

hackme_ui_pool_worker_stats() {
  local coord_url="$1"
  local worker_id="$2"
  local gh="—" att="—" pay="—" online="offline"
  if command -v curl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
    local js
    if js="$(curl -fsS --max-time 10 "${coord_url}/api/work/stats" 2>/dev/null)"; then
      online="connected"
      gh="$(printf '%s' "$js" | jq -r --arg w "$worker_id" '.workers[$w].hashrate_gh_s // 0' 2>/dev/null)"
      att="$(printf '%s' "$js" | jq -r --arg w "$worker_id" '.workers[$w].accepted_attempts // 0' 2>/dev/null)"
      pay="$(printf '%s' "$js" | jq -r --arg w "$worker_id" '.workers[$w].payout_hmc // 0' 2>/dev/null)"
      local pool_gh
      pool_gh="$(printf '%s' "$js" | jq -r '.pool_hashrate_gh_s // 0' 2>/dev/null)"
      printf '%s|%s|%s|%s|%s\n' "$online" "$gh" "$att" "$pay" "$pool_gh"
      return 0
    fi
  fi
  printf '%s|%s|%s|%s|%s\n' "$online" "$gh" "$att" "$pay" "—"
}

hackme_ui_status_dashboard() {
  local coord_url="${1:-https://hackme.tech/pool/coordinator}"
  local worker_id="${2:-unknown}"
  local pool_label="${3:-hackme.tech (FirstVDS pool)}"

  hackme_ui_box_header "HACKME OS — RIG STATUS"
  hackme_ui_box_row "Time (UTC)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  hackme_ui_box_row "Worker ID" "$worker_id"
  hackme_ui_box_row "Pool" "$pool_label"
  hackme_ui_box_row "Coordinator" "$coord_url"

  IFS='|' read -r temp fan gname gutil < <(hackme_ui_gpu_metrics)
  hackme_ui_box_row "GPU" "${gname}"
  hackme_ui_box_row "GPU temp" "${temp}"
  hackme_ui_box_row "Fan speed" "${fan}"
  hackme_ui_box_row "GPU util" "${gutil}"

  IFS='|' read -r conn gh att pay pool_gh < <(hackme_ui_pool_worker_stats "$coord_url" "$worker_id")
  hackme_ui_box_row "Pool link" "$conn"
  hackme_ui_box_row "Your GH/s" "${gh}"
  hackme_ui_box_row "Pool GH/s" "${pool_gh}"
  hackme_ui_box_row "Attempts" "${att}"
  hackme_ui_box_row "Payout HMC" "${pay}"

  local svc
  svc="$(systemctl is-active hackme-miner-worker.service 2>/dev/null || echo unknown)"
  hackme_ui_box_row "Worker svc" "$svc"
  hackme_ui_box_footer
}

hackme_ui_wallet_dashboard() {
  local wallet="${1:-}"
  local phrase="${2:-}"

  hackme_ui_box_header "HACKME OS — WALLET VAULT"
  if [[ -n "$wallet" ]]; then
    hackme_ui_box_row "Payout HMC" "$wallet"
  else
    hackme_ui_box_row "Payout HMC" "(not set)"
  fi
  if [[ -n "$phrase" ]]; then
    echo "${HM_NEON}╠══════════════════════════════════════════════════════════════════════════╣${HM_RST}"
    echo "${HM_CRIT}║  >> [CRITICAL] Recovery phrase — paper only, never cloud photos       ║${HM_RST}"
    local n=0
    for w in $phrase; do
      n=$((n + 1))
      printf "${HM_NEON}║${HM_RST}  ${HM_WARN}%2d) %-66s${HM_RST}\n" "$n" "$w"
    done
  else
    hackme_ui_box_row "Recovery" "(not stored on this boot)"
  fi
  hackme_ui_box_footer
}
