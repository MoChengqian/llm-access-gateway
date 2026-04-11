#!/usr/bin/env bash

set -euo pipefail

pid_file=""
label=""
health_url=""
log_file=""
wait_seconds=30

usage() {
  echo "usage: $0 --pid-file PATH --label NAME [--health-url URL] [--log-file PATH] [--wait-seconds N]" >&2
  return 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pid-file)
      pid_file="$2"
      shift 2
      ;;
    --label)
      label="$2"
      shift 2
      ;;
    --health-url)
      health_url="$2"
      shift 2
      ;;
    --log-file)
      log_file="$2"
      shift 2
      ;;
    --wait-seconds)
      wait_seconds="$2"
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [[ -z "${pid_file}" || -z "${label}" ]]; then
  usage
  exit 2
fi

if [[ ! -f "${pid_file}" ]]; then
  exit 0
fi

pid="$(tr -d '[:space:]' <"${pid_file}")"
rm -f "${pid_file}"

if [[ -z "${pid}" ]]; then
  exit 0
fi

pgid="$(ps -o pgid= -p "${pid}" 2>/dev/null | tr -d '[:space:]' || true)"

stop_target() {
  local signal_name="$1"
  if [[ -n "${pgid}" ]]; then
    kill -"${signal_name}" -- "-${pgid}" 2>/dev/null || true
  fi
  kill -"${signal_name}" "${pid}" 2>/dev/null || true
  return 0
}

is_alive() {
  kill -0 "${pid}" 2>/dev/null
  return $?
}

stop_target TERM
for _ in $(seq 1 "${wait_seconds}"); do
  if ! is_alive; then
    break
  fi
  sleep 1
done

if is_alive; then
  stop_target KILL
  for _ in $(seq 1 5); do
    if ! is_alive; then
      break
    fi
    sleep 1
  done
fi

if is_alive; then
  echo "${label} process still alive after forced shutdown (pid=${pid})" >&2
  if [[ -n "${log_file}" && -f "${log_file}" ]]; then
    cat "${log_file}" >&2 || true
  fi
  exit 1
fi

exit 0

if [[ -n "${health_url}" ]]; then
  for _ in $(seq 1 "${wait_seconds}"); do
    if ! curl -fsS "${health_url}" >/dev/null 2>&1; then
      exit 0
    fi
    sleep 1
  done

  echo "${label} still serving ${health_url} after shutdown" >&2
  if [[ -n "${log_file}" && -f "${log_file}" ]]; then
    cat "${log_file}" >&2 || true
  fi
  exit 1
fi
