#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
ENV_EXAMPLE_FILE="${ROOT_DIR}/.env.example"
INSTALL_STATE_FILE="${ROOT_DIR}/install/.state.env"
MODULE_HELPER="${ROOT_DIR}/install/module_registry.py"
MODULES_FILE="${ROOT_DIR}/install/modules.yaml"
COMPOSE_FILE="${ROOT_DIR}/compose.yaml"
COMPOSE_PROD_FILE="${ROOT_DIR}/compose.prod.yaml"
SETUP_SKIP_COMPOSE="${ARKLOOP_SETUP_SKIP_COMPOSE:-0}"
SETUP_LANG="${ARKLOOP_SETUP_LANG:-}"
USE_PROD_IMAGES="${ARKLOOP_PROD:-0}"

HOST_OS=""
HAS_KVM="0"
DETECTED_DOCKER_SOCKET=""
DOCKER_OK="0"
COMPOSE_OK="0"
COMPOSE_BASE_CMD=()
HAD_ENV_FILE_BEFORE_INSTALL="0"

normalize_setup_lang() {
  local lang="$1"
  case "$lang" in
    "")
      local detected="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
      case "$detected" in
        zh*|ZH*) printf 'zh-CN' ;;
        *) printf 'en' ;;
      esac
      ;;
    zh|zh-CN|zh_CN|cn|CN) printf 'zh-CN' ;;
    en|en-US|en_US|en-GB|en_GB) printf 'en' ;;
    *) fail "Unsupported setup language: $lang" ;;
  esac
}

setup_lang() {
  if [ -z "$SETUP_LANG" ]; then
    SETUP_LANG="$(normalize_setup_lang "")"
  fi
  printf '%s' "$SETUP_LANG"
}

t() {
  local key="$1"
  local lang
  lang="$(setup_lang)"
  case "$lang:$key" in
    zh-CN:usage)
      cat <<'EOF'
用法：
  ./setup.sh install [flags]
  ./setup.sh doctor [--gateway-port <port>] [--lang zh-CN|en]
  ./setup.sh status [--lang zh-CN|en]
  ./setup.sh upgrade [--prod] [--version <tag>] [--yes] [--lang zh-CN|en]
  ./setup.sh uninstall [--purge] [--yes] [--lang zh-CN|en]

install flags:
  --profile standard|full
  --mode self-hosted|saas
  --memory none|openviking
  --sandbox none|docker|firecracker
  --console lite|full
  --browser off|on
  --web-tools builtin|self-hosted
  --gateway on|off
  --gateway-port <port>
  --lang zh-CN|en
  --non-interactive
  --prod                    使用预构建镜像（compose.prod.yaml）

说明：
  - browser=on 仅支持 sandbox=docker
EOF
      ;;
    en:usage)
      cat <<'EOF'
Usage:
  ./setup.sh install [flags]
  ./setup.sh doctor [--gateway-port <port>] [--lang zh-CN|en]
  ./setup.sh status [--lang zh-CN|en]
  ./setup.sh upgrade [--prod] [--version <tag>] [--yes] [--lang zh-CN|en]
  ./setup.sh uninstall [--purge] [--yes] [--lang zh-CN|en]

install flags:
  --profile standard|full
  --mode self-hosted|saas
  --memory none|openviking
  --sandbox none|docker|firecracker
  --console lite|full
  --browser off|on
  --web-tools builtin|self-hosted
  --gateway on|off
  --gateway-port <port>
  --lang zh-CN|en
  --non-interactive
  --prod                    Use pre-built images (compose.prod.yaml)

Notes:
  - browser=on only works with sandbox=docker
