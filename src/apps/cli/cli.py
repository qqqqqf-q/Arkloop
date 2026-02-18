from __future__ import annotations

from dataclasses import dataclass
import json
import os
from pathlib import Path
import sys
from typing import Any, Callable, Mapping

import anyio
import click

from packages.arkloop_client import ArkloopApiError, ArkloopClient, ArkloopClientTransportError
from packages.config import load_dotenv_if_enabled, read_dotenv

from .profiles import ProfileLocator

_DEFAULT_API_BASE_URL = "http://127.0.0.1:8001"
_ACCESS_TOKEN_ENV = "ARKLOOP_ACCESS_TOKEN"
_API_BASE_URL_ENV = "ARKLOOP_API_BASE_URL"
_LOGIN_ENV = "ARKLOOP_LOGIN"
_PASSWORD_ENV = "ARKLOOP_PASSWORD"
_DOTENV_ENABLE_ENV = "ARKLOOP_LOAD_DOTENV"
_DOTENV_FILE_ENV = "ARKLOOP_DOTENV_FILE"
_ROUTING_JSON_ENV = "ARKLOOP_PROVIDER_ROUTING_JSON"


def _json_line(payload: Mapping[str, Any]) -> None:
    click.echo(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))


def _read_json_file(path: Path) -> str:
    try:
        content = path.read_text(encoding="utf-8-sig")
    except OSError as exc:
        raise click.ClickException(f"读取 routing 文件失败: {path}") from exc
    try:
        parsed = json.loads(content)
    except json.JSONDecodeError as exc:
        raise click.ClickException(f"routing 文件不是合法 JSON: {path}") from exc
    return json.dumps(parsed, ensure_ascii=False, separators=(",", ":"))


def _ensure_dotenv_loaded(*, dotenv_path: Path | None) -> None:
    if dotenv_path is not None:
        os.environ.setdefault(_DOTENV_ENABLE_ENV, "1")
        os.environ[_DOTENV_FILE_ENV] = str(dotenv_path)
    load_dotenv_if_enabled(override=False)


def _apply_routing_file(*, routing_path: Path | None) -> None:
    if routing_path is None:
        return
    os.environ[_ROUTING_JSON_ENV] = _read_json_file(routing_path)


def _resolve_dotenv_path(
    *,
    profile: str | None,
    dotenv_file: Path | None,
    locator: ProfileLocator,
) -> Path | None:
    if dotenv_file is not None:
        return dotenv_file
    if not profile:
        return None
    resolved = locator.resolve(profile)
    if resolved is None:
        raise click.ClickException(f"未找到 profile: {profile}")
    return resolved.path


@dataclass(frozen=True, slots=True)
class CliContext:
    api_base_url: str
    token: str | None
    login: str | None
    password: str | None


def _clean_optional(value: str | None) -> str | None:
    if value is None:
        return None
    cleaned = value.strip()
    return cleaned if cleaned else None


def _resolve_api_base_url(explicit: str | None) -> str:
    candidate = _clean_optional(explicit) or _clean_optional(os.getenv(_API_BASE_URL_ENV))
    candidate = candidate or _DEFAULT_API_BASE_URL
    if not candidate.strip():
        raise click.ClickException("API Base URL 不能为空")
    return candidate.rstrip("/")


def _build_context(
    *,
    profile: str | None,
    dotenv_file: Path | None,
    routing_file: Path | None,
    api_base_url: str | None,
    token: str | None,
    login: str | None,
    password: str | None,
) -> CliContext:
    locator = ProfileLocator()
    dotenv_path = _resolve_dotenv_path(profile=profile, dotenv_file=dotenv_file, locator=locator)
    _ensure_dotenv_loaded(dotenv_path=dotenv_path)
    _apply_routing_file(routing_path=routing_file)
    return CliContext(
        api_base_url=_resolve_api_base_url(api_base_url),
        token=_clean_optional(token) or _clean_optional(os.getenv(_ACCESS_TOKEN_ENV)),
        login=_clean_optional(login) or _clean_optional(os.getenv(_LOGIN_ENV)),
        password=_clean_optional(password) or _clean_optional(os.getenv(_PASSWORD_ENV)),
    )


