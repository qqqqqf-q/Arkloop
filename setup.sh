#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
ENV_EXAMPLE_FILE="${ROOT_DIR}/.env.example"
INSTALL_STATE_FILE="${ROOT_DIR}/install/.state.env"
MODULE_HELPER="${ROOT_DIR}/install/module_registry.py"
MODULES_FILE="${ROOT_DIR}/install/modules.yaml"
COMPOSE_FILE="${ROOT_DIR}/compose.yaml"
SETUP_SKIP_COMPOSE="${ARKLOOP_SETUP_SKIP_COMPOSE:-0}"

HOST_OS=""
HAS_KVM="0"
DETECTED_DOCKER_SOCKET=""
DOCKER_OK="0"
COMPOSE_OK="0"
COMPOSE_BASE_CMD=()

print_usage() {
  cat <<'EOF'
用法：
  ./setup.sh install [flags]
  ./setup.sh doctor
  ./setup.sh status
  ./setup.sh upgrade
  ./setup.sh uninstall [--purge] [--yes]

install flags:
  --profile standard|full
  --mode self-hosted|saas
  --memory none|openviking
  --sandbox none|docker|firecracker
  --console lite|full
  --browser off|on
  --web-tools builtin|self-hosted
  --gateway on|off
  --non-interactive

说明：
  - PR2 暂不支持 --mode saas
  - browser=on 仅支持 sandbox=docker
EOF
}

log() {
  printf '[setup] %s\n' "$*"
}

warn() {
  printf '[setup] warning: %s\n' "$*" >&2
}

fail() {
  printf '[setup] error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少依赖：$1"
}

trim() {
  local value="$1"
  value="${value#${value%%[![:space:]]*}}"
  value="${value%${value##*[![:space:]]}}"
  printf '%s' "$value"
}

detect_host() {
  local uname_out
  uname_out="$(uname -s)"
  case "$uname_out" in
    Linux)
      if [ -f /proc/version ] && grep -qi microsoft /proc/version; then
        HOST_OS="wsl2"
      else
        HOST_OS="linux"
      fi
      ;;
    Darwin)
      HOST_OS="macos"
      ;;
    *)
      HOST_OS="macos"
      ;;
  esac

  if [ "$HOST_OS" = "linux" ] && [ -c /dev/kvm ]; then
    HAS_KVM="1"
  else
    HAS_KVM="0"
  fi
}

check_docker_tools() {
  if command -v docker >/dev/null 2>&1; then
    if docker info >/dev/null 2>&1; then
      DOCKER_OK="1"
    fi
  fi
  if [ "$DOCKER_OK" = "1" ] && docker compose version >/dev/null 2>&1; then
    COMPOSE_OK="1"
  fi
}

python_env_get() {
  python3 - "$ENV_FILE" "$1" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
key = sys.argv[2]
if not path.exists():
    raise SystemExit(0)
for raw in path.read_text(encoding='utf-8').splitlines():
    line = raw.strip()
    if not line or line.startswith('#') or '=' not in raw:
        continue
    k, v = raw.split('=', 1)
    if k.strip() == key:
        print(v)
PY
}

python_env_set() {
  python3 - "$ENV_FILE" "$1" "$2" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
key = sys.argv[2]
value = sys.argv[3]
if path.exists():
    lines = path.read_text(encoding='utf-8').splitlines()
else:
    lines = []
out = []
updated = False
for raw in lines:
    if raw.strip().startswith('#') or '=' not in raw:
        out.append(raw)
        continue
    current_key, _ = raw.split('=', 1)
    if current_key.strip() == key:
        out.append(f"{key}={value}")
        updated = True
    else:
        out.append(raw)
if not updated:
    if out and out[-1] != '':
        out.append('')
    out.append(f"{key}={value}")
path.write_text("\n".join(out) + "\n", encoding='utf-8')
PY
}

python_env_delete() {
  python3 - "$ENV_FILE" "$1" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
key = sys.argv[2]
if not path.exists():
    raise SystemExit(0)
lines = path.read_text(encoding='utf-8').splitlines()
out = []
for raw in lines:
    if raw.strip().startswith('#') or '=' not in raw:
        out.append(raw)
        continue
    current_key, _ = raw.split('=', 1)
    if current_key.strip() != key:
        out.append(raw)
path.write_text("\n".join(out) + "\n", encoding='utf-8')
PY
}

