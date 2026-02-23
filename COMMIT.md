
# Git 提交信息规范指南

为了保持代码历史的可读性、可追溯性以及自动化工具的兼容性，本项目遵循严格的提交信息规范。

## 1. 提交信息格式

每个提交信息应包含：**Header** (标题)、**Body** (正文，可选) 和 **Footer** (页脚，可选)。

```text
<type>(<scope>): <subject>

<body>

<footer>

```

### Header (必须)

* **type**: 用于说明提交的类型。
* **scope**: 用于说明提交影响的范围（可选，如：`auth`, `parser`, `api`）。
* **subject**: 提交信息的简短描述。

---

## 2. 提交类型 (Type)

必须使用以下类型之一：

| 类型 | 说明 |
| --- | --- |
| **feat** | 引入新功能 |
| **fix** | 修复 Bug |
| **docs** | 仅修改文档 (README, Wiki 等) |
| **style** | 不影响代码含义的修改 (空格、格式化、缺失分号等) |
| **refactor** | 代码重构 (既不是修复 Bug 也不是添加新功能) |
| **perf** | 提高性能的代码修改 |
| **test** | 添加缺失的测试或更正现有的测试 |
| **build** | 影响构建系统或外部依赖项的更改 (如：gulp, npm, cargo) |
| **ci** | 修改 CI 配置文件和脚本 |
| **chore** | 其他不修改源代码或测试文件的变动 |
| **revert** | 撤销之前的提交 |

---

## 3. 硬性限制与原则

### 禁止使用 Emoji

* **规范描述**：提交信息中严禁包含任何 Emoji 表情符号（如各类表情符号）。
* **理由**：确保在所有终端、文本编辑器以及低版本日志查看器中拥有最佳的兼容性与一致性，保持项目的专业性。

### 原子化提交 (Atomic Commits)

* **规范描述**：一个提交只做一件事。
* **要求**：
* 严禁将不相关的多个修复或多个功能合并在一个提交中。
* 如果发现一次性修改了过多文件，请考虑使用 `git add -p` 拆分暂存区。


* **好处**：方便回滚 (Revert)、减少合并冲突、提高 Code Review 的效率。

### 写作风格

* **使用祈使句**：使用 `fix` 而不是 `fixed`；使用 `add` 而不是 `added`。
* **首字母小写**：`subject` 部分首字母通常建议小写，且结尾**不加句号**。
* **长度限制**：Header 尽量控制在 **50 个字符**以内。

---

## 4. 示例

### 基础功能提交

```text
feat(parser): add support for nested json objects

```

### 带有正文和页脚的复杂提交

```text
fix(auth): correct token expiration logic

The previous logic used milliseconds instead of seconds, causing 
tokens to expire prematurely in production environments.

Close #123

```

### 代码重构 (无 Emoji)

```text
refactor(core): simplify redundant error handling loops

```

---

### 使用项目第一语言(例如简体中文或English或双语)
### 或使用最近GIT历史用的提交语言
## 5. 快速检查清单

1. [ ] 提交类型是否正确？
2. [ ] 是否移除了所有 Emoji？
3. [ ] 这个提交是否只包含一个逻辑变动（原子化）？
4. [ ] 标题是否使用了祈使句且没有句号？
5. [ ] 是否使用了正确的语言？

## 6.请勿提交共贡献人