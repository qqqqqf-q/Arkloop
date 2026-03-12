#!/usr/bin/env python3
import argparse
import os
import shlex
import sys
from typing import Dict, List


def strip_comment(line: str) -> str:
    if "#" not in line:
        return line.rstrip("\n")
    head, _sep, _tail = line.partition("#")
    return head.rstrip("\n")


def parse_inline_list(raw: str) -> List[str]:
    text = raw.strip()
    if not (text.startswith("[") and text.endswith("]")):
        return []
    inner = text[1:-1].strip()
    if not inner:
        return []
    return [item.strip().strip('"').strip("'") for item in inner.split(",") if item.strip()]


def parse_scalar(raw: str):
    value = raw.strip()
    if value.startswith("[") and value.endswith("]"):
        return parse_inline_list(value)
    if value.lower() == "true":
        return True
    if value.lower() == "false":
        return False
    return value.strip('"').strip("'")


def parse_modules(path: str) -> Dict[str, dict]:
    with open(path, "r", encoding="utf-8") as handle:
        lines = handle.readlines()

    modules: Dict[str, dict] = {}
    current_module = None
    current_section = None
    current_profile = None

    for raw in lines:
        line = strip_comment(raw)
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip(" "))
        content = line.strip()

        if indent == 0:
            if content == "modules:":
                continue
            current_module = None
            current_section = None
            current_profile = None
            continue

        if indent == 2 and content.endswith(":"):
            current_module = content[:-1]
            modules[current_module] = {
                "id": current_module,
                "depends_on": [],
                "mutually_exclusive": [],
                "install_with": [],
                "platform_constraints": {},
                "profiles": {},
                "capabilities": {},
            }
            current_section = None
            current_profile = None
            continue

        if current_module is None:
            continue

        module = modules[current_module]

        if indent == 4:
            current_profile = None
            if content.endswith(":"):
                current_section = content[:-1]
                if current_section not in module:
                    if current_section in ("platform_constraints", "profiles", "capabilities"):
                        module[current_section] = {}
                    else:
                        module[current_section] = []
                continue

            key, sep, raw_value = content.partition(":")
            if not sep:
                continue
            module[key.strip()] = parse_scalar(raw_value)
            current_section = None
            continue

        if current_section == "platform_constraints" and indent == 6:
            key, sep, raw_value = content.partition(":")
            if sep:
                module["platform_constraints"][key.strip()] = parse_scalar(raw_value)
            continue

        if current_section == "capabilities" and indent == 6:
            key, sep, raw_value = content.partition(":")
            if sep:
                module["capabilities"][key.strip()] = parse_scalar(raw_value)
            continue

        if current_section == "profiles":
            if indent == 6 and content.endswith(":"):
                current_profile = content[:-1]
                module["profiles"][current_profile] = {}
                continue
            if indent == 8 and current_profile:
                key, sep, raw_value = content.partition(":")
                if sep:
                    module["profiles"][current_profile][key.strip()] = parse_scalar(raw_value)
                continue

    return modules


ALLOWED = {
    "profile": {"standard", "full"},
    "mode": {"self-hosted", "saas"},
    "memory": {"none", "openviking"},
    "sandbox": {"none", "docker", "firecracker", "auto"},
    "console": {"lite", "full"},
    "browser": {"off", "on"},
    "web_tools": {"builtin", "self-hosted"},
    "gateway": {"on", "off"},
}


def normalize_choice(value: str, field: str) -> str:
    if value is None or value == "":
        return ""
    normalized = value.strip()
    if normalized not in ALLOWED[field]:
        raise ValueError(f"{field}: unsupported value {normalized!r}")
    return normalized


def default_selections(profile: str, mode: str, host_os: str, has_kvm: bool) -> dict:
    if profile == "full":
        sandbox = "firecracker" if host_os == "linux" and has_kvm else "docker"
        defaults = {
            "memory": "openviking",
            "sandbox": sandbox,
            "console": "full",
            "browser": "off",
            "web_tools": "self-hosted",
            "gateway": "on",
        }
    else:
        defaults = {
            "memory": "none",
            "sandbox": "none",
            "console": "lite",
            "browser": "off",
            "web_tools": "builtin",
            "gateway": "on",
        }
    if mode == "saas":
        defaults["console"] = "full"
    return defaults


def ordered_unique(items: List[str]) -> List[str]:
    seen = set()
    ordered = []
    for item in items:
        if item and item not in seen:
            seen.add(item)
            ordered.append(item)
    return ordered