python_state_get() {
  python3 - "$INSTALL_STATE_FILE" "$1" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
key = sys.argv[2]
if not path.exists():
    raise SystemExit(0)
for raw in path.read_text(encoding='utf-8').splitlines():
    line = raw.strip()
    if not line or line.startswith('#') or '=' not in raw:
        continue
    k, v = raw.split('=', 1)
    if k.strip() == key:
        print(v)
PY
}

python_state_set() {
  python3 - "$INSTALL_STATE_FILE" "$1" "$2" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
path.parent.mkdir(parents=True, exist_ok=True)
key = sys.argv[2]
value = sys.argv[3]
if path.exists():
    lines = path.read_text(encoding='utf-8').splitlines()
else:
    lines = []
out = []
updated = False
for raw in lines:
    if raw.strip().startswith('#') or '=' not in raw:
        out.append(raw)
        continue
    current_key, _ = raw.split('=', 1)
    if current_key.strip() == key:
        out.append(f"{key}={value}")
        updated = True
    else:
        out.append(raw)
if not updated:
    if out and out[-1] != '':
        out.append('')
    out.append(f"{key}={value}")
path.write_text("\n".join(out) + "\n", encoding='utf-8')
PY
}

ensure_env_file() {
  [ -f "$ENV_EXAMPLE_FILE" ] || fail "缺少 $ENV_EXAMPLE_FILE"
  if [ ! -f "$ENV_FILE" ]; then
    cp "$ENV_EXAMPLE_FILE" "$ENV_FILE"
  fi
}

generate_hex() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$1"
  else
    python3 - "$1" <<'PY'
import secrets, sys
print(secrets.token_hex(int(sys.argv[1])))
PY
  fi
}

generate_base64() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 "$1" | tr -d '\n'
  else
    python3 - "$1" <<'PY'
import base64, secrets, sys
print(base64.b64encode(secrets.token_bytes(int(sys.argv[1]))).decode())
PY
  fi
}

ensure_secret() {
  local key="$1"
  local kind="$2"
  local current
  current="$(python_env_get "$key")"
  case "$key" in
    ARKLOOP_POSTGRES_PASSWORD)
      if [ -n "$current" ] && [ "$current" != "please_change_me" ]; then return; fi
      ;;
    ARKLOOP_REDIS_PASSWORD)
      if [ -n "$current" ] && [ "$current" != "arkloop_redis" ]; then return; fi
      ;;
    ARKLOOP_AUTH_JWT_SECRET)
      case "$current" in
        ""|please_change_me*) ;;
        *) return ;;
      esac
      ;;
    ARKLOOP_ENCRYPTION_KEY)
      case "$current" in
        ""|please_generate_with_*) ;;
        *) return ;;
      esac
      ;;
    ARKLOOP_SANDBOX_AUTH_TOKEN|ARKLOOP_S3_SECRET_KEY)
      if [ -n "$current" ] && [ "$current" != "please_change_me" ]; then return; fi
      ;;
    *)
      if [ -n "$current" ]; then return; fi
      ;;
  esac

  local generated=""
  case "$kind" in
    hex16) generated="$(generate_hex 16)" ;;
    hex32) generated="$(generate_hex 32)" ;;
    base64_48) generated="$(generate_base64 48)" ;;
    *) fail "未知 secret 生成类型：$kind" ;;
  esac
  python_env_set "$key" "$generated"
}

set_if_empty() {
  local key="$1"
  local value="$2"
  local current
  current="$(python_env_get "$key")"
  if [ -z "$current" ]; then
    python_env_set "$key" "$value"
  fi
}

set_value() {
  python_env_set "$1" "$2"
}

set_install_state() {
  python_state_set "$1" "$2"
}

port_in_use() {
  python3 - "$1" <<'PY'
import socket, sys
port = int(sys.argv[1])
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.settimeout(0.2)
    sys.exit(0 if sock.connect_ex(("127.0.0.1", port)) == 0 else 1)
PY
}

