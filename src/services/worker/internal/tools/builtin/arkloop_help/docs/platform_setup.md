# 平台配置引导

## 配置 Web Search

Web Search 让 Agent 能够联网搜索实时信息。通过 `platform_manage` 工具的 `add_tool_provider` 动作配置。

可选 Provider：

- **Basic**：内置浏览器搜索，无需 API Key。
  调用方式：`add_tool_provider`，参数 `group: "web_search"`, `provider: "basic"`

- **Tavily**：更高质量的搜索结果，需要 API Key（从 https://tavily.com 获取）。
  调用方式：`add_tool_provider`，参数 `group: "web_search"`, `provider: "tavily"`, `config: {"api_key": "tvly-..."}`

## 配置 Web Fetch

Web Fetch 让 Agent 能够获取并解析网页内容。通过 `platform_manage` 的 `add_tool_provider` 配置。

可选 Provider：

- **Basic**：内置抓取，无需 API Key，内容提取质量一般。
  调用方式：`add_tool_provider`，参数 `group: "web_fetch"`, `provider: "basic"`

- **Jina**：高质量内容提取，需要 API Key（从 https://jina.ai 获取）。
  调用方式：`add_tool_provider`，参数 `group: "web_fetch"`, `provider: "jina"`, `config: {"api_key": "..."}`

## 配置 Memory

Arkloop 的记忆系统由两个**独立**子系统组成，可单独或同时启用。所有记忆绑定在 bot owner（User）上，切换 persona 不改变记忆归属。

### 子系统一：Notebook（默认）

内置的纯文本笔记系统，无外部服务，无需配置 Provider。

- 默认启用；仅当 `ARKLOOP_MEMORY_ENABLED=false` 时整体关闭记忆
- Agent 将获得 `notebook_read`、`notebook_write`、`notebook_edit`、`notebook_forget` 四个工具
- 数据存储在本地 SQLite 的 `desktop_memory_entries` 表

### 子系统二：Nowledge（可选语义记忆）

基于外部 Nowledge 服务的语义记忆，提供语义检索、working memory 注入和会话提炼。

启用方式：

- 保持记忆启用（`ARKLOOP_MEMORY_ENABLED` 不为 `false`）
- 通过 `platform_manage` 配置 Provider：
  调用方式：`add_tool_provider`，参数 `group: "memory"`, `provider: "nowledge"`, `config: {"base_url": "http://your-nowledge-instance", "api_key": "..."}`
- 也可直接设置环境变量 `ARKLOOP_NOWLEDGE_BASE_URL` / `ARKLOOP_NOWLEDGE_API_KEY`
- Agent 将获得 `memory_search`、`memory_read`、`memory_write`、`memory_forget`、`memory_list`、`memory_status`、`memory_context`、`memory_timeline`、`memory_connections`、`memory_thread_search`、`memory_thread_fetch` 等工具

### 双系统叠加

两个子系统都启用时，Agent 同时拥有 Notebook 和 Nowledge 的全部工具；系统 prompt 注入 `<notebook>` 与 `<working-memory>` 块，运行时尾部追加召回记忆块。

### 运行模式总览

| 模式 | 条件 | 注入内容 | 可用工具 |
|------|------|---------|---------|
| disabled | `ARKLOOP_MEMORY_ENABLED=false` | 无 | 无 |
| notebook-only（默认） | 启用，未配置 Nowledge | `<notebook>` | notebook_read, notebook_write, notebook_edit, notebook_forget |
| notebook + nowledge | 启用，配置了 Nowledge | `<notebook>` + `<working-memory>` + 召回块 | notebook_* + memory_* |

## 配置 Read（文件读取增强）

Read 增强 Agent 的文件读取能力。

- **MiniMax**：增强型文件读取，需要 API Key（从 https://minimax.io 获取）。
  调用方式：`add_tool_provider`，参数 `group: "read"`, `provider: "minimax"`, `config: {"api_key": "..."}`

## 配置 LLM Provider

管理 LLM 模型提供商。**BYOK**：全部使用用户自己的 API Key。

- 查看已有 Provider 和模型：使用 `list_providers` 动作
- 添加新 Provider：使用 `add_provider` 动作，参数 `name`（自定义名称）、`provider`（提供商标识）、`api_key`、`base_url`（可选）
- 更新 Provider：`update_provider`，参数 `id`（Provider UUID）、以及要修改的字段
- 删除 Provider：`delete_provider`，参数 `id`（Provider UUID）
- 查看模型列表：`list_models`，参数 `provider_id`
- 配置模型参数：`configure_model`，参数 `provider_id`、`model_id`、`config`（可选配置对象）

支持的 Provider 标识：

- **`openai`**：OpenAI 及任何 **OpenAI 兼容接口**（DeepSeek、Moonshot、Qwen 等第三方兼容服务均用 `openai` + 自定义 `base_url` 接入）
- **`anthropic`**：Anthropic Messages
- **`gemini`**：Google Gemini
- 本地 CLI Provider：`claude_code_local`、`codex_local`（本机已登录的 CLI 工具）

## 如何调用 platform_manage

`platform_manage` 是平台管理的核心工具，所有配置操作通过它完成。

参数说明：

- **`action`**（必填，字符串）：操作类型，可用值：
  `get_settings`、`set_setting`、`configure_email`、`test_email`、`configure_smtp`、
  `configure_captcha`、`configure_registration`、`configure_gateway`、`update_styles`、
  `list_providers`、`add_provider`、`update_provider`、`delete_provider`、
  `list_models`、`configure_model`、`list_agents`、`create_agent`、`update_agent`、
  `delete_agent`、`get_agent`、`list_skills`、`install_skill_market`、`install_skill_github`、
  `remove_skill`、`list_mcp_installs`、`add_mcp_install`、`update_mcp_install`、
  `delete_mcp_install`、`check_mcp_install`、`list_workspace_mcp_enablements`、
  `set_workspace_mcp_enablement`、`list_tool_providers`、`add_tool_provider`、
  `update_tool_provider`、`list_ip_rules`、`add_ip_rule`、`delete_ip_rule`、
  `list_api_keys`、`create_api_key`、`revoke_api_key`、
  `get_status`、`list_modules`、`install_module`、`trigger_update`

- **`params`**（可选，对象）：具体操作的参数，键值对形式，不同 action 所需参数不同。

`add_tool_provider` 调用示例：
```json
{
  "action": "add_tool_provider",
  "params": {
    "group": "web_search",
    "provider": "tavily",
    "config": {
      "api_key": "tvly-..."
    }
  }
}
```