def resolve_plan(modules: Dict[str, dict], args) -> dict:
    profile = normalize_choice(args.profile or "standard", "profile")
    mode = normalize_choice(args.mode or "self-hosted", "mode")
    defaults = default_selections(profile, mode, args.host_os, args.has_kvm)

    memory = normalize_choice(args.memory or defaults["memory"], "memory")
    sandbox = normalize_choice(args.sandbox or defaults["sandbox"], "sandbox")
    if sandbox == "auto":
        sandbox = defaults["sandbox"]
    console = normalize_choice(args.console or defaults["console"], "console")
    browser = normalize_choice(args.browser or defaults["browser"], "browser")
    web_tools = normalize_choice(args.web_tools or defaults["web_tools"], "web_tools")
    gateway = normalize_choice(args.gateway or defaults["gateway"], "gateway")

    if browser == "on" and sandbox != "docker":
        raise ValueError("browser=on 仅支持 sandbox=docker")
    if gateway == "off" and console in {"lite", "full"}:
        raise ValueError("gateway=off 时不能启用 Console")

    selected: List[str] = []
    for module_id, module in modules.items():
        profile_meta = module.get("profiles", {}).get(mode, {})
        if profile_meta.get("required") is True:
            selected.append(module_id)

    if gateway == "on":
        selected.append("gateway")
    if console == "lite":
        selected.append("console-lite")
    if console == "full":
        selected.append("console")
    if memory == "openviking":
        selected.append("openviking")
    if sandbox == "docker":
        selected.append("sandbox-docker")
    if sandbox == "firecracker":
        selected.append("sandbox-firecracker")
    if browser == "on":
        selected.append("browser")
    if web_tools == "self-hosted":
        selected.extend(["searxng", "firecrawl"])

    # Auto-select modules with default=true for current mode, unless already covered
    for module_id, module in modules.items():
        profile_meta = module.get("profiles", {}).get(mode, {})
        if profile_meta.get("default") is True and module_id not in selected:
            excl = module.get("mutually_exclusive", []) or []
            if not any(e in selected for e in excl):
                selected.append(module_id)

    resolved_modules: List[str] = []
    visiting = set()

    def visit(module_id: str):
        if module_id not in modules:
            raise ValueError(f"unknown module: {module_id}")
        if module_id in resolved_modules:
            return
        if module_id in visiting:
            raise ValueError(f"module dependency cycle: {module_id}")
        visiting.add(module_id)
        module = modules[module_id]
        for blocked in module.get("mutually_exclusive", []) or []:
            if blocked in selected:
                raise ValueError(f"module conflict: {module_id} vs {blocked}")
        constraints = module.get("platform_constraints", {}) or {}
        if constraints.get("requires_linux") is True and args.host_os != "linux":
            raise ValueError(f"module {module_id} requires Linux")
        if constraints.get("requires_kvm") is True and not args.has_kvm:
            raise ValueError(f"module {module_id} requires KVM")
        for dep in module.get("depends_on", []) or []:
            if dep in modules:
                visit(dep)
        for dep in module.get("install_with", []) or []:
            if dep in modules:
                visit(dep)
        visiting.remove(module_id)
        if module_id not in resolved_modules:
            resolved_modules.append(module_id)

    for module_id in selected:
        visit(module_id)

    compose_services = ordered_unique([
        modules[module_id].get("compose_service", "") for module_id in resolved_modules
    ])
    compose_profiles = ordered_unique([
        modules[module_id].get("compose_profile", "") for module_id in resolved_modules
    ])
    health_modules = [
        module_id
        for module_id in resolved_modules
        if (modules[module_id].get("capabilities", {}) or {}).get("healthcheck") is True
    ]

    return {
        "profile": profile,
        "mode": mode,
        "memory": memory,
        "sandbox": sandbox,
        "console": console,
        "browser": browser,
        "web_tools": web_tools,
        "gateway": gateway,
        "selected_modules": resolved_modules,
        "compose_services": compose_services,
        "compose_profiles": compose_profiles,
        "health_modules": health_modules,
    }


def shell_quote(value: str) -> str:
    return shlex.quote(value)


def emit_shell(plan: dict):
    scalars = [
        ("RESOLVED_PROFILE", plan["profile"]),
        ("RESOLVED_MODE", plan["mode"]),
        ("RESOLVED_MEMORY", plan["memory"]),
        ("RESOLVED_SANDBOX", plan["sandbox"]),
        ("RESOLVED_CONSOLE", plan["console"]),
        ("RESOLVED_BROWSER", plan["browser"]),
        ("RESOLVED_WEB_TOOLS", plan["web_tools"]),
        ("RESOLVED_GATEWAY", plan["gateway"]),
    ]
    for key, value in scalars:
        print(f"{key}={shell_quote(value)}")
    for key, values in (
        ("SELECTED_MODULES", plan["selected_modules"]),
        ("COMPOSE_SERVICES", plan["compose_services"]),
        ("COMPOSE_PROFILES", plan["compose_profiles"]),
        ("HEALTH_MODULES", plan["health_modules"]),
    ):
        joined = "\n".join(values)
        print(f"{key}={shell_quote(joined)}")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Arkloop install module registry helper")
    subparsers = parser.add_subparsers(dest="command", required=True)

    resolve = subparsers.add_parser("resolve")
    resolve.add_argument("--modules", default=os.path.join(os.getcwd(), "install", "modules.yaml"))
    resolve.add_argument("--profile", default="")
    resolve.add_argument("--mode", default="")
    resolve.add_argument("--memory", default="")
    resolve.add_argument("--sandbox", default="")
    resolve.add_argument("--console", default="")
    resolve.add_argument("--browser", default="")
    resolve.add_argument("--web-tools", dest="web_tools", default="")
    resolve.add_argument("--gateway", default="")
    resolve.add_argument("--host-os", choices=["linux", "macos", "wsl2"], default="macos")
    resolve.add_argument("--has-kvm", action="store_true")
    resolve.add_argument("--format", choices=["shell"], default="shell")

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    try:
        modules = parse_modules(args.modules)
        if args.command == "resolve":
            plan = resolve_plan(modules, args)
            emit_shell(plan)
            return 0
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        return 1
    parser.print_help(sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