def _need_login_password(ctx: CliContext) -> tuple[str, str]:
    if ctx.login is None or not ctx.login.strip():
        raise click.ClickException("缺少登录名：请使用 --login 或设置 ARKLOOP_LOGIN")
    if ctx.password is None or not ctx.password.strip():
        raise click.ClickException("缺少密码：请使用 --password 或设置 ARKLOOP_PASSWORD")
    return ctx.login.strip(), ctx.password


async def _resolve_token(ctx: CliContext, *, client: ArkloopClient) -> str:
    if ctx.token is not None and ctx.token.strip():
        return ctx.token.strip()
    login, password = _need_login_password(ctx)
    return await client.login(login=login, password=password)


async def _follow_events_with_reconnect(
    *,
    client: ArkloopClient,
    token: str,
    run_id: str,
    after_seq: int,
    follow: bool,
    until_terminal: bool,
    max_reconnects: int,
    on_event: Callable[[Mapping[str, Any]], None] | None = None,
) -> tuple[int, dict[str, Any] | None]:
    cursor = int(after_seq)
    reconnect_attempts = 0
    backoff_seconds = 0.5

    terminal_event: dict[str, Any] | None = None

    while True:
        try:
            async for event in client.stream_run_events_once(
                token=token,
                run_id=run_id,
                after_seq=cursor,
                follow=follow,
            ):
                _json_line(event)
                if on_event is not None:
                    on_event(event)
                seq = event.get("seq")
                if isinstance(seq, int) and seq > cursor:
                    cursor = seq

                typ = event.get("type")
                if until_terminal and typ in {"run.completed", "run.failed"}:
                    terminal_event = dict(event)
                    return cursor, terminal_event

            if not follow:
                return cursor, terminal_event

            reconnect_attempts += 1
            if reconnect_attempts > max_reconnects:
                raise ArkloopClientTransportError(message="SSE 连接断开且重连次数已用尽")

            _json_line(
                {
                    "type": "cli.sse.reconnect",
                    "run_id": run_id,
                    "after_seq": cursor,
                    "attempt": reconnect_attempts,
                }
            )
            await anyio.sleep(backoff_seconds)
            backoff_seconds = min(backoff_seconds * 2, 3.0)
        except ArkloopClientTransportError as exc:
            reconnect_attempts += 1
            if reconnect_attempts > max_reconnects:
                raise
            _json_line(
                {
                    "type": "cli.sse.reconnect",
                    "run_id": run_id,
                    "after_seq": cursor,
                    "attempt": reconnect_attempts,
                    "error": exc.to_json(),
                }
            )
        await anyio.sleep(backoff_seconds)
        backoff_seconds = min(backoff_seconds * 2, 3.0)


@click.group()
def cli() -> None:
    pass


@cli.group("profile")
def profile_group() -> None:
    pass


@profile_group.command("list")
def profile_list() -> None:
    locator = ProfileLocator()
    profiles = locator.list_profiles()
    for item in profiles:
        _json_line(
            {
                "type": "cli.profile",
                "name": item.name,
                "path": str(item.path),
                "source": item.source,
            }
        )


@profile_group.command("show")
@click.argument("name", required=True)
@click.option("--reveal-values", is_flag=True, help="显示明文值（默认只输出 key 列表）")
def profile_show(name: str, *, reveal_values: bool) -> None:
    locator = ProfileLocator()
    profile = locator.resolve(name)
    if profile is None:
        raise click.ClickException(f"未找到 profile: {name}")

    values = read_dotenv(profile.path)
    payload: dict[str, Any] = {
        "type": "cli.profile",
        "name": profile.name,
        "path": str(profile.path),
        "source": profile.source,
        "keys": sorted(values.keys()),
    }
    if reveal_values:
        payload["values"] = dict(values)
    _json_line(payload)


