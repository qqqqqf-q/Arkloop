package accountapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ChannelCommandDeps groups repository dependencies for DispatchChannelCommand.
type ChannelCommandDeps struct {
	ChannelIdentitiesRepo    *data.ChannelIdentitiesRepository
	ChannelDMThreadsRepo     *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo  *data.ChannelGroupThreadsRepository
	PersonasRepo             *data.PersonasRepository
	RunEventRepo             *data.RunEventRepository
	ChannelBindCodesRepo     *data.ChannelBindCodesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	ThreadRepo               *data.ThreadRepository
}

// ChannelCommandResolver provides channel-specific operations needed by DispatchChannelCommand.
type ChannelCommandResolver struct {
	// ResolveThreadID resolves the thread ID for this channel.
	// Takes personaID + projectID, returns threadID.
	ResolveThreadID func(ctx context.Context, tx pgx.Tx, personaID, projectID uuid.UUID, isPrivate bool, platformChatID string) (uuid.UUID, error)

	// ResolveHeartbeatIdentity resolves the identity used for heartbeat config.
	// For group chats, this should be the group identity. For private chats, use the user identity.
	// If nil and in a group chat, the user identity is used as-is.
	ResolveHeartbeatIdentity func(ctx context.Context, tx pgx.Tx) (*data.ChannelIdentity, error)

	// IsGroupAdmin checks if the sender is a group admin (for /new /stop in groups).
	// nil = skip admin check.
	IsGroupAdmin func(ctx context.Context) bool

	// IsBoundAdmin checks if the sender is linked to this channel.
	// In IM channel groups, a linked channel identity is an Arkloop channel admin.
	IsBoundAdmin func(ctx context.Context) bool

	// ResolveStartPayload extracts the /start deep link payload (e.g., "bind_xxx").
	// Return "" for channels without deep link support.
	ResolveStartPayload func() string

	// BindCode extracts the bind code from /bind command arguments.
	// Return "" if no bind code present.
	BindCode func() string
}