detect_docker_socket() {
  local explicit="${ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH:-}"
  if [ -z "$explicit" ]; then
    explicit="$(python_env_get ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH)"
  fi
  if [ -n "$explicit" ] && [ "$explicit" != "/var/run/docker.sock" ] && [ -S "$explicit" ]; then
    DETECTED_DOCKER_SOCKET="$explicit"
    return
  fi

  local candidates=()
  case "$HOST_OS" in
    linux)
      if [ -n "${XDG_RUNTIME_DIR:-}" ]; then
        candidates+=("${XDG_RUNTIME_DIR}/docker.sock")
      fi
      candidates+=("/run/user/$(id -u)/docker.sock" "$HOME/.docker/run/docker.sock")
      ;;
    macos)
      candidates+=("$HOME/.docker/run/docker.sock")
      ;;
    wsl2)
      candidates+=("/mnt/wsl/docker-desktop/shared-sockets/guest-services/docker.sock" "$HOME/.docker/run/docker.sock")
      ;;
  esac

  local candidate
  for candidate in "${candidates[@]}"; do
    if [ -S "$candidate" ]; then
      DETECTED_DOCKER_SOCKET="$candidate"
      return
    fi
  done
  DETECTED_DOCKER_SOCKET=""
}

compose_base_cmd() {
  local profiles_text="$1"
  COMPOSE_BASE_CMD=(docker compose -f "$COMPOSE_FILE")
  local line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    COMPOSE_BASE_CMD+=(--profile "$line")
  done <<EOF
$profiles_text
EOF
}

read_lines_to_array() {
  local text="$1"
  local target_name="$2"
  eval "$target_name=()"
  local line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    eval "$target_name+=(\"\$line\")"
  done <<EOF
$text
EOF
}

resolve_plan() {
  local profile="$1"
  local mode="$2"
  local memory="$3"
  local sandbox="$4"
  local console="$5"
  local browser="$6"
  local web_tools="$7"
  local gateway="$8"
  local cmd=(python3 "$MODULE_HELPER" resolve --modules "$MODULES_FILE" --host-os "$HOST_OS")
  if [ "$HAS_KVM" = "1" ]; then
    cmd+=(--has-kvm)
  fi
  [ -n "$profile" ] && cmd+=(--profile "$profile")
  [ -n "$mode" ] && cmd+=(--mode "$mode")
  [ -n "$memory" ] && cmd+=(--memory "$memory")
  [ -n "$sandbox" ] && cmd+=(--sandbox "$sandbox")
  [ -n "$console" ] && cmd+=(--console "$console")
  [ -n "$browser" ] && cmd+=(--browser "$browser")
  [ -n "$web_tools" ] && cmd+=(--web-tools "$web_tools")
  [ -n "$gateway" ] && cmd+=(--gateway "$gateway")
  local output
  if ! output="$("${cmd[@]}")"; then
    fail "安装参数校验失败"
  fi
  eval "$output"
}

prompt_choice() {
  local label="$1"
  local default_value="$2"
  local result=""
  printf '%s [%s]: ' "$label" "$default_value" >&2
  IFS= read -r result || true
  result="$(trim "$result")"
  if [ -z "$result" ]; then
    result="$default_value"
  fi
  printf '%s' "$result"
}

collect_install_inputs() {
  local profile="$1"
  local mode="$2"
  local memory="$3"
  local sandbox="$4"
  local console="$5"
  local browser="$6"
  local web_tools="$7"
  local gateway="$8"

  if [ "${NON_INTERACTIVE:-0}" = "1" ]; then
    INSTALL_PROFILE="$profile"
    INSTALL_MODE="$mode"
    INSTALL_MEMORY="$memory"
    INSTALL_SANDBOX="$sandbox"
    INSTALL_CONSOLE="$console"
    INSTALL_BROWSER="$browser"
    INSTALL_WEB_TOOLS="$web_tools"
    INSTALL_GATEWAY="$gateway"
    return
  fi

  INSTALL_PROFILE="$(prompt_choice '部署档位（standard/full）' "${profile:-standard}")"
  INSTALL_MODE="$(prompt_choice '部署模式（self-hosted/saas）' "${mode:-self-hosted}")"
  INSTALL_MEMORY="$(prompt_choice '记忆系统（none/openviking）' "${memory:-}")"
  INSTALL_SANDBOX="$(prompt_choice '代码执行（none/docker/firecracker）' "${sandbox:-}")"
  INSTALL_WEB_TOOLS="$(prompt_choice '搜索/抓取（builtin/self-hosted）' "${web_tools:-}")"
  INSTALL_CONSOLE="$(prompt_choice 'Console（lite/full）' "${console:-}")"
  INSTALL_BROWSER="$(prompt_choice '浏览器模块（off/on）' "${browser:-off}")"
  INSTALL_GATEWAY="$(prompt_choice 'Gateway（on/off）' "${gateway:-on}")"
}