@cli.command("chat")
@click.option("--profile", default=None, help="使用指定 profile（例如 llm_test/dev/staging）")
@click.option(
    "--dotenv-file",
    type=click.Path(path_type=Path, exists=True, dir_okay=False),
    default=None,
    help="指定 dotenv 文件路径（优先级高于 profile）",
)
@click.option(
    "--routing-file",
    type=click.Path(path_type=Path, exists=True, dir_okay=False),
    default=None,
    help="指定 provider routing JSON 文件路径（仅影响当前进程的环境变量）",
)
@click.option(
    "--api-base-url",
    default=None,
    show_default=_DEFAULT_API_BASE_URL,
    help="API Base URL（例如 http://127.0.0.1:8001）",
)
@click.option(
    "--token", default=None, help="复用已有 access token（也可在 env 里设置 ARKLOOP_ACCESS_TOKEN）"
)
@click.option("--login", default=None, help="登录名（当未提供 --token 时用于登录）")
@click.option("--password", default=None, help="密码（当未提供 --token 时用于登录）")
@click.option("--message", required=True, help="用户消息内容")
@click.option("--thread-title", default=None, help="Thread 标题（可选）")
@click.option("--route-id", default=None, help="可选：强制使用指定 route_id")
@click.option(
    "--max-reconnects", default=30, show_default=True, type=int, help="SSE 断线最大重连次数"
)
def chat_command(
    *,
    profile: str | None,
    dotenv_file: Path | None,
    routing_file: Path | None,
    api_base_url: str | None,
    token: str | None,
    login: str | None,
    password: str | None,
    message: str,
    thread_title: str | None,
    route_id: str | None,
    max_reconnects: int,
) -> None:
    ctx = _build_context(
        profile=profile,
        dotenv_file=dotenv_file,
        routing_file=routing_file,
        api_base_url=api_base_url,
        token=token,
        login=login,
        password=password,
    )

    async def _run() -> int:
        async with ArkloopClient(base_url=ctx.api_base_url) as client:
            token = await _resolve_token(ctx, client=client)

            thread_id = await client.create_thread(token=token, title=thread_title)
            await client.create_message(token=token, thread_id=thread_id, content=message)
            run_id, trace_id = await client.create_run(
                token=token, thread_id=thread_id, route_id=route_id
            )

            _json_line(
                {
                    "type": "cli.chat.started",
                    "thread_id": thread_id,
                    "run_id": run_id,
                    "trace_id": trace_id,
                }
            )

            assistant_parts: list[str] = []

            def _consume_delta(event: Mapping[str, Any]) -> None:
                if event.get("type") != "message.delta":
                    return
                data = event.get("data")
                if not isinstance(data, dict):
                    return
                role = data.get("role")
                if role is not None and role != "assistant":
                    return
                delta = data.get("content_delta")
                if isinstance(delta, str) and delta:
                    assistant_parts.append(delta)

            after_seq, terminal = await _follow_events_with_reconnect(
                client=client,
                token=token,
                run_id=run_id,
                after_seq=0,
                follow=True,
                until_terminal=True,
                max_reconnects=max_reconnects,
                on_event=_consume_delta,
            )
            _json_line(
                {
                    "type": "cli.chat.ended",
                    "thread_id": thread_id,
                    "run_id": run_id,
                    "trace_id": trace_id,
                    "after_seq": after_seq,
                }
            )

            if terminal is not None and terminal.get("type") == "run.failed":
                data = terminal.get("data")
                code = None
                if isinstance(data, dict):
                    code = data.get("code") or data.get("error_class")
                _json_line(
                    {
                        "type": "cli.chat.failed",
                        "run_id": run_id,
                        "trace_id": trace_id,
                        "code": str(code) if code else "run.failed",
                    }
                )
                return 1

            _json_line(
                {
                    "type": "cli.chat.result",
                    "run_id": run_id,
                    "trace_id": trace_id,
                    "content": "".join(assistant_parts),
                }
            )
            return 0

    try:
        raise SystemExit(anyio.run(_run))
    except ArkloopApiError as exc:
        _json_line({"type": "cli.error", "error": exc.to_json()})
        raise SystemExit(2)
    except ArkloopClientTransportError as exc:
        _json_line({"type": "cli.error", "error": exc.to_json()})
        raise SystemExit(2)
    except KeyboardInterrupt:
        raise SystemExit(130)


