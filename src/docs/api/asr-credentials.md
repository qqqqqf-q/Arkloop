# ASR Credentials

管理语音识别（ASR）服务凭证。所有端点需要 Bearer Token 认证。

## 创建凭证

```http
POST /v1/asr-credentials
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | 显示名称 |
| `provider` | `string` | 是 | `groq` 或 `openai` |
| `api_key` | `string` | 是 | API 密钥（加密存储） |
| `base_url` | `string` | 否 | 自定义端点 |
| `model` | `string` | 是 | 模型名称（如 `whisper-large-v3`） |
| `is_default` | `bool` | 否 | 是否为默认凭证 |
| `scope` | `string` | 否 | `org`（默认）或 `platform`（需 platform_admin） |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "scope": "org",
  "provider": "groq",
  "name": "Groq ASR",
  "key_prefix": "gsk_xxxx",
  "base_url": null,
  "model": "whisper-large-v3",
  "is_default": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 列出凭证

```http
GET /v1/asr-credentials
```

---

## 删除凭证

```http
DELETE /v1/asr-credentials/{id}
```

---

## 设为默认

```http
POST /v1/asr-credentials/{id}/set-default
```

---

## 语音转文字

```http
POST /v1/asr/transcribe
```

`multipart/form-data` 上传音频文件。

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `file` | `file` | 是 | 音频文件 |
| `language` | `string` | 否 | 语言代码（如 `zh`） |

**响应**

```json
{
  "text": "转录的文本内容..."
}
```