compose_ps_lines() {
  if [ "$COMPOSE_OK" != "1" ]; then
    return 0
  fi
  local raw
  raw="$(${COMPOSE_BASE_CMD[@]} ps --format json 2>/dev/null || true)"
  python3 - <<'PY' "$raw"
import json, sys
raw = sys.argv[1].strip()
if not raw:
    raise SystemExit(0)
items = []
try:
    parsed = json.loads(raw)
    if isinstance(parsed, dict):
        items = [parsed]
    elif isinstance(parsed, list):
        items = parsed
except Exception:
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        items.append(json.loads(line))
for item in items:
    print("\t".join([
        str(item.get("Service", "")),
        str(item.get("State", "")),
        str(item.get("Health", "")),
        str(item.get("ExitCode", "")),
    ]))
PY
}

service_status_line() {
  local target_service="$1"
  local line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    local service state health exit_code
    service="${line%%$'\t'*}"
    if [ "$service" = "$target_service" ]; then
      printf '%s' "$line"
      return 0
    fi
  done <<EOF
$(compose_ps_lines)
EOF
  return 1
}

service_ready() {
  local service="$1"
  local record state health exit_code rest
  if ! record="$(service_status_line "$service")"; then
    return 1
  fi
  rest="${record#*$'\t'}"
  state="${rest%%$'\t'*}"
  rest="${rest#*$'\t'}"
  health="${rest%%$'\t'*}"
  exit_code="${record##*$'\t'}"

  case "$service" in
    migrate)
      [ "$state" = "exited" ] && [ "$exit_code" = "0" ]
      return
      ;;
  esac

  if [ "$health" = "healthy" ]; then
    return 0
  fi
  [ "$state" = "running" ] && [ -z "$health" ]
}

wait_for_http() {
  local url="$1"
  local timeout_seconds="$2"
  local started_at now
  started_at="$(date +%s)"
  while true; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    now="$(date +%s)"
    if [ $((now - started_at)) -ge "$timeout_seconds" ]; then
      return 1
    fi
    sleep 2
  done
}

wait_for_services() {
  local -a services=("$@")
  local started_at now
  started_at="$(date +%s)"
  while true; do
    local all_ready="1"
    local service
    for service in "${services[@]}"; do
      if ! service_ready "$service"; then
        all_ready="0"
        break
      fi
    done
    if [ "$all_ready" = "1" ]; then
      return 0
    fi
    now="$(date +%s)"
    if [ $((now - started_at)) -ge 180 ]; then
      return 1
    fi
    sleep 3
  done
}

