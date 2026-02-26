# Admin 总览

管理员端点需要 `platform_admin` 权限。

## 仪表盘统计

```http
GET /v1/admin/dashboard
```

**响应**

```json
{
  "total_users": 1200,
  "active_users_30d": 380,
  "total_runs": 45000,
  "runs_today": 230,
  "total_input_tokens": 150000000,
  "total_output_tokens": 75000000,
  "total_cost_usd": 450.00,
  "active_orgs": 85
}
```

---

## 用户管理

### 列出用户

```http
GET /v1/admin/users
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `q` | `string` | 关键词搜索（用户名/邮箱） |
| `status` | `string` | 用户状态过滤 |
| `limit` | `int` | |
| `cursor` | `string` | 分页 cursor |

**响应**

```json
[
  {
    "id": "...",
    "login": "alice",
    "username": "Alice",
    "email": "alice@example.com",
    "status": "active",
    "avatar_url": null,
    "locale": "zh-CN",
    "timezone": "Asia/Shanghai",
    "last_login_at": "2024-01-15T10:00:00Z",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

### 获取用户详情

```http
GET /v1/admin/users/{user_id}
```

响应包含用户基本信息及所属组织列表。

### 更新用户

```http
PATCH /v1/admin/users/{user_id}
```

**请求体**（所有字段可选）

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | `string` | 用户状态 |
| `username` | `string` | |
| `email` | `string` | |
| `email_verified` | `bool` | |
| `locale` | `string` | |
| `timezone` | `string` | |

### 删除用户

```http
DELETE /v1/admin/users/{user_id}
```

响应 `204 No Content`。

---

## Run 详情

```http
GET /v1/admin/runs/{run_id}
```

**响应**（包含完整执行信息）

```json
{
  "run_id": "...",
  "org_id": "...",
  "thread_id": "...",
  "status": "completed",
  "model": "claude-3-5-sonnet-20241022",
  "skill_id": null,
  "provider_kind": "anthropic",
  "credential_name": "主 Anthropic 账号",
  "agent_config_name": "标准顾问",
  "duration_ms": 3200,
  "total_input_tokens": 1000,
  "total_output_tokens": 500,
  "total_cost_usd": 0.005,
  "created_at": "2024-01-01T00:00:00Z",
  "completed_at": "2024-01-01T00:00:03Z"
}
```

---

## 邮件配置

### 获取邮件状态

```http
GET /v1/admin/email/status
```

**响应**

```json
{
  "configured": true,
  "from": "noreply@example.com",
  "source": "db"
}
```

`source` 取值：`db`（数据库配置）、`env`（环境变量）、`none`（未配置）。

### 获取邮件配置

```http
GET /v1/admin/email/config
```

**响应**（不返回密码明文）

```json
{
  "from": "noreply@example.com",
  "smtp_host": "smtp.example.com",
  "smtp_port": 587,
  "smtp_user": "noreply@example.com",
  "smtp_pass_set": true,
  "smtp_tls_mode": "starttls"
}
```

### 更新邮件配置

```http
PUT /v1/admin/email/config
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `from` | `string` | 发件人地址 |
| `smtp_host` | `string` | |
| `smtp_port` | `int` | |
| `smtp_user` | `string` | |
| `smtp_pass` | `string` | |
| `smtp_tls_mode` | `string` | `none`/`starttls`/`tls` |

响应 `204 No Content`。

### 发送测试邮件

```http
POST /v1/admin/email/test
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `to` | `string` | 是 |

响应 `204 No Content`。

---

## 网关配置

### 获取网关配置

```http
GET /v1/admin/gateway-config
```

**响应**

```json
{
  "ip_mode": "proxy",
  "trusted_cidrs": ["10.0.0.0/8"],
  "risk_reject_threshold": 0.8
}
```

### 更新网关配置

```http
PUT /v1/admin/gateway-config
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `ip_mode` | `string` | `direct`/`proxy`/`cdn` |
| `trusted_cidrs` | `[]string` | 可信 CIDR 列表 |
| `risk_reject_threshold` | `float64` | 风险拒绝阈值（0-1） |

---

## 访问日志

```http
GET /v1/admin/access-log
```

基于 Redis Stream 的实时访问日志查询。

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | 每页数量 |
| `before` | `string` | cursor（向前翻页） |
| `since` | `string` | 从此 ID 开始 |
| `method` | `string` | HTTP 方法过滤 |
| `path` | `string` | 路径前缀过滤 |
| `ip` | `string` | IP 过滤 |
| `country` | `string` | 国家代码过滤 |
| `risk_min` | `float64` | 最低风险分数 |
| `ua_type` | `string` | UA 类型过滤 |

**响应**

```json
{
  "data": [
    {
      "id": "...",
      "timestamp": "2024-01-01T00:00:00Z",
      "trace_id": "...",
      "method": "POST",
      "path": "/v1/threads",
      "status_code": 201,
      "duration_ms": 45,
      "client_ip": "1.2.3.4",
      "country": "CN",
      "city": "Beijing",
      "user_agent": "Mozilla/5.0...",
      "ua_type": "browser",
      "risk_score": 0.1,
      "identity_type": "user",
      "org_id": "...",
      "user_id": "...",
      "username": "alice"
    }
  ],
  "has_more": true,
  "next_before": "..."
}
```

---

## 邀请码管理

### 列出邀请码

```http
GET /v1/admin/invite-codes
```

**查询参数**：`limit`、`q`（关键词）、`cursor`

**响应**

```json
[
  {
    "id": "...",
    "user_id": "...",
    "code": "ABC123",
    "max_uses": 10,
    "use_count": 3,
    "is_active": true,
    "created_at": "...",
    "user_login": "alice",
    "user_email": "alice@example.com"
  }
]
```

### 获取邀请码

```http
GET /v1/admin/invite-codes/{id}
```

### 更新邀请码

```http
PATCH /v1/admin/invite-codes/{id}
```

**请求体**

| 字段 | 类型 |
|------|------|
| `max_uses` | `int` |
| `is_active` | `bool` |

---

## 推荐关系

### 列出推荐记录

```http
GET /v1/admin/referrals
```

**查询参数**：`inviter_user_id`、`limit`、`cursor`

### 获取推荐树

```http
GET /v1/admin/referrals/tree?user_id={user_id}
```

**响应**

```json
[
  {
    "user_id": "...",
    "login": "alice",
    "inviter_id": null,
    "depth": 0,
    "created_at": "..."
  }
]
```

---

## 兑换码管理

### 批量创建兑换码

```http
POST /v1/admin/redemption-codes/batch
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `count` | `int` | 是 | 生成数量 |
| `type` | `string` | 是 | 类型（如 `credits`） |
| `value` | `string` | 是 | 兑换价值 |
| `max_uses` | `int` | 否 | 每码最大使用次数 |
| `expires_at` | `string` | 否 | 过期时间（RFC3339） |
| `batch_id` | `string` | 否 | 批次标识 |

### 列出兑换码

```http
GET /v1/admin/redemption-codes
```

**查询参数**：`limit`、`q`、`type`、`cursor`

### 更新兑换码

```http
PATCH /v1/admin/redemption-codes/{id}
```

**请求体**

| 字段 | 类型 |
|------|------|
| `is_active` | `bool` |
