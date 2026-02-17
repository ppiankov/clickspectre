#!/usr/bin/env bash
set -euo pipefail

readonly REPO_OWNER="ppiankov"
readonly REPO_NAME="clickspectre"

log() {
  echo "[clickspectre-action] $*"
}

fail() {
  echo "[clickspectre-action] ERROR: $*" >&2
  exit 1
}

normalize() {
  echo "$1" | tr '[:upper:]' '[:lower:]' | tr '_' '-' | xargs
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

resolve_platform() {
  local uname_s uname_m os arch ext
  uname_s="$(uname -s | tr '[:upper:]' '[:lower:]')"
  uname_m="$(uname -m | tr '[:upper:]' '[:lower:]')"

  case "$uname_s" in
    linux*) os="linux" ;;
    darwin*) os="darwin" ;;
    mingw*|msys*|cygwin*) os="windows" ;;
    *) fail "unsupported OS: $uname_s" ;;
  esac

  case "$uname_m" in
    x86_64|amd64) arch="x86_64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) fail "unsupported architecture: $uname_m" ;;
  esac

  ext="tar.gz"
  if [[ "$os" == "windows" ]]; then
    ext="zip"
  fi

  echo "$os,$arch,$ext"
}

resolve_version() {
  local explicit action_ref resolved
  explicit="$(normalize "${INPUT_VERSION:-}")"
  action_ref="${GITHUB_ACTION_REF:-}"

  if [[ -n "$explicit" && "$explicit" != "latest" ]]; then
    if [[ "$explicit" =~ ^v[0-9]+$ ]]; then
      echo "latest"
      return
    fi
    echo "$explicit"
    return
  fi

  if [[ "$action_ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "$action_ref"
    return
  fi

  resolved="latest"
  echo "$resolved"
}

download_binary() {
  local version="$1"
  local os="$2"
  local arch="$3"
  local ext="$4"
  local work_dir="$5"

  require_command curl

  local asset_name download_url archive_path
  asset_name="clickspectre-${os}-${arch}.${ext}"
  archive_path="${work_dir}/${asset_name}"

  if [[ "$version" == "latest" ]]; then
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download/${asset_name}"
  else
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${asset_name}"
  fi

  log "Downloading ${download_url}"
  curl -fsSL "$download_url" -o "$archive_path"

  case "$ext" in
    tar.gz)
      tar -xzf "$archive_path" -C "$work_dir"
      ;;
    zip)
      require_command unzip
      unzip -q "$archive_path" -d "$work_dir"
      ;;
    *)
      fail "unsupported archive extension: $ext"
      ;;
  esac

  local binary_path
  binary_path="$(find "$work_dir" -type f \( -name 'clickspectre' -o -name 'clickspectre.exe' \) | head -n1)"
  [[ -n "$binary_path" ]] || fail "clickspectre binary not found in extracted archive"
  chmod +x "$binary_path" || true
  echo "$binary_path"
}

count_findings() {
  local format="$1"
  local report_path="$2"

  require_command python3
  python3 - "$format" "$report_path" <<'PY'
import json
import sys

fmt = sys.argv[1]
path = sys.argv[2]

safe = 0
likely = 0
anomaly = 0

with open(path, encoding="utf-8") as f:
    payload = json.load(f)

if fmt == "json":
    rec = payload.get("cleanup_recommendations", {})
    safe = len(rec.get("safe_to_drop", []))
    safe += len(rec.get("zero_usage_non_replicated", []))
    safe += len(rec.get("zero_usage_replicated", []))
    likely = len(rec.get("likely_safe", []))
    anomaly = len(payload.get("anomalies", []))
elif fmt == "sarif":
    runs = payload.get("runs", [])
    results = runs[0].get("results", []) if runs else []
    for result in results:
        rule = str(result.get("ruleId", "")).upper()
        if rule.endswith("/ZERO_USAGE"):
            safe += 1
        elif rule.endswith("/LOW_USAGE"):
            likely += 1
        elif rule.endswith("/ANOMALY"):
            anomaly += 1
else:
    raise SystemExit(f"unsupported format: {fmt}")

print(f"{safe},{likely},{anomaly}")
PY
}

evaluate_gate() {
  local fail_on="$1"
  local safe="$2"
  local likely="$3"
  local anomaly="$4"

  local should_fail=false
  local threshold

  case "$fail_on" in
    ""|none|off|false)
      threshold=0
      ;;
    safe|safe-to-drop|high)
      threshold="$safe"
      ;;
    likely|likely-safe|medium)
      threshold=$((safe + likely))
      ;;
    any|all|low)
      threshold=$((safe + likely + anomaly))
      ;;
    anomaly)
      threshold="$anomaly"
      ;;
    *)
      fail "invalid fail-on value: ${fail_on} (supported: none, safe, likely, any, anomaly)"
      ;;
  esac

  if [[ "$threshold" -gt 0 ]]; then
    should_fail=true
  fi

  echo "$should_fail"
}