// DispatchChannelCommand handles command dispatch for all text-based IM channels.
func DispatchChannelCommand(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	persona data.Persona,
	identity data.ChannelIdentity,
	commandText string,
	isPrivate bool,
	platformChatID string,
	entSvc *entitlement.Service,
	resolver ChannelCommandResolver,
	deps ChannelCommandDeps,
	channelLabel string,
) (handled bool, reply *CommandReply, err error) {
	cmd, ok := slashCommandBase(strings.TrimSpace(commandText), "")
	if !ok {
		return false, nil, nil
	}
	if !isPrivate && channelCommandRequiresAdmin(cmd) && !resolveChannelCommandAdmin(ctx, resolver) {
		return true, &CommandReply{Text: "无权限。"}, nil
	}

	threadProjectID := derefUUID(persona.ProjectID)
	if threadProjectID == uuid.Nil {
		ownerUserID := uuid.Nil
		if ch.OwnerUserID != nil {
			ownerUserID = *ch.OwnerUserID
		}
		if ownerUserID == uuid.Nil && identity.UserID != nil {
			ownerUserID = *identity.UserID
		}
		if ownerUserID != uuid.Nil {
			if pid, err := deps.PersonasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, ownerUserID); err == nil {
				threadProjectID = pid
			}
		}
	}

	resolveThreadID := func() (uuid.UUID, error) {
		if threadProjectID == uuid.Nil {
			return uuid.Nil, fmt.Errorf("cannot resolve project for persona %s", persona.ID)
		}
		if resolver.ResolveThreadID == nil {
			return uuid.Nil, fmt.Errorf("thread resolution not configured")
		}
		return resolver.ResolveThreadID(ctx, tx, persona.ID, threadProjectID, isPrivate, platformChatID)
	}

	switch {
	case cmd == "/model" || strings.HasPrefix(cmd, "/think"):
		threadID, err := resolveThreadID()
		if err != nil {
			return true, nil, err
		}
		reply, err = handlePreferenceCommand(ctx, tx, ch.AccountID, threadID, strings.TrimSpace(commandText), entSvc)
		return true, reply, err

	case strings.HasPrefix(cmd, "/heartbeat"):
		if isPrivate {
			return true, &CommandReply{Text: "请在群聊中使用 /heartbeat。"}, nil
		}
		threadID, err := resolveThreadID()
		if err != nil {
			return true, nil, err
		}
		heartbeatIdentity := identity
		if !isPrivate && resolver.ResolveHeartbeatIdentity != nil {
			gi, err := resolver.ResolveHeartbeatIdentity(ctx, tx)
			if err != nil {
				return true, nil, err
			}
			if gi != nil {
				heartbeatIdentity = *gi
			}
		}
		replyText, err := handleTelegramHeartbeatCommand(
			ctx, tx,
			ch.ID, ch.AccountID, ch.PersonaID,
			threadID,
			heartbeatIdentity,
			strings.TrimSpace(commandText),
			deps.ChannelIdentitiesRepo,
			deps.PersonasRepo,
			entSvc,
		)
		if err != nil {
			return true, nil, err
		}
		return true, &CommandReply{Text: replyText}, nil

	case cmd == "/new":
		if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
			return true, &CommandReply{Text: "当前会话未配置 persona。"}, nil
		}
		if isPrivate {
			if deps.ChannelDMThreadsRepo != nil {
				if err := deps.ChannelDMThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, identity.ID, *ch.PersonaID, ""); err != nil {
					slog.WarnContext(ctx, "channel_command_new_delete_dm_failed", "error", err, "channel_id", ch.ID, "identity_id", identity.ID)
					return true, &CommandReply{Text: "操作失败。"}, nil
				}
			}
		} else {
			if deps.ChannelGroupThreadsRepo != nil {
				if err := deps.ChannelGroupThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, platformChatID, *ch.PersonaID); err != nil {
					slog.WarnContext(ctx, "channel_command_new_delete_group_failed", "error", err, "channel_id", ch.ID, "platform_chat_id", platformChatID)
					return true, &CommandReply{Text: "操作失败。"}, nil
				}
			}
		}
		modelName, personaName := resolveNewSessionContext(ctx, tx, ch, deps)
		return true, &CommandReply{Text: RenderNewSessionText(modelName, personaName)}, nil

	case cmd == "/stop":
		if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
			return true, &CommandReply{Text: "当前没有运行中的任务。"}, nil
		}
		threadID, err := resolveThreadID()
		if err != nil {
			return true, &CommandReply{Text: "当前没有运行中的任务。"}, nil
		}
		activeRun, _ := deps.RunEventRepo.WithTx(tx).GetActiveRootRunForThread(ctx, threadID)
		if activeRun == nil {
			return true, &CommandReply{Text: "当前没有运行中的任务。"}, nil
		}
		if _, err := deps.RunEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, identity.UserID, "", 0, nil); err != nil {
			return true, nil, err
		}
		return true, &CommandReply{Text: "已请求停止当前任务。", CancelRunID: activeRun.ID}, nil

	case cmd == "/help":
		return true, &CommandReply{Text: channelCommandHelpText(isPrivate)}, nil

	case cmd == "/start":
		if resolver.ResolveStartPayload != nil {
			payload := resolver.ResolveStartPayload()
			if strings.HasPrefix(payload, "bind_") {
				code := strings.TrimPrefix(payload, "bind_")
				replyText, err := bindChannelIdentity(ctx, tx, &ch, identity, code, channelLabel, deps.ChannelBindCodesRepo, deps.ChannelIdentitiesRepo, deps.ChannelIdentityLinksRepo, deps.ChannelDMThreadsRepo, deps.ThreadRepo)
				if err != nil {
					return true, nil, err
				}
				return true, &CommandReply{Text: replyText}, nil
			}
		}
		return true, &CommandReply{Text: "已连接 Arkloop\n\n使用 /bind <code> 绑定账号\n私聊直接发消息开始对话，/new 开启新会话\n群内 @bot 触发对话，管理员可用 /new 重置会话"}, nil

	case cmd == "/bind":
		code := ""
		if resolver.BindCode != nil {
			code = resolver.BindCode()
		}
		if code == "" {
			return true, &CommandReply{Text: "用法：/bind <code>"}, nil
		}
		replyText, err := bindChannelIdentity(ctx, tx, &ch, identity, code, channelLabel, deps.ChannelBindCodesRepo, deps.ChannelIdentitiesRepo, deps.ChannelIdentityLinksRepo, deps.ChannelDMThreadsRepo, deps.ThreadRepo)
		if err != nil {
			return true, nil, err
		}
		return true, &CommandReply{Text: replyText}, nil

	case cmd == "/reset":
		if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
			return true, &CommandReply{Text: "当前会话未配置 persona。"}, nil
		}
		if isPrivate {
			if deps.ChannelDMThreadsRepo != nil {
				if err := deps.ChannelDMThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, identity.ID, *ch.PersonaID, ""); err != nil {
					slog.WarnContext(ctx, "channel_command_reset_delete_dm_failed", "error", err, "channel_id", ch.ID)
					return true, &CommandReply{Text: "操作失败。"}, nil
				}
			}
		} else {
			if deps.ChannelGroupThreadsRepo != nil {
				if err := deps.ChannelGroupThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, platformChatID, *ch.PersonaID); err != nil {
					slog.WarnContext(ctx, "channel_command_reset_delete_group_failed", "error", err, "channel_id", ch.ID)
					return true, &CommandReply{Text: "操作失败。"}, nil
				}
			}
		}
		modelName, personaName := resolveNewSessionContext(ctx, tx, ch, deps)
		return true, &CommandReply{Text: RenderNewSessionText(modelName, personaName)}, nil

	case cmd == "/status":
		threadID, resolveErr := resolveThreadID()
		preferredModel := ""
		reasoningMode := ""
		if resolveErr == nil && threadID != uuid.Nil {
			var err error
			preferredModel, reasoningMode, _, err = getInboundThreadModelPreference(ctx, tx, threadID)
			if err != nil {
				return true, nil, err
			}
		}
		modelDisplay := ""
		if strings.TrimSpace(preferredModel) != "" {
			modelDisplay = preferredModel
		}
		personaName := resolveChannelPersonaName(ctx, tx, ch, deps)
		runStatus := "空闲"
		if resolveErr == nil && threadID != uuid.Nil {
			activeRun, _ := deps.RunEventRepo.WithTx(tx).GetActiveRootRunForThread(ctx, threadID)
			if activeRun != nil {
				runStatus = "运行中"
			}
		}
		return true, &CommandReply{Text: RenderStatusText(modelDisplay, reasoningMode, personaName, runStatus)}, nil

	case cmd == "/models":
		allowUserScoped, err := resolveByokEnabled(ctx, entSvc, ch.AccountID)
		if err != nil {
			return true, nil, err
		}
		candidates, err := loadModelSelectorCandidates(ctx, tx, ch.AccountID)
		if err != nil {
			return true, nil, err
		}
		threadID, err := resolveThreadID()
		preferredModel := ""
		if err == nil && threadID != uuid.Nil {
			preferredModel, _, _, _ = getInboundThreadModelPreference(ctx, tx, threadID)
		}
		pickerData := GroupCandidatesByProvider(candidates, preferredModel, allowUserScoped)
		if len(pickerData.Providers) == 0 {
			return true, &CommandReply{Text: "暂无可用模型。"}, nil
		}
		return true, &CommandReply{
			Text:        RenderModelPickerText(pickerData),
			Interactive: pickerData,
		}, nil

	case cmd == "/persona":
		if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
			return true, &CommandReply{Text: "当前会话未配置 persona。"}, nil
		}
		if !isPrivate && identity.UserID == nil {
			return true, &CommandReply{Text: "无权限。"}, nil
		}
		currentPersona, err := deps.PersonasRepo.GetByIDForAccount(ctx, ch.AccountID, *ch.PersonaID)
		if err != nil {
			return true, nil, err
		}
		if currentPersona == nil || currentPersona.ProjectID == nil {
			return true, &CommandReply{Text: "当前会话未配置 persona。"}, nil
		}
		personas, err := deps.PersonasRepo.ListActiveByProject(ctx, *currentPersona.ProjectID)
		if err != nil {
			return true, nil, err
		}
		currentPersonaID := uuid.Nil
		if ch.PersonaID != nil {
			currentPersonaID = *ch.PersonaID
		}
		pickerData := BuildPersonaPickerData(personas, currentPersonaID, currentPersona.DisplayName)
		if len(pickerData.Personas) == 0 {
			return true, &CommandReply{Text: "没有可切换的 persona。"}, nil
		}
		return true, &CommandReply{
			Text:        RenderPersonaPickerText(pickerData),
			Interactive: pickerData,
		}, nil

	default:
		return false, nil, nil
	}
}

