---
title: Channel Settings
description: Configure messaging platform connections.
---

Channel settings configure how Arkloop connects to each external messaging platform. Each platform has its own card under Settings → Channels.

## Telegram

Connect to a Telegram bot account.

- **Bot Token** — issued by @BotFather. Identifies the bot.
- **Private chat access** — toggle whether the bot answers direct messages.
- **Group chat access** — toggle whether the bot answers in group chats.
- **Allowed Users** — list of Telegram user IDs that may talk to the bot in private. Empty list closes the channel.
- **Allowed Groups** — list of Telegram group/chat IDs that may invoke the bot.
- **Persona binding** — pick which persona handles incoming messages.
- **Heartbeat** — enable periodic synthetic turns and set the interval in minutes.
- **Webhook URL** — public URL Telegram posts updates to. Set this on a publicly reachable deployment.

Path: Settings → Channels → Telegram.

> [!NOTE] Screenshot placeholder.

## Discord

Connect to a Discord bot application.

- **Bot Token** — from the Discord Developer Portal.
- **Allowed Server IDs** — guilds the bot may operate in.
- **Allowed Channel IDs** — channels within those guilds the bot may post to.

Path: Settings → Channels → Discord.

## Feishu (Lark)

Connect to a Feishu or Lark custom app.

- **App ID** + **App Secret** — credentials from the Feishu / Lark developer console.
- **Verification Token** + **Encrypt Key** — used to verify and decrypt event callbacks.
- **Domain** — choose Feishu (feishu.cn) or Lark (larksuite.com).
- **Allowed Users** — Feishu user IDs permitted to message the bot.
- **Allowed Chats** — group chat IDs permitted to invoke the bot.
- **Trigger Keywords** — words that activate the bot inside a group. Empty list means every message triggers.

Path: Settings → Channels → Feishu.

## QQ Official

Connect to an official QQ Bot account.

- **App ID** + **Client Secret** — from the QQ Bot platform.
- **Allowed User OpenIDs** — QQ OpenIDs allowed in private chats.
- **Allowed Group OpenIDs** — QQ group OpenIDs allowed in group chats.

Path: Settings → Channels → QQ Official.

## QQ OneBot

Connect to a self-hosted OneBot v11 backend (go-cqhttp, NapCat, Lagrange, etc.).

- **WebSocket URL** — reverse or forward WebSocket endpoint of the OneBot service.
- **HTTP API URL** + **Token** — used for outbound API calls and authentication.
- **Bot name** — robot name used as a group-chat trigger keyword.
- **QR Login** — scan to log in the underlying QQ account.
- **Auto Re-login** — automatically reconnect when the session drops.
- **Allowed QQ Users** — QQ numbers allowed in private chats.
- **Allowed Groups** — QQ group numbers allowed in group chats.

Path: Settings → Channels → QQ OneBot.

## WeChat

Connect to a personal WeChat account via QR-code login.

- **QR Login** — scan with the WeChat mobile app to start the session.
- **Allowed Users** — WeChat user IDs allowed in private chats.
- **Allowed Groups** — WeChat group IDs allowed for group messages.

Path: Settings → Channels → WeChat.

> [!NOTE] Screenshot placeholder.