EOF
      ;;
    zh-CN:missing_dependency) printf '缺少依赖：%s' "$2" ;;
    en:missing_dependency) printf 'Missing dependency: %s' "$2" ;;
    zh-CN:missing_env_example) printf '缺少 %s' "$2" ;;
    en:missing_env_example) printf 'Missing %s' "$2" ;;
    zh-CN:unknown_secret_kind) printf '未知 secret 生成类型：%s' "$2" ;;
    en:unknown_secret_kind) printf 'Unknown secret generator kind: %s' "$2" ;;
    zh-CN:install_validation_failed) printf '安装参数校验失败' ;;
    en:install_validation_failed) printf 'Install argument validation failed' ;;
    zh-CN:prompt_profile) printf '部署档位（standard/full）' ;;
    en:prompt_profile) printf 'Deployment profile (standard/full)' ;;
    zh-CN:prompt_mode) printf '部署模式（self-hosted/saas）' ;;
    en:prompt_mode) printf 'Deployment mode (self-hosted/saas)' ;;
    zh-CN:prompt_memory) printf '记忆系统（none/openviking）' ;;
    en:prompt_memory) printf 'Memory system (none/openviking)' ;;
    zh-CN:prompt_sandbox) printf '代码执行（none/docker/firecracker）' ;;
    en:prompt_sandbox) printf 'Code execution (none/docker/firecracker)' ;;
    zh-CN:prompt_web_tools) printf '搜索/抓取（builtin/self-hosted）' ;;
    en:prompt_web_tools) printf 'Search/scraping (builtin/self-hosted)' ;;
    zh-CN:prompt_console) printf 'Console（lite/full）' ;;
    en:prompt_console) printf 'Console (lite/full)' ;;
    zh-CN:prompt_browser) printf '浏览器模块（off/on）' ;;
    en:prompt_browser) printf 'Browser module (off/on)' ;;
    zh-CN:prompt_gateway) printf 'Gateway（on/off）' ;;
    en:prompt_gateway) printf 'Gateway (on/off)' ;;
    zh-CN:prompt_gateway_port) printf 'Gateway 端口' ;;
    en:prompt_gateway_port) printf 'Gateway port' ;;
    zh-CN:missing_docker_socket) printf '未找到可用的用户态 Docker socket' ;;
    en:missing_docker_socket) printf 'No usable user-space Docker socket found' ;;
    zh-CN:docker_unavailable) printf 'Docker 不可用' ;;
    en:docker_unavailable) printf 'Docker is unavailable' ;;
    zh-CN:compose_unavailable) printf 'docker compose 不可用' ;;
    en:compose_unavailable) printf 'docker compose is unavailable' ;;
    zh-CN:firecracker_linux_only) printf 'firecracker 仅支持 Linux' ;;
    en:firecracker_linux_only) printf 'firecracker is only supported on Linux' ;;
    zh-CN:kvm_missing) printf '当前宿主未检测到 KVM' ;;
    en:kvm_missing) printf 'KVM was not detected on this host' ;;
    zh-CN:gateway_port_in_use) printf '端口 %s 已被占用' "$2" ;;
    en:gateway_port_in_use) printf 'Port %s is already in use' "$2" ;;
    zh-CN:preflight_failed) printf 'pre-flight 检测未通过' ;;
    en:preflight_failed) printf 'Pre-flight checks failed' ;;
    zh-CN:stale_postgres_volume) printf '检测到旧的 PostgreSQL 数据卷，但当前 .env 是新生成的。请执行 ./setup.sh uninstall --purge --yes 清理旧卷，或恢复原来的 .env。' ;;
    en:stale_postgres_volume) printf 'An existing PostgreSQL data volume was found, but the current .env was freshly generated. Run ./setup.sh uninstall --purge --yes to remove the old volume, or restore the previous .env.' ;;
    zh-CN:unknown_arg) printf '未知参数：%s' "$2" ;;
    en:unknown_arg) printf 'Unknown argument: %s' "$2" ;;
    zh-CN:invalid_port) printf '无效端口：%s' "$2" ;;
    en:invalid_port) printf 'Invalid port: %s' "$2" ;;
    zh-CN:install_plan) printf '安装方案：profile=%s mode=%s memory=%s sandbox=%s console=%s browser=%s web-tools=%s gateway-port=%s' "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9" ;;
    en:install_plan) printf 'Install plan: profile=%s mode=%s memory=%s sandbox=%s console=%s browser=%s web-tools=%s gateway-port=%s' "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9" ;;
    zh-CN:skip_compose) printf '已跳过 Compose 执行（ARKLOOP_SETUP_SKIP_COMPOSE=1）' ;;
    en:skip_compose) printf 'Skipped Compose execution (ARKLOOP_SETUP_SKIP_COMPOSE=1)' ;;
    zh-CN:starting_modules) printf '启动模块：%s' "$2" ;;
    en:starting_modules) printf 'Starting modules: %s' "$2" ;;
    zh-CN:starting_gateway) printf '启动 Gateway' ;;
    en:starting_gateway) printf 'Starting Gateway' ;;
    zh-CN:service_health_timeout) printf '服务健康检查超时，请执行 ./setup.sh status 查看详情' ;;
    en:service_health_timeout) printf 'Service health checks timed out, run ./setup.sh status for details' ;;
    zh-CN:gateway_health_failed) printf 'Gateway 健康检查失败' ;;
    en:gateway_health_failed) printf 'Gateway health check failed' ;;
    zh-CN:console_not_ready) printf 'Console 入口未就绪' ;;
    en:console_not_ready) printf 'Console entry is not ready' ;;
    zh-CN:install_done) printf '安装完成' ;;
    en:install_done) printf 'Install completed' ;;
    zh-CN:entry_url) printf '入口地址：http://localhost:%s' "$2" ;;
    en:entry_url) printf 'Entry URL: http://localhost:%s' "$2" ;;
    zh-CN:next_step_console) printf '下一步：如上方已打印管理员初始化地址，请优先打开它；否则直接登录 Console。' ;;
    en:next_step_console) printf 'Next: open the admin bootstrap URL above if one was printed; otherwise log in to Console directly.' ;;
    zh-CN:install_done_no_gateway) printf '安装完成（未启用 Gateway）' ;;
    en:install_done_no_gateway) printf 'Install completed (Gateway disabled)' ;;
    zh-CN:status_metadata_missing) printf '未发现 setup.sh 安装元数据，仅输出当前 compose 状态' ;;
    en:status_metadata_missing) printf 'No setup.sh install metadata found, printing current compose state only' ;;
    zh-CN:upgrade_prereq_failed) printf 'upgrade 前置检查失败：Docker / Compose 不可用' ;;
    en:upgrade_prereq_failed) printf 'Upgrade pre-check failed: Docker / Compose is unavailable' ;;
    zh-CN:upgrade_no_install) printf '未找到安装记录，请先执行 ./setup.sh install' ;;
    en:upgrade_no_install) printf 'No installation found. Please run ./setup.sh install first.' ;;
    zh-CN:upgrade_current_state) printf '当前安装状态：profile=%s mode=%s' "$2" "$3" ;;
    en:upgrade_current_state) printf 'Current install state: profile=%s mode=%s' "$2" "$3" ;;
    zh-CN:upgrade_confirm) printf '确认升级？[y/N]: ' ;;
    en:upgrade_confirm) printf 'Proceed with upgrade? [y/N]: ' ;;
    zh-CN:upgrade_pulling) printf '正在拉取最新镜像...' ;;
    en:upgrade_pulling) printf 'Pulling latest images...' ;;
    zh-CN:upgrade_building) printf '正在重新构建服务...' ;;
    en:upgrade_building) printf 'Rebuilding services...' ;;
    zh-CN:upgrade_migrating) printf '正在执行数据库迁移...' ;;
    en:upgrade_migrating) printf 'Running database migrations...' ;;
    zh-CN:upgrade_restarting) printf '正在重启服务...' ;;
    en:upgrade_restarting) printf 'Restarting services...' ;;
    zh-CN:upgrade_health_wait) printf '等待服务健康检查...' ;;
    en:upgrade_health_wait) printf 'Waiting for service health checks...' ;;
    zh-CN:upgrade_done) printf '升级完成' ;;
    en:upgrade_done) printf 'Upgrade completed' ;;
    zh-CN:upgrade_failed) printf '升级失败' ;;
    en:upgrade_failed) printf 'Upgrade failed' ;;
    zh-CN:upgrade_version_set) printf '目标版本已设置为 %s' "$2" ;;
    en:upgrade_version_set) printf 'Target version set to %s' "$2" ;;
    zh-CN:upgrade_prod_note) printf '使用预构建镜像模式' ;;
    en:upgrade_prod_note) printf 'Using pre-built images mode' ;;
    zh-CN:confirm_uninstall) printf '确认卸载 Arkloop？默认保留卷与 .env [y/N]: ' ;;
    en:confirm_uninstall) printf 'Uninstall Arkloop? Volumes and .env are kept by default [y/N]: ' ;;
    zh-CN:cancelled) printf '已取消' ;;
    en:cancelled) printf 'Cancelled' ;;
    zh-CN:uninstall_done) printf '卸载完成' ;;
    en:uninstall_done) printf 'Uninstall completed' ;;
    zh-CN:unknown_command) printf '未知命令：%s' "$2" ;;
    en:unknown_command) printf 'Unknown command: %s' "$2" ;;
    *) printf '%s' "$key" ;;
  esac
}

