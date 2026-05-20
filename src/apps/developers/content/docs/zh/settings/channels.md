---
title: 频道设置
description: 配置消息平台连接。
---

Channel 设置用于配置 Arkloop 与各个外部消息平台的连接。每个平台在 Settings → Channels 下有独立的卡片。

## Telegram

连接到一个 Telegram bot 账号。

- **Bot Token** —— 由 @BotFather 颁发，标识 bot。
- **Private chat access** —— 是否允许 bot 在私聊中回复。
- **Group chat access** —— 是否允许 bot 在群聊中回复。
- **Allowed Users** —— 允许私聊的 Telegram user ID 列表。空列表表示频道关闭。
- **Allowed Groups** —— 允许使用 bot 的 Telegram 群/会话 ID。
- **Persona binding** —— 选择处理此频道消息的 persona。
- **Heartbeat** —— 开启定时合成轮次，并设置间隔（分钟）。
- **Webhook URL** —— Telegram 推送更新的公网地址。在可被公网访问的部署上配置。

路径：Settings → Channels → Telegram。

> [!NOTE] Screenshot placeholder.

## Discord

连接到一个 Discord bot 应用。

- **Bot Token** —— 来自 Discord 开发者后台。
- **Allowed Server IDs** —— 允许 bot 工作的 guild。
- **Allowed Channel IDs** —— 上述 guild 内允许发消息的频道。

路径：Settings → Channels → Discord。

## Feishu (Lark)

连接到飞书或 Lark 的自建应用。

- **App ID** + **App Secret** —— 来自飞书 / Lark 开发者后台的凭据。
- **Verification Token** + **Encrypt Key** —— 用于校验和解密事件回调。
- **Domain** —— 选择 Feishu（feishu.cn）或 Lark（larksuite.com）。
- **Allowed Users** —— 允许与 bot 私聊的飞书 user ID。
- **Allowed Chats** —— 允许调用 bot 的群聊 ID。
- **Trigger Keywords** —— 群聊中触发 bot 的关键词。空列表代表每条消息都触发。

路径：Settings → Channels → Feishu。

## QQ Official

连接到 QQ 官方 Bot 账号。

- **App ID** + **Client Secret** —— 来自 QQ Bot 平台。
- **Allowed User OpenIDs** —— 允许私聊的 QQ OpenID。
- **Allowed Group OpenIDs** —— 允许群聊的 QQ 群 OpenID。

路径：Settings → Channels → QQ Official。

## QQ OneBot

连接到自托管的 OneBot v11 后端（go-cqhttp、NapCat、Lagrange 等）。

- **WebSocket URL** —— OneBot 服务的正向或反向 WebSocket 端点。
- **HTTP API URL** + **Token** —— 对外 API 调用地址及鉴权 token。
- **Bot 名称** —— 群聊中可作为触发关键词使用的机器人名称。
- **QR Login** —— 扫码登录底层 QQ 账号。
- **Auto Re-login** —— 会话掉线时自动重连。
- **Allowed QQ Users** —— 允许私聊的 QQ 号。
- **Allowed Groups** —— 允许群聊的 QQ 群号。

路径：Settings → Channels → QQ OneBot。

## WeChat

通过个人号扫码登录连接到微信。

- **QR Login** —— 用微信 App 扫码启动会话。
- **Allowed Users** —— 允许私聊的微信 user ID。
- **Allowed Groups** —— 允许群聊的微信群 ID。

路径：Settings → Channels → WeChat。

> [!NOTE] Screenshot placeholder.
