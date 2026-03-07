---
---

# Persona YAML Reference

Persona 定义位于 `src/personas/<id>/` 目录下，每个目录包含：

- `persona.yaml` (必需) -- 人格配置
- `prompt.md` (可选) -- 系统提示词
- `agent.lua` (可选) -- 自定义 agent loop 脚本 (executor_type: agent.lua)

## 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | string | yes | 唯一标识，与目录名一致 |
| `version` | string | no | 版本号，默认 `"1"` |
| `title` | string | yes | 展示名称 (Console / API 响应中使用) |
| `description` | string | no | 简要描述 |
| `tool_allowlist` | string[] | no | 允许使用的工具，空 = 全部允许 |
| `tool_denylist` | string[] | no | 禁止使用的工具 |
| `executor_type` | string | no | 执行引擎，默认 `agent.simple`。自定义脚本用 `agent.lua` |
| `executor_config` | map | no | 引擎配置。`agent.lua` 需要 `script_file: agent.lua` |
| `budgets` | map | no | `max_iterations`, `max_output_tokens`, `temperature` |
| `agent_config` | string | no | 模型路由配置名 |
| `title_summarize` | map | no | 对话标题生成：`prompt` + `max_tokens` |
| `user_selectable` | bool | no | 是否出现在 Web 前端的人格选择器中。默认 `false` |
| `selector_name` | string | no | 选择器中显示的标签。省略时回退到 `title` |
| `selector_order` | int | no | 选择器排序权重，数值小的排前面。省略默认 `99` |

## 前端选择器

Web 前端在挂载时请求 `GET /v1/personas`，过滤 `user_selectable: true` 的条目，按 `selector_order` 升序排列后渲染选择器下拉菜单。

修改 YAML 后需要**重启 API 服务**才能生效 (persona 在 API 启动时从文件系统一次性加载)。

如果 API 不可达，选择器按钮仅显示当前 persona key 原文，下拉菜单不可用。

## 示例

```yaml
id: normal
version: "1"
title: Normal
description: 通用对话模式
budgets:
  max_iterations: 99999
  max_output_tokens: 20480
  temperature: 1.0
agent_config: openrouter-oss-120b-groq

# 前端选择器
user_selectable: true
selector_name: Normal
selector_order: 1
```