print_usage() {
  t usage
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
  command -v "$1" >/dev/null 2>&1 || fail "$(t missing_dependency "$1")"
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
  [ -f "$ENV_EXAMPLE_FILE" ] || fail "$(t missing_env_example "$ENV_EXAMPLE_FILE")"
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
    *) fail "$(t unknown_secret_kind "$kind")" ;;
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
  if [ "$USE_PROD_IMAGES" = "1" ]; then
    COMPOSE_BASE_CMD+=(-f "$COMPOSE_PROD_FILE")
  fi
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
    fail "$(t install_validation_failed)"
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

validate_port() {
  local port="$1"
  case "$port" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$port" -ge 1 ] && [ "$port" -le 65535 ]
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
  local gateway_port="$9"

  if [ "${NON_INTERACTIVE:-0}" = "1" ]; then
    INSTALL_PROFILE="$profile"
    INSTALL_MODE="$mode"
    INSTALL_MEMORY="$memory"
    INSTALL_SANDBOX="$sandbox"
    INSTALL_CONSOLE="$console"
    INSTALL_BROWSER="$browser"
    INSTALL_WEB_TOOLS="$web_tools"
    INSTALL_GATEWAY="$gateway"
    INSTALL_GATEWAY_PORT="$gateway_port"
    return
  fi

  INSTALL_PROFILE="$(prompt_choice "$(t prompt_profile)" "${profile:-standard}")"
  INSTALL_MODE="$(prompt_choice "$(t prompt_mode)" "${mode:-self-hosted}")"
  INSTALL_MEMORY="$(prompt_choice "$(t prompt_memory)" "${memory:-}")"
  INSTALL_SANDBOX="$(prompt_choice "$(t prompt_sandbox)" "${sandbox:-}")"
  INSTALL_WEB_TOOLS="$(prompt_choice "$(t prompt_web_tools)" "${web_tools:-}")"
  INSTALL_CONSOLE="$(prompt_choice "$(t prompt_console)" "${console:-}")"
  INSTALL_BROWSER="$(prompt_choice "$(t prompt_browser)" "${browser:-off}")"
  INSTALL_GATEWAY="$(prompt_choice "$(t prompt_gateway)" "${gateway:-on}")"
  INSTALL_GATEWAY_PORT="$(prompt_choice "$(t prompt_gateway_port)" "${gateway_port:-19000}")"
}