@cli.group("events")
def events_group() -> None:
    pass


@events_group.command("follow")
@click.option("--profile", default=None, help="使用指定 profile（例如 llm_test/dev/staging）")
@click.option(
    "--dotenv-file",
    type=click.Path(path_type=Path, exists=True, dir_okay=False),
    default=None,
    help="指定 dotenv 文件路径（优先级高于 profile）",
)
@click.option(
    "--routing-file",
    type=click.Path(path_type=Path, exists=True, dir_okay=False),
    default=None,
    help="指定 provider routing JSON 文件路径（仅影响当前进程的环境变量）",
)
@click.option(
    "--api-base-url",
    default=None,
    show_default=_DEFAULT_API_BASE_URL,
    help="API Base URL（例如 http://127.0.0.1:8001）",
)
@click.option(
    "--token", default=None, help="复用已有 access token（也可在 env 里设置 ARKLOOP_ACCESS_TOKEN）"
)
@click.option("--login", default=None, help="登录名（当未提供 --token 时用于登录）")
@click.option("--password", default=None, help="密码（当未提供 --token 时用于登录）")
@click.option("--run-id", required=True, help="Run ID")
@click.option("--after-seq", default=0, show_default=True, type=int, help="从该 seq 之后开始续传")
@click.option(
    "--follow/--no-follow", default=True, show_default=True, help="是否持续跟随（SSE follow）"
)
@click.option(
    "--until-terminal/--no-until-terminal", default=True, show_default=True, help="遇到终态事件退出"
)
@click.option(
    "--max-reconnects", default=60, show_default=True, type=int, help="SSE 断线最大重连次数"
)
def events_follow_command(
    *,
    profile: str | None,
    dotenv_file: Path | None,
    routing_file: Path | None,
    api_base_url: str | None,
    token: str | None,
    login: str | None,
    password: str | None,
    run_id: str,
    after_seq: int,
    follow: bool,
    until_terminal: bool,
    max_reconnects: int,
) -> None:
    ctx = _build_context(
        profile=profile,
        dotenv_file=dotenv_file,
        routing_file=routing_file,
        api_base_url=api_base_url,
        token=token,
        login=login,
        password=password,
    )

    async def _run() -> int:
        async with ArkloopClient(base_url=ctx.api_base_url) as client:
            token = await _resolve_token(ctx, client=client)
            _json_line(
                {
                    "type": "cli.events.follow",
                    "run_id": run_id,
                    "after_seq": int(after_seq),
                    "follow": bool(follow),
                    "until_terminal": bool(until_terminal),
                }
            )
            await _follow_events_with_reconnect(
                client=client,
                token=token,
                run_id=run_id,
                after_seq=after_seq,
                follow=follow,
                until_terminal=until_terminal,
                max_reconnects=max_reconnects,
            )
            return 0

    try:
        raise SystemExit(anyio.run(_run))
    except ArkloopApiError as exc:
        _json_line({"type": "cli.error", "error": exc.to_json()})
        raise SystemExit(2)
    except ArkloopClientTransportError as exc:
        _json_line({"type": "cli.error", "error": exc.to_json()})
        raise SystemExit(2)
    except KeyboardInterrupt:
        raise SystemExit(130)


def main(argv: list[str] | None = None) -> int:
    try:
        cli.main(args=argv, prog_name="arkloop", standalone_mode=False)
        return 0
    except click.ClickException as exc:
        _json_line({"type": "cli.error", "error": {"message": str(exc)}})
        return 2
    except SystemExit as exc:
        code = exc.code
        return int(code) if isinstance(code, int) else 1
    except Exception as exc:
        _json_line(
            {"type": "cli.error", "error": {"message": "internal_error", "detail": str(exc)}}
        )
        return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