write_output() {
  local key="$1"
  local value="$2"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf '%s=%s\n' "$key" "$value" >>"$GITHUB_OUTPUT"
  fi
}

main() {
  local clickhouse_url format fail_on extra_args raw_platform os arch ext

  clickhouse_url="${INPUT_CLICKHOUSE_URL:-}"
  [[ -n "$clickhouse_url" ]] || fail "input 'clickhouse-url' is required"

  format="$(normalize "${INPUT_FORMAT:-sarif}")"
  case "$format" in
    json|sarif) ;;
    *) fail "input 'format' must be one of: json, sarif" ;;
  esac

  fail_on="$(normalize "${INPUT_FAIL_ON:-none}")"
  extra_args="${INPUT_ARGS:-}"

  local work_dir output_dir
  work_dir="${RUNNER_TEMP:-/tmp}/clickspectre-action-${RANDOM}${RANDOM}"
  output_dir="${work_dir}/output"
  mkdir -p "$output_dir"

  raw_platform="$(resolve_platform)"
  os="${raw_platform%%,*}"
  raw_platform="${raw_platform#*,}"
  arch="${raw_platform%%,*}"
  ext="${raw_platform##*,}"

  local binary_path resolved_version
  if [[ -n "${INPUT_BINARY_PATH:-}" ]]; then
    binary_path="${INPUT_BINARY_PATH}"
    [[ -f "$binary_path" ]] || fail "binary-path does not exist: ${binary_path}"
    chmod +x "$binary_path" || true
    resolved_version="local"
  else
    resolved_version="$(resolve_version)"
    binary_path="$(download_binary "$resolved_version" "$os" "$arch" "$ext" "$work_dir")"
  fi

  local -a cmd
  cmd=("$binary_path" "analyze")
  if [[ -n "$extra_args" ]]; then
    # shellcheck disable=SC2206
    local -a parsed_args=($extra_args)
    cmd+=("${parsed_args[@]}")
  fi
  cmd+=(
    "--clickhouse-dsn" "$clickhouse_url"
    "--format" "$format"
    "--output" "$output_dir"
  )

  log "Running: ${cmd[*]}"
  "${cmd[@]}"

  local report_json report_sarif report_for_counts
  report_json=""
  report_sarif=""

  if [[ -f "${output_dir}/report.json" ]]; then
    report_json="${output_dir}/report.json"
  fi
  if [[ -f "${output_dir}/report.sarif" ]]; then
    report_sarif="${output_dir}/report.sarif"
  fi

  if [[ "$format" == "json" && -z "$report_json" ]]; then
    fail "expected JSON report at ${output_dir}/report.json"
  fi
  if [[ "$format" == "sarif" && -z "$report_sarif" ]]; then
    fail "expected SARIF report at ${output_dir}/report.sarif"
  fi

  if [[ "$format" == "sarif" ]]; then
    report_for_counts="$report_sarif"
  else
    report_for_counts="$report_json"
  fi

  local counts safe_count likely_count anomaly_count total_count
  counts="$(count_findings "$format" "$report_for_counts")"
  safe_count="${counts%%,*}"
  counts="${counts#*,}"
  likely_count="${counts%%,*}"
  anomaly_count="${counts##*,}"
  total_count=$((safe_count + likely_count + anomaly_count))

  local should_fail fail_message
  should_fail="$(evaluate_gate "$fail_on" "$safe_count" "$likely_count" "$anomaly_count")"
  fail_message="fail-on=${fail_on} threshold reached (safe=${safe_count}, likely=${likely_count}, anomaly=${anomaly_count})"
  if [[ "$should_fail" != "true" ]]; then
    fail_message="fail-on=${fail_on} threshold not reached (safe=${safe_count}, likely=${likely_count}, anomaly=${anomaly_count})"
  fi

  log "Findings: safe=${safe_count}, likely=${likely_count}, anomaly=${anomaly_count}, total=${total_count}"

  write_output "resolved-version" "$resolved_version"
  write_output "report-json" "$report_json"
  write_output "report-sarif" "$report_sarif"
  write_output "findings-safe" "$safe_count"
  write_output "findings-likely" "$likely_count"
  write_output "findings-anomaly" "$anomaly_count"
  write_output "findings-total" "$total_count"
  write_output "should-fail" "$should_fail"
  write_output "fail-message" "$fail_message"
}

main "$@"