compose_ps_lines() {
  if [ "$COMPOSE_OK" != "1" ]; then
    return 0
  fi
  local raw
  raw="$(${COMPOSE_BASE_CMD[@]} ps -a --format json 2>/dev/null || true)"
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

compose_project_name() {
  basename "$ROOT_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]//g'
}

named_volume_exists() {
  local name="$1"
  docker volume inspect "$name" >/dev/null 2>&1
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

bootstrap_init_url() {
  local gateway_port="$1"
  local endpoint="http://127.0.0.1:${gateway_port}/v1/bootstrap/init"
  local tmp_file http_code payload token expires_at
  tmp_file="$(mktemp)"
  http_code="$(curl -sS -o "$tmp_file" -w '%{http_code}' -X POST "$endpoint" || true)"
  payload="$(cat "$tmp_file" 2>/dev/null || true)"
  rm -f "$tmp_file"

  case "$http_code" in
    201)
      token="$(python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("token", ""))' <<<"$payload" 2>/dev/null || true)"
      expires_at="$(python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("expires_at", ""))' <<<"$payload" 2>/dev/null || true)"
      if [ -z "$token" ]; then
        warn "bootstrap token 创建失败：响应缺少 token"
        return 0
      fi
      printf '管理员初始化地址：http://localhost:%s/bootstrap/%s
' "$gateway_port" "$token"
      if [ -n "$expires_at" ]; then
        printf '过期时间：%s
' "$expires_at"
      fi
      ;;
    409)
      log "已存在平台管理员，跳过 bootstrap URL"
      ;;
    *)
      warn "bootstrap token 创建失败（status=${http_code:-unknown}）"
      ;;
  esac
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
  local gateway_port pg_user pg_db pg_pass redis_pass console_upstream
  gateway_port="$INSTALL_GATEWAY_PORT"
  [ -n "$gateway_port" ] || gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT)"
  [ -n "$gateway_port" ] || gateway_port="19000"
  validate_port "$gateway_port" || fail "$(t invalid_port "$gateway_port")"
  set_value ARKLOOP_GATEWAY_PORT "$gateway_port"
  pg_user="$(python_env_get ARKLOOP_POSTGRES_USER)"
  [ -n "$pg_user" ] || pg_user="arkloop"
  pg_db="$(python_env_get ARKLOOP_POSTGRES_DB)"
  [ -n "$pg_db" ] || pg_db="arkloop"
  pg_pass="$(python_env_get ARKLOOP_POSTGRES_PASSWORD)"
  redis_pass="$(python_env_get ARKLOOP_REDIS_PASSWORD)"

  set_value DATABASE_URL "postgresql://${pg_user}:${pg_pass}@127.0.0.1:5432/${pg_db}"
  set_value ARKLOOP_DATABASE_URL "postgresql://${pg_user}:${pg_pass}@127.0.0.1:5432/${pg_db}"
  set_value ARKLOOP_PGBOUNCER_URL "postgresql://${pg_user}:${pg_pass}@127.0.0.1:5433/${pg_db}"
  set_value ARKLOOP_REDIS_URL "redis://:${redis_pass}@127.0.0.1:6379/0"
  set_value ARKLOOP_GATEWAY_REDIS_URL "redis://:${redis_pass}@127.0.0.1:6379/1"
  set_if_empty ARKLOOP_GATEWAY_CORS_ALLOWED_ORIGINS "http://localhost:19080,http://localhost:19081,http://localhost:19082"

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
      [ -n "$DETECTED_DOCKER_SOCKET" ] || fail "$(t missing_docker_socket)"
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

  # SaaS mode: PGBouncer, S3 storage, security hardening
  if [ "$RESOLVED_MODE" = "saas" ]; then
    # When PGBouncer is selected, route Docker traffic through it
    if printf '%s' "$SELECTED_MODULES" | grep -q pgbouncer; then
      set_value ARKLOOP_DOCKER_DATABASE_URL "postgresql://${pg_user}:${pg_pass}@pgbouncer:5432/${pg_db}"
      set_value ARKLOOP_DOCKER_DATABASE_DIRECT_URL "postgresql://${pg_user}:${pg_pass}@postgres:5432/${pg_db}"
    fi
    # When SeaweedFS is selected, switch storage backend to S3
    if printf '%s' "$SELECTED_MODULES" | grep -q seaweedfs; then
      set_value ARKLOOP_STORAGE_BACKEND "s3"
      set_if_empty ARKLOOP_S3_ENDPOINT "http://127.0.0.1:9000"
      set_if_empty ARKLOOP_S3_ENDPOINT_DOCKER "http://seaweedfs:8333"
      set_if_empty ARKLOOP_S3_REGION "us-east-1"
    fi
    # SaaS: disable fake-IP trust, set Turnstile placeholders
    set_value ARKLOOP_OUTBOUND_TRUST_FAKE_IP "false"
    set_if_empty ARKLOOP_TURNSTILE_SECRET_KEY ""
    set_if_empty ARKLOOP_TURNSTILE_SITE_KEY ""
    set_if_empty ARKLOOP_TURNSTILE_ALLOWED_HOST ""
  fi

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
  python_env_delete ARKLOOP_SETUP_LANG

  set_install_state ARKLOOP_INSTALL_PROFILE "$RESOLVED_PROFILE"
  set_install_state ARKLOOP_INSTALL_MODE "$RESOLVED_MODE"
  set_install_state ARKLOOP_INSTALL_MEMORY "$RESOLVED_MEMORY"
  set_install_state ARKLOOP_INSTALL_SANDBOX "$RESOLVED_SANDBOX"
  set_install_state ARKLOOP_INSTALL_CONSOLE "$RESOLVED_CONSOLE"
  set_install_state ARKLOOP_INSTALL_BROWSER "$RESOLVED_BROWSER"
  set_install_state ARKLOOP_INSTALL_WEB_TOOLS "$RESOLVED_WEB_TOOLS"
  set_install_state ARKLOOP_INSTALL_GATEWAY "$RESOLVED_GATEWAY"
  set_install_state ARKLOOP_INSTALL_MODULES "$(printf '%s' "$SELECTED_MODULES" | paste -sd, -)"
  set_install_state ARKLOOP_SETUP_LANG "$(setup_lang)"
}

