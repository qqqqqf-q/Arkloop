---
title: "认证 (Auth)"
---
## 获取 Captcha 配置

```http
GET /v1/auth/captcha-config
```

无需认证。返回 Cloudflare Turnstile 站点配置（前端渲染 captcha 时使用）。

**响应**

```json
{
  "enabled": true,
  "site_key": "0x..."
}
```

---

## 检查用户是否存在

```http
POST /v1/auth/check
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `login` | `string` | 用户名或邮箱 |

**响应**

```json
{
  "exists": true
}
```

---

## 注册模式查询

```http
GET /v1/auth/registration-mode
```

**响应**

```json
{
  "mode": "open"
}
```

`mode` 取值：`open`（开放）、`invite_only`（仅邀请）、`disabled`（关闭）。

---

## 注册

```http
POST /v1/auth/register
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `login` | `string` | 是 | 用户名 |
| `password` | `string` | 是 | 密码 |
| `email` | `string` | 是 | 邮箱 |
| `invite_code` | `string` | 条件 | 邀请制模式下必填 |
| `locale` | `string` | 否 | 语言偏好 |
| `cf_turnstile_token` | `string` | 条件 | 启用 Turnstile 时必填 |

**响应**

```json
{
  "user_id": "...",
  "access_token": "...",
  "token_type": "bearer",
  "warning": null
}
```

成功时服务端会通过 `Set-Cookie` 下发 Refresh Token（HttpOnly Cookie：`arkloop_refresh_token`）。

---

## 登录

```http
POST /v1/auth/login
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `login` | `string` | 是 | 用户名或邮箱 |
| `password` | `string` | 是 | 密码 |
| `cf_turnstile_token` | `string` | 条件 | 启用 Turnstile 时必填 |

**响应**

```json
{
  "access_token": "...",
  "token_type": "bearer"
}
```

成功时服务端会通过 `Set-Cookie` 下发 Refresh Token（HttpOnly Cookie：`arkloop_refresh_token`）。

---

## 刷新 Token

```http
POST /v1/auth/refresh
```

**说明**

无需请求体。服务端从 HttpOnly Cookie `arkloop_refresh_token` 读取 Refresh Token 并轮换。

**响应** — 同登录响应格式（仅含 `access_token`、`token_type`），同时会更新 Refresh Token Cookie。

---

## 登出

```http
POST /v1/auth/logout
```

需携带 Bearer Token。使当前 Token 失效，并清理 Refresh Token Cookie。

**响应**

```json
{ "ok": true }
```

---

## 邮箱验证 — 发送验证邮件

```http
POST /v1/auth/email/verify/send
```

**无请求体**，使用当前登录用户的邮箱。

---

## 邮箱验证 — 确认

```http
POST /v1/auth/email/verify/confirm
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `token` | `string` | 是 |

---

## 邮箱 OTP 登录 — 发送 OTP

```http
POST /v1/auth/email/otp/send
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | `string` | 是 | 目标邮箱 |
| `cf_turnstile_token` | `string` | 条件 | |

---

## 邮箱 OTP 登录 — 验证 OTP

```http
POST /v1/auth/email/otp/verify
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `email` | `string` | 是 |
| `otp` | `string` | 是 |

**响应** — 同登录响应格式（含 `access_token`、`token_type`），同时会更新 Refresh Token Cookie。