apply_runtime_env() {
  local gateway_port pg_user pg_db pg_pass pg_port pgbouncer_port redis_pass redis_port web_port console_port console_lite_port console_upstream
  gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT)"
  [ -n "$gateway_port" ] || gateway_port="8000"
  pg_user="$(python_env_get ARKLOOP_POSTGRES_USER)"
  [ -n "$pg_user" ] || pg_user="arkloop"
  pg_db="$(python_env_get ARKLOOP_POSTGRES_DB)"
  [ -n "$pg_db" ] || pg_db="arkloop"
  pg_pass="$(python_env_get ARKLOOP_POSTGRES_PASSWORD)"
  pg_port="$(python_env_get ARKLOOP_POSTGRES_PORT)"
  [ -n "$pg_port" ] || pg_port="5432"
  pgbouncer_port="$(python_env_get ARKLOOP_PGBOUNCER_PORT)"
  [ -n "$pgbouncer_port" ] || pgbouncer_port="5433"
  redis_pass="$(python_env_get ARKLOOP_REDIS_PASSWORD)"
  redis_port="$(python_env_get ARKLOOP_REDIS_PORT)"
  [ -n "$redis_port" ] || redis_port="6379"
  web_port="$(python_env_get ARKLOOP_WEB_PORT)"
  [ -n "$web_port" ] || web_port="5173"
  console_port="$(python_env_get ARKLOOP_CONSOLE_PORT)"
  [ -n "$console_port" ] || console_port="5174"
  console_lite_port="$(python_env_get ARKLOOP_CONSOLE_LITE_PORT)"
  [ -n "$console_lite_port" ] || console_lite_port="5175"

  set_value DATABASE_URL "postgresql://${pg_user}:${pg_pass}@127.0.0.1:${pg_port}/${pg_db}"
  set_value ARKLOOP_DATABASE_URL "postgresql://${pg_user}:${pg_pass}@127.0.0.1:${pg_port}/${pg_db}"
  set_value ARKLOOP_PGBOUNCER_URL "postgresql://${pg_user}:${pg_pass}@127.0.0.1:${pgbouncer_port}/${pg_db}"
  set_value ARKLOOP_REDIS_URL "redis://:${redis_pass}@127.0.0.1:${redis_port}/0"
  set_value ARKLOOP_GATEWAY_REDIS_URL "redis://:${redis_pass}@127.0.0.1:${redis_port}/1"
  set_if_empty ARKLOOP_GATEWAY_CORS_ALLOWED_ORIGINS "http://localhost:${web_port},http://localhost:${console_port},http://localhost:${console_lite_port}"

  case "$RESOLVED_CONSOLE" in
    lite) console_upstream="http://console-lite:80" ;;
    full) console_upstream="http://console:80" ;;
    *) console_upstream="" ;;
  esac
  set_value ARKLOOP_GATEWAY_FRONTEND_UPSTREAM "$console_upstream"

  case "$RESOLVED_MEMORY" in
    openviking|none) python_env_delete ARKLOOP_OPENVIKING_BASE_URL ;;
  esac

  case "$RESOLVED_SANDBOX" in
    docker)
      set_value ARKLOOP_SANDBOX_PROVIDER "docker"
      [ -n "$DETECTED_DOCKER_SOCKET" ] || fail "未找到可用的用户态 Docker socket"
      set_value ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH "$DETECTED_DOCKER_SOCKET"
      ;;
    firecracker)
      set_value ARKLOOP_SANDBOX_PROVIDER "firecracker"
      python_env_delete ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH
      ;;
    none)
      python_env_delete ARKLOOP_SANDBOX_PROVIDER
      python_env_delete ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH
      ;;
  esac
  python_env_delete ARKLOOP_SANDBOX_BASE_URL

  if [ "$RESOLVED_BROWSER" = "on" ]; then
    set_value ARKLOOP_SANDBOX_WARM_BROWSER "1"
  else
    set_value ARKLOOP_SANDBOX_WARM_BROWSER "0"
  fi

  python_env_delete ARKLOOP_APP_BASE_URL
  python_env_delete ARKLOOP_WEB_SEARCH_PROVIDER
  python_env_delete ARKLOOP_WEB_SEARCH_SEARXNG_BASE_URL
  python_env_delete ARKLOOP_WEB_FETCH_PROVIDER
  python_env_delete ARKLOOP_WEB_FETCH_FIRECRAWL_BASE_URL

  python_env_delete ARKLOOP_INSTALL_PROFILE
  python_env_delete ARKLOOP_INSTALL_MODE
  python_env_delete ARKLOOP_INSTALL_MEMORY
  python_env_delete ARKLOOP_INSTALL_SANDBOX
  python_env_delete ARKLOOP_INSTALL_CONSOLE
  python_env_delete ARKLOOP_INSTALL_BROWSER
  python_env_delete ARKLOOP_INSTALL_WEB_TOOLS
  python_env_delete ARKLOOP_INSTALL_GATEWAY
  python_env_delete ARKLOOP_INSTALL_MODULES

  set_install_state ARKLOOP_INSTALL_PROFILE "$RESOLVED_PROFILE"
  set_install_state ARKLOOP_INSTALL_MODE "$RESOLVED_MODE"
  set_install_state ARKLOOP_INSTALL_MEMORY "$RESOLVED_MEMORY"
  set_install_state ARKLOOP_INSTALL_SANDBOX "$RESOLVED_SANDBOX"
  set_install_state ARKLOOP_INSTALL_CONSOLE "$RESOLVED_CONSOLE"
  set_install_state ARKLOOP_INSTALL_BROWSER "$RESOLVED_BROWSER"
  set_install_state ARKLOOP_INSTALL_WEB_TOOLS "$RESOLVED_WEB_TOOLS"
  set_install_state ARKLOOP_INSTALL_GATEWAY "$RESOLVED_GATEWAY"
  set_install_state ARKLOOP_INSTALL_MODULES "$(printf '%s' "$SELECTED_MODULES" | paste -sd, -)"
}