preflight_install() {
  local failures=0
  require_command python3
  require_command curl
  detect_host
  check_docker_tools
  detect_docker_socket

  local project_name postgres_volume
  project_name="$(compose_project_name)"
  postgres_volume="${project_name}_postgres_data"
  if [ "$HAD_ENV_FILE_BEFORE_INSTALL" = "0" ] && named_volume_exists "$postgres_volume"; then
    warn "$(t stale_postgres_volume)"
    failures=1
  fi

  if [ "$DOCKER_OK" != "1" ]; then
    warn "$(t docker_unavailable)"
    failures=1
  fi
  if [ "$COMPOSE_OK" != "1" ]; then
    warn "$(t compose_unavailable)"
    failures=1
  fi

  if [ "$RESOLVED_SANDBOX" = "firecracker" ]; then
    if [ "$HOST_OS" != "linux" ]; then
      warn "$(t firecracker_linux_only)"
      failures=1
    fi
    if [ "$HAS_KVM" != "1" ]; then
      warn "$(t kvm_missing)"
      failures=1
    fi
  fi

  if [ "$RESOLVED_SANDBOX" = "docker" ] && [ -z "$DETECTED_DOCKER_SOCKET" ]; then
    warn "$(t missing_docker_socket)"
    failures=1
  fi

  compose_base_cmd "$COMPOSE_PROFILES"

  local gateway_port
  gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT || true)"
  [ -n "$gateway_port" ] || gateway_port="19000"
  if [ "$RESOLVED_GATEWAY" = "on" ] && port_in_use "$gateway_port"; then
    if ! service_ready gateway >/dev/null 2>&1; then
      warn "$(t gateway_port_in_use "$gateway_port")"
      failures=1
    fi
  fi

  [ "$failures" -eq 0 ] || fail "$(t preflight_failed)"
}

