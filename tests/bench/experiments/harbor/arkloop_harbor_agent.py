from __future__ import annotations

import asyncio
import json
import os
import shutil
from pathlib import Path

from harbor.agents.base import BaseAgent
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext


class ArkloopCliAgent(BaseAgent):
    @staticmethod
    def name() -> str:
        return "arkloop-cli"

    def version(self) -> str | None:
        return "dev"

    async def setup(self, environment: BaseEnvironment) -> None:
        del environment
        self.logs_dir.mkdir(parents=True, exist_ok=True)
        self._required_env("ARKLOOP_CLI_BINARY", must_be_file=True)

    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        self.logs_dir.mkdir(parents=True, exist_ok=True)

        cli_binary = self._required_env("ARKLOOP_CLI_BINARY", must_be_file=True)
        model = self._resolve_model()
        persona = self._required_env("ARKLOOP_PERSONA")

        workspace_dir = self.logs_dir / "workspace"
        if workspace_dir.exists():
            shutil.rmtree(workspace_dir)
        workspace_dir.mkdir(parents=True, exist_ok=True)

        await environment.download_dir("/app", workspace_dir)

        original_instruction_path = self.logs_dir / "instruction.original.txt"
        original_instruction_path.write_text(instruction, encoding="utf-8")

        rewritten_instruction = self._rewrite_instruction(instruction, workspace_dir)
        rewritten_instruction_path = self.logs_dir / "instruction.rewritten.txt"
        rewritten_instruction_path.write_text(rewritten_instruction, encoding="utf-8")

        args = [
            cli_binary,
            "run",
            "--output-format",
            "json",
            "--prompt-file",
            str(rewritten_instruction_path),
            "--work-dir",
            str(workspace_dir),
            "--persona",
            persona,
            "--model",
            model,
        ]

        # Host/token 由 CLI 自行从 ~/.arkloop（及 Desktop 写入的 desktop.token 等）解析，不在此注入。
        env = os.environ.copy()
        for key in ("ARKLOOP_HOST", "ARKLOOP_TOKEN"):
            env.pop(key, None)

        process = await asyncio.create_subprocess_exec(
            *args,
            cwd=str(workspace_dir),
            env=env,
            stdin=asyncio.subprocess.DEVNULL,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout_bytes, stderr_bytes = await process.communicate()
        stdout = stdout_bytes.decode("utf-8", errors="replace")
        stderr = stderr_bytes.decode("utf-8", errors="replace")

        await self._sync_workspace_to_environment(environment, workspace_dir, "/app")
        remote_snapshot = await environment.exec(
            "find /app -maxdepth 4 -type f | sort",
            timeout_sec=30,
            user="root",
        )
        (self.logs_dir / "remote.app.snapshot.txt").write_text(
            json.dumps(
                {
                    "return_code": remote_snapshot.return_code,
                    "stdout": remote_snapshot.stdout or "",
                    "stderr": remote_snapshot.stderr or "",
                },
                ensure_ascii=False,
                indent=2,
            ),
            encoding="utf-8",
        )

        (self.logs_dir / "arkloop.stdout.log").write_text(stdout, encoding="utf-8")
        (self.logs_dir / "arkloop.stderr.log").write_text(stderr, encoding="utf-8")
        payload = {
            "return_code": process.returncode,
            "stdout": stdout,
            "stderr": stderr,
            "workspace_dir": str(workspace_dir),
        }
        (self.logs_dir / "arkloop.exec.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )

        metadata: dict[str, object] = {
            "return_code": process.returncode,
            "persona": persona,
            "model": model,
            "workspace_dir": str(workspace_dir),
            "stdout_log": str(self.logs_dir / "arkloop.stdout.log"),
            "stderr_log": str(self.logs_dir / "arkloop.stderr.log"),
            "instruction_path": str(rewritten_instruction_path),
            "remote_snapshot_path": str(self.logs_dir / "remote.app.snapshot.txt"),
        }
        parsed = self._parse_last_json(stdout)
        if parsed is not None:
            metadata["result"] = parsed
        elif stdout.strip():
            metadata["stdout_tail"] = stdout.strip().splitlines()[-1]
        if stderr.strip():
            metadata["stderr_tail"] = stderr.strip().splitlines()[-1]

        context.metadata = metadata

    def _required_env(self, name: str, must_be_file: bool = False) -> str:
        value = os.environ.get(name, "").strip()
        if not value:
            raise ValueError(f"missing required env var: {name}")
        if must_be_file and not Path(value).is_file():
            raise ValueError(f"{name} not found: {value}")
        return value

    def _resolve_model(self) -> str:
        if self.model_name:
            return self.model_name
        value = os.environ.get("ARKLOOP_MODEL", "").strip()
        if value:
            return value
        raise ValueError("missing required model: pass --model or set ARKLOOP_MODEL")

    def _rewrite_instruction(self, instruction: str, workspace_dir: Path) -> str:
        workspace = workspace_dir.as_posix()
        bridge_note = (
            "Execution note:\n"
            f"- The original container path `/app` is mirrored locally at `{workspace}`.\n"
            f"- Read and write files under `{workspace}` for this run.\n"
            f"- Any files you create there will be synchronized back to container `/app` after the run.\n\n"
        )
        return bridge_note + instruction.replace("/app", workspace)

    async def _sync_workspace_to_environment(
        self,
        environment: BaseEnvironment,
        workspace_dir: Path,
        remote_root: str,
    ) -> None:
        del workspace_dir
        result = await environment.exec(
            (
                "set -euo pipefail; "
                f"mkdir -p {remote_root}; "
                f"cp -a /logs/agent/workspace/. {remote_root}/"
            ),
            timeout_sec=30,
            user="root",
        )
        if result.return_code != 0:
            raise RuntimeError(
                "sync workspace to environment failed: "
                f"rc={result.return_code} stdout={result.stdout!r} stderr={result.stderr!r}"
            )

    def _parse_last_json(self, stdout: str) -> dict[str, object] | None:
        lines = [line.strip() for line in stdout.splitlines() if line.strip()]
        for line in reversed(lines):
            try:
                parsed = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(parsed, dict):
                return parsed
        return None
