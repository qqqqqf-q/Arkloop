# Desktop（桌面端）

## 定位

Desktop 是 **API + Worker + Bridge** 的**单进程**嵌入式发行版：**SQLite** 本地数据库，**不依赖**自建 Postgres/Redis 作为默认路径；与服务端共用 **Agent 管道思想**，但存储、事件总线、部分仓储实现不同（`_desktop.go`）。

## 技术栈事实

- **桌面壳**：**Electron**（版本以 `src/apps/desktop/package.json` 为准），**不是 Tauri**。
- **业务侧**：Go 运行时内嵌；**前端**为 `src/apps/desktop` 等应用入口，与 Web 共享 `@arkloop/shared` 等包的模式见仓库说明。
- 架构对照表（README）：**Desktop = Electron + Go**，原生应用 + 内嵌 sidecar。

## 配置与数据目录

以下以代码/文档约定为准，便于技术支持时对齐用户环境。

| 项 | 说明 |
|----|------|
| **用户级配置（Electron）** | `~/.arkloop/config.json`（配置目录 `~/.arkloop`） |
| **Go 数据根目录** | 默认 **`~/.arkloop`**；可用环境变量 **`ARKLOOP_DATA_DIR`** 覆盖整个数据根 |
| **SQLite 主库** | `{DataDir}/data.db` |
| **本地对象存储根** | `{DataDir}/storage`（实现对应 `StorageRoot(dataDir)`） |

若文档仅写「数据在 ~/.arkloop」，通常即指上述根目录；**具体文件**以 `data.db`、`storage/` 子路径为准。

## 与服务器版的差异（摘要）

| 维度 | Server 典型 | Desktop |
|------|-------------|---------|
| 数据库 | PostgreSQL 16 + 迁移 | **SQLite** |
| 任务/事件 | Redis、`pg_notify` 等 | **进程内 EventBus** 等桌面实现 |
| 桥梁 Bridge | 独立服务或模式相关 | **内嵌同进程**（desktop 描述） |
| Notebook 条目表 | `notebook_entries` | **`desktop_memory_entries`** |
| 部署 | compose / 自托管多容器 | **单应用安装包** |

## 设置与引导（UI）

向用户说明操作路径时，使用**短层级**，避免绑定易过期的截图文案：

1. **主界面左下角**可进入 **「设置」**。
2. **模型供应商**：在设置内查找 **「供应商」**或等价分组，添加 API Key、选择路由/模型（文案以当前版本界面为准）。
3. **频道 / 接入**：在设置内查找 **「接入」**、**「频道」**或 **Integrations** 类入口，选择 **Telegram** 等，按向导填写 **Bot Token**（来自 Telegram BotFather）及平台侧绑定步骤。
4. 若界面文案与上述不一致，**以用户屏幕上实际菜单名为准**，本帮助只提供**导航语义**（设置 → 分区 → 子项）。

## 记忆与频道在桌面上的行为

- 若在桌面侧启用了 Notebook/OpenViking 相关能力，身份与快照规则仍遵循 **Account / User / agent_id** 模型；频道场景下 **UserID 解析**与「群友 / owner」规则见 **Telegram 帮助文档**。
- **`arkloop_help`** 所带文档与 **Desktop 安装包版本**一致；若用户从源码自举桌面，以构建提交为准。

## 自动化更新

桌面发行说明（README）：可通过 **GitHub Releases** 渠道获取更新；具体应用内「检查更新」入口以当前 Electron 壳实现为准。
