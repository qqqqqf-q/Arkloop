package accountapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/discordbot"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

const discordRemoteRequestTimeout = 10 * time.Second

type discordChannelConfig struct {
	AllowedServerIDs   []string `json:"allowed_server_ids"`
	AllowedChannelIDs  []string `json:"allowed_channel_ids"`
	DefaultModel       string   `json:"default_model,omitempty"`
	DiscordApplicationID string `json:"discord_application_id,omitempty"`
	DiscordBotUserID     string `json:"discord_bot_user_id,omitempty"`
}

func normalizeDiscordChannelConfig(raw json.RawMessage) (json.RawMessage, *discordChannelConfig, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, nil, fmt.Errorf("config_json must be a valid JSON object")
	}

	var cfg discordChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	cfg.AllowedServerIDs = normalizeDiscordIDList(cfg.AllowedServerIDs)
	cfg.AllowedChannelIDs = normalizeDiscordIDList(cfg.AllowedChannelIDs)
	cfg.DefaultModel = strings.TrimSpace(cfg.DefaultModel)
	cfg.DiscordApplicationID = strings.TrimSpace(cfg.DiscordApplicationID)
	cfg.DiscordBotUserID = strings.TrimSpace(cfg.DiscordBotUserID)

	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	return normalized, &cfg, nil
}

func resolveDiscordConfig(channelType string, raw json.RawMessage) (discordChannelConfig, error) {
	if channelType != "discord" {
		return discordChannelConfig{}, fmt.Errorf("unsupported channel type")
	}
	_, cfg, err := normalizeDiscordChannelConfig(raw)
	if err != nil {
		return discordChannelConfig{}, err
	}
	if cfg == nil {
		return discordChannelConfig{}, nil
	}
	return *cfg, nil
}

func normalizeDiscordIDList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				continue
			}
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			out = append(out, cleaned)
		}
	}
	return out
}

func mergeDiscordChannelConfigJSONPatch(existing, patch json.RawMessage) (json.RawMessage, error) {
	if len(patch) == 0 {
		normalized, _, err := normalizeDiscordChannelConfig(existing)
		return normalized, err
	}
	ex := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &ex); err != nil {
			return nil, fmt.Errorf("config_json must be a valid JSON object")
		}
	}
	if ex == nil {
		ex = map[string]any{}
	}
	patchMap := map[string]any{}
	if err := json.Unmarshal(patch, &patchMap); err != nil {
		return nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	for k, v := range patchMap {
		ex[k] = v
	}
	merged, err := json.Marshal(ex)
	if err != nil {
		return nil, err
	}
	normalized, _, err := normalizeDiscordChannelConfig(merged)
	return normalized, err
}

func mustValidateDiscordActivation(
	ctx context.Context,
	accountID uuid.UUID,
	personasRepo *data.PersonasRepository,
	personaID *uuid.UUID,
) (*data.Persona, string, error) {
	if personaID == nil || *personaID == uuid.Nil {
		return nil, "", fmt.Errorf("discord channel requires persona_id before activation")
	}
	persona, err := personasRepo.GetByIDForAccount(ctx, accountID, *personaID)
	if err != nil {
		return nil, "", err
	}
	if persona == nil || !persona.IsActive {
		return nil, "", fmt.Errorf("persona not found or inactive")
	}
	if persona.ProjectID == nil || *persona.ProjectID == uuid.Nil {
		return nil, "", fmt.Errorf("discord channel persona must belong to a project")
	}
	return persona, buildPersonaRef(*persona), nil
}

func mergeDiscordBotProfile(raw json.RawMessage, info *discordbot.VerifiedBot) (json.RawMessage, bool, error) {
	if info == nil {
		return nil, false, fmt.Errorf("discord verify result required")
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	cfg, err := resolveDiscordConfig("discord", raw)
	if err != nil {
		return nil, false, err
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, false, err
	}
	if generic == nil {
		generic = map[string]any{}
	}

	changed := false
	if cfg.DiscordApplicationID == "" && strings.TrimSpace(info.ApplicationID) != "" {
		generic["discord_application_id"] = strings.TrimSpace(info.ApplicationID)
		changed = true
	}
	if cfg.DiscordBotUserID == "" && strings.TrimSpace(info.BotUserID) != "" {
		generic["discord_bot_user_id"] = strings.TrimSpace(info.BotUserID)
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(generic)
	if err != nil {
		return nil, false, err
	}
	normalized, _, err := normalizeDiscordChannelConfig(out)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func discordCommands() []discordbot.ApplicationCommand {
	return []discordbot.ApplicationCommand{
		{
			Name:        "help",
			Description: "查看帮助",
			Type:        int(discordgo.ChatApplicationCommand),
		},
		{
			Name:        "bind",
			Description: "绑定 Arkloop 账号",
			Type:        int(discordgo.ChatApplicationCommand),
			Options: []discordbot.ApplicationCommandOption{
				{
					Type:        int(discordgo.ApplicationCommandOptionString),
					Name:        "code",
					Description: "绑定码",
					Required:    true,
				},
			},
		},
		{
			Name:        "new",
			Description: "开启新会话",
			Type:        int(discordgo.ChatApplicationCommand),
		},
		{
			Name:        "model",
			Description: "查看或切换模型",
			Type:        int(discordgo.ChatApplicationCommand),
			Options: []discordbot.ApplicationCommandOption{
				{
					Type:        int(discordgo.ApplicationCommandOptionString),
					Name:        "name",
					Description: "模型名称，不填则查看当前",
					Required:    false,
				},
			},
		},
		{
			Name:        "think",
			Description: "查看或设置思考深度",
			Type:        int(discordgo.ChatApplicationCommand),
			Options: []discordbot.ApplicationCommandOption{
				{
					Type:        int(discordgo.ApplicationCommandOptionString),
					Name:        "level",
					Description: "思考深度",
					Required:    false,
					Choices: []discordbot.ApplicationCommandOptionChoice{
						{Name: "off", Value: "off"},
						{Name: "minimal", Value: "minimal"},
						{Name: "low", Value: "low"},
						{Name: "medium", Value: "medium"},
						{Name: "high", Value: "high"},
						{Name: "max", Value: "max"},
					},
				},
			},
		},
		{
			Name:        "heartbeat",
			Description: "查看或切换心跳状态",
			Type:        int(discordgo.ChatApplicationCommand),
			Options: []discordbot.ApplicationCommandOption{
				{
					Type:        int(discordgo.ApplicationCommandOptionString),
					Name:        "action",
					Description: "操作",
					Required:    false,
					Choices: []discordbot.ApplicationCommandOptionChoice{
						{Name: "status", Value: "status"},
						{Name: "on", Value: "on"},
						{Name: "off", Value: "off"},
					},
				},
			},
		},
		{
			Name:        "stop",
			Description: "停止当前任务",
			Type:        int(discordgo.ChatApplicationCommand),
		},
	}
}

func discordCommandAllowed(cfg discordChannelConfig, guildID, channelID string) bool {
	guildID = strings.TrimSpace(guildID)
	channelID = strings.TrimSpace(channelID)
	if guildID == "" {
		return true
	}
	if len(cfg.AllowedServerIDs) > 0 && !containsString(cfg.AllowedServerIDs, guildID) {
		return false
	}
	if len(cfg.AllowedChannelIDs) > 0 && !containsString(cfg.AllowedChannelIDs, channelID) {
		return false
	}
	return true
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range values {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}
