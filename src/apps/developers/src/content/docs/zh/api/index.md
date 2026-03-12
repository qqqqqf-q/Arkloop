---
---

# API 概览

Arkloop API 是 RESTful 风格的 HTTP API，基础路径为 `/v1`。

## 基础 URL

| 环境 | 地址 |
|------|------|
| 本地开发（直连） | `http://127.0.0.1:19001` |
| 本地开发（经 Gateway） | `http://127.0.0.1:19000` |

## 认证

所有受保护端点需要在请求头中携带 Bearer Token：

```http
Authorization: Bearer <access_token>
```

Token 通过 [`POST /v1/auth/login`](./auth#登录) 获取。过期后使用 Refresh Token 换取新 Token（[`POST /v1/auth/refresh`](./auth#刷新-token)）。
登录后 Refresh Token 由服务端写入 HttpOnly Cookie；前端通过调用刷新接口获取新的 Access Token。

也可使用 API Key 认证（部分端点支持）：

```http
Authorization: Bearer al_...
```

## 错误响应格式

```json
{
  "error": "error.code",
  "message": "human-readable description",
  "trace_id": "...",
  "details": {}
}
```

常见错误码：

| HTTP 状态码 | 错误码 | 说明 |
|------------|--------|------|
| 400 | `validation.error` | 请求参数校验失败 |
| 401 | `auth.unauthorized` | 未认证或 Token 无效 |
| 403 | `auth.forbidden` | 权限不足 |
| 404 | `not_found` | 资源不存在 |
| 409 | `conflict` | 资源冲突（如重复创建） |
| 422 | `validation.error` | 请求体解析失败 |
| 429 | `rate_limit` | 请求频率超限 |
| 500 | `internal_error` | 服务内部错误 |

## 分页

支持分页的列表端点使用 cursor-based 分页：

```
GET /v1/threads?limit=20&before=<cursor>
```

## SSE（Server-Sent Events）

Run 执行过程通过 SSE 推送事件，详见 [Run 执行端点](./runs#sse-事件流)。

## 端点索引

### 认证与账户
- [认证 (Auth)](./auth) — 登录、注册、Token 刷新、邮箱验证
- [当前用户 (Me)](./me) — 个人信息、用量、积分、邀请码
- [API Keys](./api-keys) — 程序化访问密钥

### 核心资源
- [线程 (Threads)](./threads) — 会话管理
- [消息 (Messages)](./messages) — 消息读写
- [运行 (Runs)](./runs) — Agent Loop 执行
- [项目 (Projects)](./projects) — 会话分组

### 组织
- [组织 (Orgs)](./orgs) — 多租户管理、团队、邀请

### 配置
- [LLM Providers](./llm-providers) — 提供商账号、模型列表与 selector 前缀
- [MCP Configs](./mcp-configs) — MCP 服务器配置
- [Tool Providers](./tool-providers) — 工具后端与凭证配置
- [ASR Credentials](./asr-credentials) — 语音识别凭证

### 计费与权益
- [Credits & Usage](./credits) — 积分管理与用量统计
- [Subscriptions & Plans](./subscriptions) — 订阅与套餐
- [Entitlements](./entitlements) — 权益覆盖
- [API Keys](./api-keys) — API 密钥管理

### 通知与 Webhook
- [Notifications](./notifications) — 站内通知
- [Webhooks](./webhooks) — 事件推送

### 管理员 (Admin)
- [Admin 总览](./admin) — 平台管理端点汇总
- [审计日志](./audit-logs) — 操作审计
- [IP 规则](./ip-rules) — 访问控制
- [Feature Flags](./feature-flags) — 功能开关

### 系统
- [健康检查](./health) — `/healthz` `/readyz`