// handlePreferenceCommand 处理 /model 和 /think 偏好命令，返回 CommandReply。
func handlePreferenceCommand(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	rawText string,
	entSvc *entitlement.Service,
) (*CommandReply, error) {
	parts := strings.Fields(rawText)
	if len(parts) == 0 {
		return nil, nil
	}
	cmd, _ := slashCommandBase(rawText, "")
	switch cmd {
	case "/model":
		allowUserScoped, err := resolveByokEnabled(ctx, entSvc, accountID)
		if err != nil {
			return nil, err
		}
		if threadID == uuid.Nil {
			return &CommandReply{Text: "当前会话未配置 persona。"}, nil
		}
		preferredModel, reasoningMode, _, err := getInboundThreadModelPreference(ctx, tx, threadID)
		if err != nil {
			return nil, err
		}
		if len(parts) < 2 {
			// /model 无参数：显示当前状态 + 快速切换
			candidates, err := loadModelSelectorCandidates(ctx, tx, accountID)
			if err != nil {
				return nil, err
			}
			pickerData := GroupCandidatesByProvider(candidates, preferredModel, allowUserScoped)
			pickerData.ShowQuickSwitch = true
			modelDisplay := preferredModel
			if strings.TrimSpace(preferredModel) == "" {
				modelDisplay = "跟随频道默认"
			}
			return &CommandReply{
				Text:        RenderModelStatusText(modelDisplay, reasoningMode),
				Interactive: pickerData,
			}, nil
		}
		newModel := strings.TrimSpace(parts[1])
		if err := validateModelSelector(ctx, tx, accountID, newModel, allowUserScoped); err != nil {
			return &CommandReply{Text: fmt.Sprintf("模型选择器无效：%s", newModel)}, nil
		}
		if err := updateInboundThreadModelPreference(ctx, tx, threadID, newModel, reasoningMode); err != nil {
			return nil, err
		}
		return &CommandReply{Text: "model → " + newModel}, nil

	case "/think":
		if threadID == uuid.Nil {
			return &CommandReply{Text: "当前会话未配置 persona。"}, nil
		}
		preferredModel, reasoningMode, _, err := getInboundThreadModelPreference(ctx, tx, threadID)
		if err != nil {
			return nil, err
		}
		if len(parts) < 2 {
			display := reasoningMode
			if display == "" {
				display = "off"
			}
			modes := []ThinkModeOption{
				{Name: "off", IsSelected: display == "off"},
				{Name: "minimal", IsSelected: display == "minimal"},
				{Name: "low", IsSelected: display == "low"},
				{Name: "medium", IsSelected: display == "medium"},
				{Name: "high", IsSelected: display == "high"},
				{Name: "max", IsSelected: display == "max"},
			}
			pickerData := ThinkPickerData{CurrentMode: display, Modes: modes}
			return &CommandReply{
				Text:        RenderThinkPickerText(pickerData),
				Interactive: pickerData,
			}, nil
		}
		newLevel := strings.TrimSpace(parts[1])
		validModes := map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "max": true}
		if !validModes[newLevel] {
			return &CommandReply{Text: "无效思考级别。可选：off, minimal, low, medium, high, max"}, nil
		}
		if err := updateInboundThreadModelPreference(ctx, tx, threadID, preferredModel, newLevel); err != nil {
			return nil, err
		}
		return &CommandReply{Text: "think → " + newLevel}, nil

	default:
		return nil, nil
	}
}