preflight_install() {
  local failures=0
  require_command python3
  require_command curl
  detect_host
  check_docker_tools
  detect_docker_socket

  if [ "$DOCKER_OK" != "1" ]; then
    warn "Docker 不可用"
    failures=1
  fi
  if [ "$COMPOSE_OK" != "1" ]; then
    warn "docker compose 不可用"
    failures=1
  fi

  if [ "$RESOLVED_SANDBOX" = "firecracker" ]; then
    if [ "$HOST_OS" != "linux" ]; then
      warn "firecracker 仅支持 Linux"
      failures=1
    fi
    if [ "$HAS_KVM" != "1" ]; then
      warn "当前宿主未检测到 KVM"
      failures=1
    fi
  fi

  if [ "$RESOLVED_SANDBOX" = "docker" ] && [ -z "$DETECTED_DOCKER_SOCKET" ]; then
    warn "未找到用户态 Docker socket"
    failures=1
  fi

  compose_base_cmd "$COMPOSE_PROFILES"

  if [ "$RESOLVED_GATEWAY" = "on" ] && port_in_use "$(python_env_get ARKLOOP_GATEWAY_PORT || true)"; then
    if ! service_ready gateway >/dev/null 2>&1; then
      warn "端口 $(python_env_get ARKLOOP_GATEWAY_PORT || true) 已被占用"
      failures=1
    fi
  fi

  [ "$failures" -eq 0 ] || fail "pre-flight 检测未通过"
}

run_install() {
  local profile="" mode="" memory="" sandbox="" console="" browser="" web_tools="" gateway=""
  NON_INTERACTIVE="0"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --profile) profile="$2"; shift 2 ;;
      --mode) mode="$2"; shift 2 ;;
      --memory) memory="$2"; shift 2 ;;
      --sandbox) sandbox="$2"; shift 2 ;;
      --console) console="$2"; shift 2 ;;
      --browser) browser="$2"; shift 2 ;;
      --web-tools) web_tools="$2"; shift 2 ;;
      --gateway) gateway="$2"; shift 2 ;;
      --non-interactive) NON_INTERACTIVE="1"; shift ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "未知参数：$1" ;;
    esac
  done

  detect_host
  collect_install_inputs "$profile" "$mode" "$memory" "$sandbox" "$console" "$browser" "$web_tools" "$gateway"
  resolve_plan "$INSTALL_PROFILE" "$INSTALL_MODE" "$INSTALL_MEMORY" "$INSTALL_SANDBOX" "$INSTALL_CONSOLE" "$INSTALL_BROWSER" "$INSTALL_WEB_TOOLS" "$INSTALL_GATEWAY"

  ensure_env_file
  ensure_secret ARKLOOP_POSTGRES_PASSWORD hex16
  ensure_secret ARKLOOP_REDIS_PASSWORD hex16
  ensure_secret ARKLOOP_AUTH_JWT_SECRET base64_48
  ensure_secret ARKLOOP_ENCRYPTION_KEY hex32
  ensure_secret ARKLOOP_SANDBOX_AUTH_TOKEN hex32
  detect_host
  check_docker_tools
  detect_docker_socket
  apply_runtime_env
  preflight_install

  log "安装方案：profile=${RESOLVED_PROFILE} mode=${RESOLVED_MODE} memory=${RESOLVED_MEMORY} sandbox=${RESOLVED_SANDBOX} console=${RESOLVED_CONSOLE} browser=${RESOLVED_BROWSER} web-tools=${RESOLVED_WEB_TOOLS}"

  if [ "$SETUP_SKIP_COMPOSE" = "1" ]; then
    log "已跳过 Compose 执行（ARKLOOP_SETUP_SKIP_COMPOSE=1）"
    return 0
  fi

  read_lines_to_array "$COMPOSE_PROFILES" COMPOSE_PROFILES_ARRAY
  compose_base_cmd "$COMPOSE_PROFILES"
  read_lines_to_array "$COMPOSE_SERVICES" SELECTED_SERVICES_ARRAY

  local -a phase_one=()
  local -a phase_two=()
  local service
  for service in "${SELECTED_SERVICES_ARRAY[@]}"; do
    if [ "$service" = "gateway" ]; then
      phase_two+=("$service")
    else
      phase_one+=("$service")
    fi
  done

  if [ "${#phase_one[@]}" -gt 0 ]; then
    log "启动模块：${phase_one[*]}"
    local cmd=("${COMPOSE_BASE_CMD[@]}" up -d "${phase_one[@]}")
    "${cmd[@]}"
  fi

  if [ "${#phase_two[@]}" -gt 0 ]; then
    log "启动 Gateway"
    local cmd=("${COMPOSE_BASE_CMD[@]}" up -d "${phase_two[@]}")
    "${cmd[@]}"
  fi

  if ! wait_for_services "${SELECTED_SERVICES_ARRAY[@]}"; then
    fail "服务健康检查超时，请执行 ./setup.sh status 查看详情"
  fi

  local gateway_port
  gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT)"
  [ -n "$gateway_port" ] || gateway_port="8000"

  if [ "$RESOLVED_GATEWAY" = "on" ]; then
    wait_for_http "http://127.0.0.1:${gateway_port}/healthz" 60 || fail "Gateway 健康检查失败"
    wait_for_http "http://127.0.0.1:${gateway_port}/" 60 || fail "Console 入口未就绪"
    log "安装完成"
    printf '入口地址：http://localhost:%s\n' "$gateway_port"
    printf '下一步：使用 Console 完成平台配置；bootstrap token 会在 PR3 补齐。\n'
  else
    log "安装完成（未启用 Gateway）"
  fi
}

