from __future__ import annotations

import logging

from packages.llm_gateway import ToolSpec
from services.api.provider_routed_runner import _merge_tool_specs


def test_merge_tool_specs_conflict_prefers_context_and_warns(caplog) -> None:
    base = (
        ToolSpec(
            name="echo",
            description="base",
            json_schema={"type": "object", "properties": {"a": {"type": "string"}}},
        ),
    )
    context = (
        ToolSpec(
            name="echo",
            description="context",
            json_schema={"type": "object", "properties": {"b": {"type": "string"}}},
        ),
    )

    with caplog.at_level(logging.WARNING):
        merged = _merge_tool_specs(base, context)

    assert merged == context
    assert any("tool spec 冲突" in record.message for record in caplog.records)

