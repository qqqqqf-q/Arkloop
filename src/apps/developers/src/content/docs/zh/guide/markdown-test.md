---
title: Markdown 渲染测试
description: 覆盖所有 Markdown 元素的渲染测试页，用于检查样式问题。
order: 99
---

# 一级标题 H1

## 二级标题 H2

### 三级标题 H3

#### 四级标题 H4

---

## 段落与内联样式

普通段落文本。Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.

**粗体文本**，*斜体文本*，***粗斜体***，~~删除线~~，`行内代码 inline code`。

这是一个包含 [超链接](https://arkloop.dev) 的段落。还有一个[相对链接](/docs/guide)。

---

## 代码块

普通文本代码块：

```
plain text code block
no language specified
```

```bash
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=.env
cd src/services/api && go run ./cmd/api
```

```powershell
$env:ARKLOOP_LOAD_DOTENV="1"
$env:ARKLOOP_DOTENV_FILE=".env"
cd src/services/api; go run ./cmd/api
```

```go
package main

import (
    "fmt"
    "net/http"
)

func handler(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "Hello, Arkloop!")
}

func main() {
    http.HandleFunc("/", handler)
    http.ListenAndServe(":19001", nil)
}
```

```typescript
interface PersonaConfig {
  persona_key: string;
  display_name: string;
  model?: string;
  budgets?: {
    temperature?: number;
    max_output_tokens?: number;
  };
}

async function createPersona(config: PersonaConfig): Promise<string> {
  const res = await fetch('/v1/personas', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  const data = await res.json();
  return data.id;
}
```

```json
{
  "persona_key": "default-assistant",
  "display_name": "默认助手",
  "model": "openai-main^gpt-4o",
  "budgets": {
    "temperature": 0.7
  },
  "budget": 100
}
```

```yaml
services:
  api:
    image: arkloop/api:latest
    ports:
      - "19001:19001"
    environment:
      ARKLOOP_DATABASE_URL: postgres://arkloop:secret@postgres/arkloop
```

```sql
SELECT u.id, u.email, COUNT(t.id) AS thread_count
FROM users u
LEFT JOIN threads t ON t.user_id = u.id
WHERE u.created_at > NOW() - INTERVAL '30 days'
GROUP BY u.id
ORDER BY thread_count DESC
LIMIT 20;
```

---

## 列表

### 无序列表

- 第一项
- 第二项
  - 嵌套项 A
  - 嵌套项 B
    - 深度嵌套
- 第三项

### 有序列表

1. 第一步：安装依赖
2. 第二步：配置环境变量
3. 第三步：启动服务
   1. 启动 PostgreSQL
   2. 启动 API
   3. 启动 Worker

### 任务列表

- [x] 完成数据库迁移
- [x] 实现认证模块
- [ ] 完成 WebSocket 支持
- [ ] 编写集成测试

---

## 表格

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ARKLOOP_API_GO_ADDR` | string | `127.0.0.1:19001` | API 监听地址 |
| `ARKLOOP_LOAD_DOTENV` | bool | `0` | 是否从 .env 加载 |
| `ARKLOOP_JWT_SECRET` | string | — | JWT 签名密钥 |
| `ARKLOOP_DATABASE_URL` | string | — | PostgreSQL 连接串 |

左对齐、居中、右对齐：

| 左对齐 | 居中 | 右对齐 |
|:-------|:----:|-------:|
| foo | bar | 1234 |
| longer text | center | 99 |

---

## 引用块

> 这是一段引用文本。引用块通常用于摘录或高亮重要信息。

> 多行引用：
> 
> 第一行内容。
> 
> 第二行内容，包含 `行内代码` 和 **粗体**。

嵌套引用：

> 外层引用
>
> > 内层引用
> >
> > 内层第二段

---

## 分隔线

上方内容

---

下方内容

***

另一条分隔线

---

## 图片

![Arkloop Logo](https://avatars.githubusercontent.com/u/0)

带标题的图片：

![示例图片](https://via.placeholder.com/800x200 "图片标题")

---

## 水平规则与混合内容

段落后跟代码块：

安装完依赖后运行以下命令：

```bash
pnpm install && pnpm dev
```

然后访问 `http://localhost:19080`。

代码块内有特殊字符：

```bash
# 注释行
echo "Hello, World!" > /tmp/test.txt
cat /tmp/test.txt | grep -E "^[A-Z]" | awk '{print $1}'
ls -la ~/.config/arkloop/
```

---

## 长代码块（测试横向滚动）

```typescript
export async function processAgentRun(runId: string, config: AgentConfig, context: RunContext): Promise<RunResult> {
  const { threadId, userId, orgId, projectId, maxIterations = 50, timeout = 300_000 } = context;
  const startedAt = Date.now();
  let iteration = 0;

  while (iteration < maxIterations && Date.now() - startedAt < timeout) {
    const step = await executeAgentStep(runId, config, context, iteration);
    if (step.done) return { runId, status: 'completed', iterations: iteration, output: step.output };
    iteration++;
  }

  return { runId, status: iteration >= maxIterations ? 'max_iterations' : 'timeout', iterations: iteration };
}
```