run_doctor() {
  detect_host
  check_docker_tools
  detect_docker_socket

  local gateway_port
  gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT)"
  [ -n "$gateway_port" ] || gateway_port="8000"

  printf 'platform=%s\n' "$HOST_OS"
  printf 'docker=%s\n' "$DOCKER_OK"
  printf 'compose=%s\n' "$COMPOSE_OK"
  printf 'docker_socket=%s\n' "${DETECTED_DOCKER_SOCKET:-not-found}"
  printf 'kvm=%s\n' "$HAS_KVM"
  if port_in_use "$gateway_port"; then
    printf 'port_%s=in-use\n' "$gateway_port"
  else
    printf 'port_%s=free\n' "$gateway_port"
  fi
  if port_in_use 9000; then
    printf 'port_9000=in-use\n'
  else
    printf 'port_9000=free\n'
  fi
  printf 'started_modules='
  local first="1"
  local line service state rest
  compose_base_cmd ""
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    service="${line%%$'\t'*}"
    rest="${line#*$'\t'}"
    state="${rest%%$'\t'*}"
    case "$state" in
      running|exited)
        if [ "$first" = "0" ]; then printf ','; fi
        printf '%s' "$service"
        first="0"
        ;;
    esac
  done <<EOF
$(compose_ps_lines)
EOF
  printf '\n'
}

status_from_metadata() {
  local profile mode memory sandbox console browser web_tools gateway
  profile="$(python_state_get ARKLOOP_INSTALL_PROFILE)"
  mode="$(python_state_get ARKLOOP_INSTALL_MODE)"
  memory="$(python_state_get ARKLOOP_INSTALL_MEMORY)"
  sandbox="$(python_state_get ARKLOOP_INSTALL_SANDBOX)"
  console="$(python_state_get ARKLOOP_INSTALL_CONSOLE)"
  browser="$(python_state_get ARKLOOP_INSTALL_BROWSER)"
  web_tools="$(python_state_get ARKLOOP_INSTALL_WEB_TOOLS)"
  gateway="$(python_state_get ARKLOOP_INSTALL_GATEWAY)"

  if [ -z "$profile" ]; then
    profile="$(python_env_get ARKLOOP_INSTALL_PROFILE)"
    mode="$(python_env_get ARKLOOP_INSTALL_MODE)"
    memory="$(python_env_get ARKLOOP_INSTALL_MEMORY)"
    sandbox="$(python_env_get ARKLOOP_INSTALL_SANDBOX)"
    console="$(python_env_get ARKLOOP_INSTALL_CONSOLE)"
    browser="$(python_env_get ARKLOOP_INSTALL_BROWSER)"
    web_tools="$(python_env_get ARKLOOP_INSTALL_WEB_TOOLS)"
    gateway="$(python_env_get ARKLOOP_INSTALL_GATEWAY)"
  fi

  if [ -z "$profile" ]; then
    return 1
  fi
  detect_host
  resolve_plan "$profile" "$mode" "$memory" "$sandbox" "$console" "$browser" "$web_tools" "$gateway"
  return 0
}