run_install() {
  local profile="" mode="" memory="" sandbox="" console="" browser="" web_tools="" gateway="" gateway_port=""
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
      --gateway-port) gateway_port="$2"; shift 2 ;;
      --lang) SETUP_LANG="$(normalize_setup_lang "$2")"; shift 2 ;;
      --non-interactive) NON_INTERACTIVE="1"; shift ;;
      --prod) USE_PROD_IMAGES="1"; shift ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "$(t unknown_arg "$1")" ;;
    esac
  done

  detect_host
  if [ -f "$ENV_FILE" ]; then
    HAD_ENV_FILE_BEFORE_INSTALL="1"
  else
    HAD_ENV_FILE_BEFORE_INSTALL="0"
  fi
  collect_install_inputs "$profile" "$mode" "$memory" "$sandbox" "$console" "$browser" "$web_tools" "$gateway" "$gateway_port"
  resolve_plan "$INSTALL_PROFILE" "$INSTALL_MODE" "$INSTALL_MEMORY" "$INSTALL_SANDBOX" "$INSTALL_CONSOLE" "$INSTALL_BROWSER" "$INSTALL_WEB_TOOLS" "$INSTALL_GATEWAY"

  ensure_env_file
  ensure_secret ARKLOOP_POSTGRES_PASSWORD hex16
  ensure_secret ARKLOOP_REDIS_PASSWORD hex16
  ensure_secret ARKLOOP_AUTH_JWT_SECRET base64_48
  ensure_secret ARKLOOP_ENCRYPTION_KEY hex32
  ensure_secret ARKLOOP_SANDBOX_AUTH_TOKEN hex32
  ensure_secret ARKLOOP_S3_SECRET_KEY hex32
  set_if_empty ARKLOOP_S3_ACCESS_KEY arkloop
  detect_host
  check_docker_tools
  detect_docker_socket
  apply_runtime_env
  preflight_install

  log "$(t install_plan "$RESOLVED_PROFILE" "$RESOLVED_MODE" "$RESOLVED_MEMORY" "$RESOLVED_SANDBOX" "$RESOLVED_CONSOLE" "$RESOLVED_BROWSER" "$RESOLVED_WEB_TOOLS" "$INSTALL_GATEWAY_PORT")"

  if [ "$SETUP_SKIP_COMPOSE" = "1" ]; then
    log "$(t skip_compose)"
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
    log "$(t starting_modules "${phase_one[*]}")"
    local cmd=("${COMPOSE_BASE_CMD[@]}" up -d "${phase_one[@]}")
    "${cmd[@]}"
  fi

  if [ "${#phase_two[@]}" -gt 0 ]; then
    log "$(t starting_gateway)"
    local cmd=("${COMPOSE_BASE_CMD[@]}" up -d "${phase_two[@]}")
    "${cmd[@]}"
  fi

  if ! wait_for_services "${SELECTED_SERVICES_ARRAY[@]}"; then
    fail "$(t service_health_timeout)"
  fi

  local gateway_port
  gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT)"
  [ -n "$gateway_port" ] || gateway_port="19000"

  if [ "$RESOLVED_GATEWAY" = "on" ]; then
    wait_for_http "http://127.0.0.1:${gateway_port}/healthz" 60 || fail "$(t gateway_health_failed)"
    wait_for_http "http://127.0.0.1:${gateway_port}/" 60 || fail "$(t console_not_ready)"
    if [ "$RESOLVED_MODE" != "saas" ]; then
      bootstrap_init_url "$gateway_port"
    fi
    log "$(t install_done)"
    printf '%s\n' "$(t entry_url "$gateway_port")"
    printf '%s\n' "$(t next_step_console)"
  else
    log "$(t install_done_no_gateway)"
  fi
}

