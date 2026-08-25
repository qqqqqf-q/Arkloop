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
            continue

        if indent == 2 and content.endswith(":"):
            current_module = content[:-1]
            modules[current_module] = {
                "id": current_module,
                "depends_on": [],
                "mutually_exclusive": [],
                "install_with": [],
                "platform_constraints": {},
                "capabilities": {},
            }
            current_section = None
            continue

        if current_module is None:
            continue

        module = modules[current_module]

        if indent == 4:
            if content.endswith(":"):
                current_section = content[:-1]
                if current_section not in module:
                    if current_section in ("platform_constraints", "capabilities"):
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

    return modules


ALLOWED = {
    "profile": {"standard", "full"},
    "sandbox": {"none", "docker", "auto"},
    "browser": {"off", "on"},
    "web_tools": {"builtin", "self-hosted"},
}


def normalize_choice(value: str, field: str) -> str:
    if value is None or value == "":
        return ""
    normalized = value.strip()
    if normalized not in ALLOWED[field]:
        raise ValueError(f"{field}: unsupported value {normalized!r}")
    return normalized


def default_selections(profile: str) -> dict:
    if profile == "full":
        defaults = {
            "sandbox": "docker",
            "browser": "off",
            "web_tools": "self-hosted",
        }
    else:
        defaults = {
            "sandbox": "none",
            "browser": "off",
            "web_tools": "builtin",
        }
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
    defaults = default_selections(profile)

    sandbox = normalize_choice(args.sandbox or defaults["sandbox"], "sandbox")
    if sandbox == "auto":
        # auto 不再探测宿主：Firecracker 已移除，唯一可选后端即 docker
        sandbox = "docker"
    browser = normalize_choice(args.browser or defaults["browser"], "browser")
    web_tools = normalize_choice(args.web_tools or defaults["web_tools"], "web_tools")

    if browser == "on" and sandbox != "docker":
        raise ValueError("browser=on 仅支持 sandbox=docker")

    selected: List[str] = []

    if sandbox == "docker":
        selected.append("sandbox-docker")
    if browser == "on":
        selected.append("browser")
    if web_tools == "self-hosted":
        selected.extend(["searxng", "firecrawl"])

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
        "sandbox": sandbox,
        "browser": browser,
        "web_tools": web_tools,
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
        ("RESOLVED_SANDBOX", plan["sandbox"]),
        ("RESOLVED_BROWSER", plan["browser"]),
        ("RESOLVED_WEB_TOOLS", plan["web_tools"]),
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
    resolve.add_argument("--sandbox", default="")
    resolve.add_argument("--browser", default="")
    resolve.add_argument("--web-tools", dest="web_tools", default="")
    resolve.add_argument("--host-os", choices=["linux", "macos", "wsl2"], default="macos")
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
