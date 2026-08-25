# Desktop（桌面端）

## 定位

Desktop 是 Arkloop 的主要形态：**Electron 壳 + 内嵌 Go 运行时**（API + Worker + Bridge 单进程），**SQLite** 本地数据库，无需任何外部基础设施。安装包打开即用。

## 技术栈事实

- **桌面壳**：**Electron**（版本以 `src/apps/desktop/package.json` 为准），**不是 Tauri**。
- **业务侧**：Go 运行时以库形式内嵌（`src/services/desktop`）；界面为打包后的 `src/apps/web`，与 Web 共享 `@arkloop/shared`。

## 配置与数据目录

| 项 | 说明 |
|----|------|
| **数据根目录** | 默认 **`~/.arkloop`**；环境变量 **`ARKLOOP_DATA_DIR`** 可覆盖 |
| **SQLite 主库** | `{DataDir}/data.db`（启动时自动迁移到最新 schema） |
| **本地对象存储根** | `{DataDir}/storage` |

若只说「数据在 ~/.arkloop」，通常即指上述根目录；具体文件以 `data.db`、`storage/` 子路径为准。

## 认证（单用户）

Desktop 是**单用户**应用：首次启动写入固定种子用户，本机请求由 desktop token 自动注入完成认证，没有注册/登录流程。可选设置**本机密码**——`ark web --host 0.0.0.0` 等对外暴露场景下用于密码登录；密码可通过 `ark web reset-password --password <新密码>` 重置。

## 设置与引导（UI）

向用户说明操作路径时，使用**短层级**，避免绑定易过期的截图文案：

1. **主界面左下角**可进入 **「设置」**。
2. **模型供应商**：在设置内查找 **「供应商」** 或等价分组，添加 API Key（自带密钥，BYOK）、选择路由/模型。
3. **频道 / 接入**：在设置内查找 **「接入」**、**「频道」** 或 Integrations 类入口，选择 Telegram 等平台，按向导填写 **Bot Token**（Telegram 来自 BotFather）并完成绑定；桌面端 Telegram 默认走 getUpdates 长轮询，无需公网 webhook。
4. **记忆**：Notebook 默认启用；Nowledge 语义记忆需配置外部服务地址后启用（见 `architecture` 文档记忆小节）。
5. **模块**：sandbox（Docker 代码执行）、SearXNG（搜索）、Firecrawl（抓取）为可选模块，在设置内安装/管理，由 Bridge 以 Docker 容器运行。
6. 若界面文案与上述不一致，**以用户屏幕上实际菜单名为准**，本帮助只提供**导航语义**（设置 → 分区 → 子项）。

## ark CLI

首次启动时 Desktop 可安装 **`ark`** 命令行。安装后无需打开桌面窗口即可启动同一套本地运行时：

```bash
ark web                 # 启动运行时并打开 Web 界面（默认 web 19080 / api 19001 / bridge 19003）
ark web --host 0.0.0.0 --no-open   # 对外提供（headless 服务器场景）
ark web reset-password --password <新密码>
ark status              # 查看本地运行时状态
```

CLI 与 Desktop 共用同一个数据目录（`~/.arkloop`），看到的是同一份数据。

## 自动化更新

桌面端通过 **GitHub Releases** 通道自动更新（electron-updater）；应用内「检查更新」入口以当前版本为准。

## 与本帮助的关系

`arkloop_help` 所带文档与 **Desktop 安装包版本**一致；若用户从源码自举桌面，以构建提交为准。
