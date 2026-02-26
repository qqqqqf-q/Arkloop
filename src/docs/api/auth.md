# 认证 (Auth)

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
| `email` | `string` | 否 | 邮箱 |
| `invite_code` | `string` | 条件 | 邀请制模式下必填 |
| `locale` | `string` | 否 | 语言偏好 |
| `cf_turnstile_token` | `string` | 条件 | 启用 Turnstile 时必填 |

**响应**

```json
{
  "user_id": "...",
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "warning": null
}
```

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
  "refresh_token": "...",
  "token_type": "Bearer"
}
```

---

## 刷新 Token

```http
POST /v1/auth/refresh
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `refresh_token` | `string` | 是 |

**响应** — 同登录响应格式。

---

## 登出

```http
POST /v1/auth/logout
```

需携带 Bearer Token。使当前 Token 失效。

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

**响应** — 同登录响应格式（含 `access_token`、`refresh_token`）。
