#!/usr/bin/env bash

set -euo pipefail

pid_file=""
log_file=""

usage() {
  echo "usage: $0 --pid-file PATH --log-file PATH -- command [args...]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pid-file)
      pid_file="$2"
      shift 2
      ;;
    --log-file)
      log_file="$2"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [[ -z "${pid_file}" || -z "${log_file}" || $# -eq 0 ]]; then
  usage
  exit 2
fi

mkdir -p "$(dirname "${pid_file}")"
mkdir -p "$(dirname "${log_file}")"
rm -f "${pid_file}"

if command -v setsid >/dev/null 2>&1; then
  nohup setsid "$@" >"${log_file}" 2>&1 &
else
  nohup "$@" >"${log_file}" 2>&1 &
fi

pid=$!
printf '%s\n' "${pid}" >"${pid_file}"
printf 'started pid=%s cmd=%s\n' "${pid}" "$1"