// resolveNewSessionContext 获取 /new 和 /reset 回复所需的上下文信息。
// /new 后 thread 已删除，model 退回到 channel 默认值。
func resolveNewSessionContext(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	deps ChannelCommandDeps,
) (modelName, personaName string) {
	return extractChannelDefaultModel(ch), resolveChannelPersonaName(ctx, tx, ch, deps)
}

func resolveChannelPersonaName(ctx context.Context, tx pgx.Tx, ch data.Channel, deps ChannelCommandDeps) string {
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return ""
	}
	p, err := deps.PersonasRepo.WithTx(tx).GetByIDForAccount(ctx, ch.AccountID, *ch.PersonaID)
	if err != nil || p == nil {
		return ""
	}
	return p.DisplayName
}

func channelCommandRequiresAdmin(cmd string) bool {
	switch {
	case cmd == "/new", cmd == "/reset", cmd == "/stop", cmd == "/status", cmd == "/model", cmd == "/models", cmd == "/persona":
		return true
	case strings.HasPrefix(cmd, "/think"), strings.HasPrefix(cmd, "/heartbeat"):
		return true
	default:
		return false
	}
}

func resolveChannelCommandAdmin(ctx context.Context, resolver ChannelCommandResolver) bool {
	if resolver.IsBoundAdmin != nil {
		return resolver.IsBoundAdmin(ctx)
	}
	if resolver.IsGroupAdmin != nil {
		return resolver.IsGroupAdmin(ctx)
	}
	return true
}

var channelCommandHelpEntries = []struct {
	cmd       string
	args      string
	desc      string
	groupOnly bool
}{
	{"/start", "", "查看连接状态", false},
	{"/bind", "<code>", "绑定你的账号", false},
	{"/new", "", "开启新会话", false},
	{"/reset", "", "重置会话", false},
	{"/stop", "", "停止当前任务", false},
	{"/status", "", "查看当前状态", false},
	{"/model", "[name]", "View or switch model", false},
	{"/think", "[level]", "View or set thinking intensity", false},
	{"/models", "", "列出所有可用模型", false},
	{"/persona", "", "切换当前 persona", false},
	{"/heartbeat", "on/off", "设置心跳", true},
	{"/help", "", "显示此帮助", false},
}

func channelCommandHelpText(isPrivate bool) string {
	var sb strings.Builder
	for i, e := range channelCommandHelpEntries {
		if isPrivate && e.groupOnly {
			continue
		}
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(e.cmd)
		if !isPrivate {
			sb.WriteString("@bot")
		}
		if e.args != "" {
			sb.WriteByte(' ')
			sb.WriteString(e.args)
		}
		sb.WriteString(" — ")
		sb.WriteString(e.desc)
	}
	return sb.String()
}
