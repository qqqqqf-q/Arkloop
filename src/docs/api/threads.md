# 线程 (Threads)

所有端点需要 Bearer Token（或 API Key）认证。

## 创建线程

```http
POST /v1/threads
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `title` | `string` | 否 | 线程标题 |
| `is_private` | `bool` | 否 | 是否私有，默认 `false` |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "created_by_user_id": "...",
  "title": "我的第一个对话",
  "project_id": null,
  "created_at": "2024-01-01T00:00:00Z",
  "active_run_id": null,
  "is_private": false
}
```

---

## 列出线程

```http
GET /v1/threads
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | 每页数量 |
| `before` | `string` | cursor，用于翻页 |
| `project_id` | `string` | 按项目过滤 |

---

## 搜索线程

```http
GET /v1/threads/search
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `q` | `string` | 搜索关键词 |
| `limit` | `int` | 每页数量 |

---

## 已收藏线程

```http
GET /v1/threads/starred
```

---

## 获取线程详情

```http
GET /v1/threads/{thread_id}
```

---

## 更新线程

```http
PATCH /v1/threads/{thread_id}
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `title` | `string \| null` | 标题（`null` 清空） |
| `project_id` | `string \| null` | 关联项目 |
| `agent_config_id` | `string \| null` | 关联 Agent 配置 |

---

## 删除线程

```http
DELETE /v1/threads/{thread_id}
```

---

## 收藏 / 取消收藏线程

```http
POST   /v1/threads/{thread_id}/star
DELETE /v1/threads/{thread_id}/star
```