run_doctor() {
  local gateway_port_override=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --gateway-port) gateway_port_override="$2"; shift 2 ;;
      --lang) SETUP_LANG="$(normalize_setup_lang "$2")"; shift 2 ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "$(t unknown_arg "$1")" ;;
    esac
  done

  detect_host
  check_docker_tools
  detect_docker_socket

  local gateway_port
  gateway_port="$gateway_port_override"
  [ -n "$gateway_port" ] || gateway_port="$(python_env_get ARKLOOP_GATEWAY_PORT)"
  [ -n "$gateway_port" ] || gateway_port="19000"
  validate_port "$gateway_port" || fail "$(t invalid_port "$gateway_port")"

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
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --lang) SETUP_LANG="$(normalize_setup_lang "$2")"; shift 2 ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "$(t unknown_arg "$1")" ;;
    esac
  done

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
        pgbouncer) service="pgbouncer" ;;
        seaweedfs) service="seaweedfs" ;;
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
    warn "$(t status_metadata_missing)"
    compose_base_cmd ""
    compose_ps_lines
  fi
}

run_upgrade() {
  local target_version="" prod="0" yes="0"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version) target_version="$2"; shift 2 ;;
      --prod) prod="1"; shift ;;
      --yes) yes="1"; shift ;;
      --lang) SETUP_LANG="$(normalize_setup_lang "$2")"; shift 2 ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "$(t unknown_arg "$1")" ;;
    esac
  done

  detect_host
  check_docker_tools
  if [ "$DOCKER_OK" != "1" ] || [ "$COMPOSE_OK" != "1" ]; then
    fail "$(t upgrade_prereq_failed)"
  fi

  # Read current install state
  if ! status_from_metadata; then
    fail "$(t upgrade_no_install)"
  fi

  local current_profile current_mode
  current_profile="$(python_state_get ARKLOOP_INSTALL_PROFILE || true)"
  current_mode="$(python_state_get ARKLOOP_INSTALL_MODE || true)"
  log "$(t upgrade_current_state "$current_profile" "$current_mode")"

  if [ "$prod" = "1" ]; then
    USE_PROD_IMAGES="1"
    log "$(t upgrade_prod_note)"
  fi

  if [ -n "$target_version" ]; then
    log "$(t upgrade_version_set "$target_version")"
  fi

  # Confirmation
  if [ "$yes" != "1" ]; then
    local answer
    printf '%s' "$(t upgrade_confirm)"
    IFS= read -r answer || true
    answer="$(trim "$answer")"
    [ "$answer" = "y" ] || [ "$answer" = "Y" ] || fail "$(t cancelled)"
  fi

  # Set target version in .env
  if [ -n "$target_version" ]; then
    python_env_set ARKLOOP_VERSION "$target_version"
  fi

  compose_base_cmd "$COMPOSE_PROFILES"

  # Pull images (prod mode only)
  if [ "$prod" = "1" ]; then
    log "$(t upgrade_pulling)"
    "${COMPOSE_BASE_CMD[@]}" pull || fail "$(t upgrade_failed)"
  fi

  # Run migrations
  log "$(t upgrade_migrating)"
  "${COMPOSE_BASE_CMD[@]}" run --rm migrate up || fail "$(t upgrade_failed)"

  # Recreate services
  if [ "$prod" = "1" ]; then
    log "$(t upgrade_restarting)"
    "${COMPOSE_BASE_CMD[@]}" up -d || fail "$(t upgrade_failed)"
  else
    log "$(t upgrade_building)"
    "${COMPOSE_BASE_CMD[@]}" up -d --build || fail "$(t upgrade_failed)"
  fi

  # Wait for health
  log "$(t upgrade_health_wait)"
  local services_array
  read_lines_to_array "$COMPOSE_SERVICES" services_array
  if ! wait_for_services "${services_array[@]}"; then
    warn "$(t service_health_timeout)"
  fi

  log "$(t upgrade_done)"
}