run_status() {
  detect_host
  check_docker_tools

  if status_from_metadata; then
    printf 'profile=%s\n' "$RESOLVED_PROFILE"
    printf 'mode=%s\n' "$RESOLVED_MODE"
    printf 'modules=%s\n' "$(printf '%s' "$SELECTED_MODULES" | paste -sd, -)"
    compose_base_cmd "$COMPOSE_PROFILES"
    read_lines_to_array "$SELECTED_MODULES" MODULES_ARRAY
    local module service record state health rest
    for module in "${MODULES_ARRAY[@]}"; do
      case "$module" in
        postgres) service="postgres" ;;
        redis) service="redis" ;;
        migrate) service="migrate" ;;
        api) service="api" ;;
        worker) service="worker" ;;
        gateway) service="gateway" ;;
        console-lite) service="console-lite" ;;
        console) service="console" ;;
        openviking) service="openviking" ;;
        sandbox-docker|browser) service="sandbox-docker" ;;
        sandbox-firecracker) service="sandbox" ;;
        searxng) service="searxng" ;;
        firecrawl) service="firecrawl" ;;
        *) service="$module" ;;
      esac
      if record="$(service_status_line "$service")"; then
        rest="${record#*$'\t'}"
        state="${rest%%$'\t'*}"
        rest="${rest#*$'\t'}"
        health="${rest%%$'\t'*}"
        printf '%s=%s' "$module" "$state"
        if [ -n "$health" ]; then
          printf '(%s)' "$health"
        fi
        printf '\n'
      else
        printf '%s=not-created\n' "$module"
      fi
    done
  else
    warn "未发现 setup.sh 安装元数据，仅输出当前 compose 状态"
    compose_base_cmd ""
    compose_ps_lines
  fi
}

run_upgrade() {
  detect_host
  check_docker_tools
  if [ "$DOCKER_OK" != "1" ] || [ "$COMPOSE_OK" != "1" ]; then
    fail "upgrade 前置检查失败：Docker / Compose 不可用"
  fi
  printf 'upgrade=not-implemented\n'
  printf 'message=完整升级流程留到 PR9；当前 PR2 仅保留安全占位命令。\n'
}

run_uninstall() {
  local purge="0"
  local yes="0"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --purge) purge="1"; shift ;;
      --yes) yes="1"; shift ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "未知参数：$1" ;;
    esac
  done

  detect_host
  check_docker_tools
  status_from_metadata || true
  compose_base_cmd "${COMPOSE_PROFILES:-}"

  if [ "$yes" != "1" ]; then
    local answer
    printf '确认卸载 Arkloop？默认保留卷与 .env [y/N]: '
    IFS= read -r answer || true
    answer="$(trim "$answer")"
    [ "$answer" = "y" ] || [ "$answer" = "Y" ] || fail "已取消"
  fi

  local cmd=("${COMPOSE_BASE_CMD[@]}" down --remove-orphans)
  if [ "$purge" = "1" ]; then
    cmd+=(--volumes)
  fi
  "${cmd[@]}"
  rm -f "$INSTALL_STATE_FILE"
  log "卸载完成"
}

main() {
  local command="${1:-}"
  case "$command" in
    install)
      shift
      run_install "$@"
      ;;
    doctor)
      shift
      run_doctor "$@"
      ;;
    status)
      shift
      run_status "$@"
      ;;
    upgrade)
      shift
      run_upgrade "$@"
      ;;
    uninstall)
      shift
      run_uninstall "$@"
      ;;
    -h|--help|help|"")
      print_usage
      ;;
    *)
      fail "未知命令：$command"
      ;;
  esac
}

main "$@"