run_uninstall() {
  local purge="0"
  local yes="0"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --purge) purge="1"; shift ;;
      --yes) yes="1"; shift ;;
      --lang) SETUP_LANG="$(normalize_setup_lang "$2")"; shift 2 ;;
      -h|--help) print_usage; exit 0 ;;
      *) fail "$(t unknown_arg "$1")" ;;
    esac
  done

  detect_host
  check_docker_tools
  status_from_metadata || true
  compose_base_cmd "${COMPOSE_PROFILES:-}"

  if [ "$yes" != "1" ]; then
    local answer
    printf '%s' "$(t confirm_uninstall)"
    IFS= read -r answer || true
    answer="$(trim "$answer")"
    [ "$answer" = "y" ] || [ "$answer" = "Y" ] || fail "$(t cancelled)"
  fi

  local cmd=("${COMPOSE_BASE_CMD[@]}" down --remove-orphans)
  if [ "$purge" = "1" ]; then
    cmd+=(--volumes)
  fi
  "${cmd[@]}"
  rm -f "$INSTALL_STATE_FILE"
  log "$(t uninstall_done)"
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
      shift || true
      while [ "$#" -gt 0 ]; do
        case "$1" in
          --lang) SETUP_LANG="$(normalize_setup_lang "$2")"; shift 2 ;;
          *) fail "$(t unknown_arg "$1")" ;;
        esac
      done
      print_usage
      ;;
    *)
      fail "$(t unknown_command "$command")"
      ;;
  esac
}

main "$@"
