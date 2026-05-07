package accountapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/feishuclient"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/pgnotify"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const feishuWebhookMaxBodyBytes = 1 << 20
const feishuSignatureTolerance = 5 * time.Minute

type feishuChannelConfig struct {
	AppID             string   `json:"app_id"`
	Domain            string   `json:"domain,omitempty"`
	EncryptKey        string   `json:"-"`
	VerificationToken string   `json:"-"`
	AllowedUserIDs    []string `json:"allowed_user_ids,omitempty"`
	AllowedChatIDs    []string `json:"allowed_chat_ids,omitempty"`
	AllowAllUsers     bool     `json:"allow_all_users,omitempty"`
	DefaultModel      string   `json:"default_model,omitempty"`
	BotOpenID         string   `json:"bot_open_id,omitempty"`
	BotUserID         string   `json:"bot_user_id,omitempty"`
	BotName           string   `json:"bot_name,omitempty"`
	TriggerKeywords   []string `json:"trigger_keywords,omitempty"`
}

type feishuChannelSecret struct {
	AppSecret         string `json:"app_secret"`
	EncryptKey        string `json:"encrypt_key,omitempty"`
	VerificationToken string `json:"verification_token,omitempty"`
}

type feishuChannelSecretPatch struct {
	EncryptKey        *string
	VerificationToken *string
}

type feishuConnector struct {
	channelsRepo            *data.ChannelsRepository
	channelIdentitiesRepo   *data.ChannelIdentitiesRepository
	channelDMThreadsRepo    *data.ChannelDMThreadsRepository
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository
	channelReceiptsRepo     *data.ChannelMessageReceiptsRepository
	channelLedgerRepo       *data.ChannelMessageLedgerRepository
	secretsRepo             *data.SecretsRepository
	personasRepo            *data.PersonasRepository
	threadRepo              *data.ThreadRepository
	messageRepo             *data.MessageRepository
	runEventRepo            *data.RunEventRepository
	jobRepo                 *data.JobRepository
	pool                    data.DB
	inputNotify             func(ctx context.Context, runID uuid.UUID)
}

type feishuWebhookEnvelope struct {
	Type      string             `json:"type"`
	Token     string             `json:"token"`
	Challenge string             `json:"challenge"`
	Schema    string             `json:"schema"`
	Header    feishuEventHeader  `json:"header"`
	Event     feishuMessageEvent `json:"event"`
	Encrypt   string             `json:"encrypt"`
}

type feishuEventHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Token     string `json:"token"`
}

type feishuMessageEvent struct {
	Sender  feishuSender  `json:"sender"`
	Message feishuMessage `json:"message"`
}

type feishuSender struct {
	SenderID   feishuSenderID `json:"sender_id"`
	SenderType string         `json:"sender_type"`
}

type feishuSenderID struct {
	OpenID  string `json:"open_id"`
	UserID  string `json:"user_id"`
	UnionID string `json:"union_id"`
}

type feishuMessage struct {
	MessageID            string          `json:"message_id"`
	RootID               string          `json:"root_id"`
	ParentID             string          `json:"parent_id"`
	ThreadID             string          `json:"thread_id"`
	ReplyTargetMessageID string          `json:"reply_target_message_id"`
	ChatID               string          `json:"chat_id"`
	ChatType             string          `json:"chat_type"`
	MessageType          string          `json:"message_type"`
	Content              string          `json:"content"`
	Mentions             []feishuMention `json:"mentions"`
}

type feishuMention struct {
	Key string `json:"key"`
	ID  struct {
		OpenID  string `json:"open_id"`
		UserID  string `json:"user_id"`
		UnionID string `json:"union_id"`
	} `json:"id"`
	Name     string `json:"name"`
	UserName string `json:"user_name"`
}

type feishuIncomingMessage struct {
	MessageID        string
	ChatID           string
	ConversationType string
	MessageType      string
	Text             string
	SenderID         string
	SenderOpenID     string
	SenderUserID     string
	SenderUnionID    string
	SenderType       string
	ThreadID         string
	ParentMessageID  string
	MentionsBot      bool
	MentionsAll      bool
}

func normalizeFeishuChannelConfig(raw json.RawMessage) (json.RawMessage, *feishuChannelConfig, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var cfg feishuChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	if cfg.AppID == "" {
		return nil, nil, fmt.Errorf("feishu app_id must not be empty")
	}
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	if cfg.Domain == "" {
		cfg.Domain = "feishu"
	}
	if !validFeishuDomain(cfg.Domain) {
		return nil, nil, fmt.Errorf("feishu domain must be feishu or lark")
	}
	cfg.DefaultModel = strings.TrimSpace(cfg.DefaultModel)
	cfg.BotOpenID = strings.TrimSpace(cfg.BotOpenID)
	cfg.BotUserID = strings.TrimSpace(cfg.BotUserID)
	cfg.BotName = strings.TrimSpace(cfg.BotName)
	cfg.AllowedUserIDs = normalizeFeishuStringList(cfg.AllowedUserIDs, false)
	cfg.AllowedChatIDs = normalizeFeishuStringList(cfg.AllowedChatIDs, false)
	cfg.TriggerKeywords = normalizeFeishuStringList(cfg.TriggerKeywords, true)
	cfg.AllowAllUsers = len(cfg.AllowedUserIDs) == 0 && len(cfg.AllowedChatIDs) == 0
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	return normalized, &cfg, nil
}

func validFeishuDomain(domain string) bool {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "", "feishu", "lark":
		return true
	default:
		return false
	}
}

func mergeFeishuChannelConfigJSONPatch(existing, patch json.RawMessage) (json.RawMessage, error) {
	var base map[string]any
	if len(existing) == 0 {
		existing = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(existing, &base); err != nil {
		return nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	var delta map[string]any
	if len(patch) == 0 {
		patch = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(patch, &delta); err != nil {
		return nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	for k, v := range delta {
		if v == nil {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	merged, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	normalized, _, err := normalizeFeishuChannelConfig(merged)
	return normalized, err
}

func resolveFeishuChannelConfig(raw json.RawMessage) (feishuChannelConfig, error) {
	_, cfg, err := normalizeFeishuChannelConfig(raw)
	if err != nil {
		return feishuChannelConfig{}, err
	}
	return *cfg, nil
}

func sanitizeFeishuConfigForResponse(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return raw
	}
	delete(values, "encrypt_key")
	delete(values, "verification_token")
	out, err := json.Marshal(values)
	if err != nil {
		return raw
	}
	return out
}

func feishuSecretPatchFromConfig(raw json.RawMessage) (feishuChannelSecretPatch, error) {
	var patch feishuChannelSecretPatch
	if len(raw) == 0 {
		return patch, nil
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return patch, fmt.Errorf("config_json must be a valid JSON object")
	}
	if value, ok := values["encrypt_key"]; ok {
		parsed, err := feishuSecretStringField(value, "encrypt_key")
		if err != nil {
			return patch, err
		}
		patch.EncryptKey = &parsed
	}
	if value, ok := values["verification_token"]; ok {
		parsed, err := feishuSecretStringField(value, "verification_token")
		if err != nil {
			return patch, err
		}
		patch.VerificationToken = &parsed
	}
	return patch, nil
}

func feishuSecretStringField(value any, field string) (string, error) {
	if value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("feishu %s must be a string", field)
	}
	return strings.TrimSpace(text), nil
}

func encodeFeishuChannelSecret(secret feishuChannelSecret) (string, error) {
	secret.AppSecret = strings.TrimSpace(secret.AppSecret)
	secret.EncryptKey = strings.TrimSpace(secret.EncryptKey)
	secret.VerificationToken = strings.TrimSpace(secret.VerificationToken)
	if secret.AppSecret == "" {
		return "", fmt.Errorf("feishu app_secret must not be empty")
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeFeishuChannelSecret(raw string) (feishuChannelSecret, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return feishuChannelSecret{}, fmt.Errorf("feishu credentials unavailable")
	}
	var secret feishuChannelSecret
	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		return feishuChannelSecret{}, fmt.Errorf("feishu credentials invalid")
	}
	secret.AppSecret = strings.TrimSpace(secret.AppSecret)
	secret.EncryptKey = strings.TrimSpace(secret.EncryptKey)
	secret.VerificationToken = strings.TrimSpace(secret.VerificationToken)
	if secret.AppSecret == "" {
		return feishuChannelSecret{}, fmt.Errorf("feishu app_secret must not be empty")
	}
	return secret, nil
}

func applyFeishuSecretPatch(secret feishuChannelSecret, patch feishuChannelSecretPatch) feishuChannelSecret {
	if patch.EncryptKey != nil {
		secret.EncryptKey = strings.TrimSpace(*patch.EncryptKey)
	}
	if patch.VerificationToken != nil {
		secret.VerificationToken = strings.TrimSpace(*patch.VerificationToken)
	}
	return secret
}

func feishuSecretPatchPresent(patch feishuChannelSecretPatch) bool {
	return patch.EncryptKey != nil || patch.VerificationToken != nil
}

func feishuWebhookAuthConfigured(secret feishuChannelSecret) bool {
	return strings.TrimSpace(secret.EncryptKey) != "" && strings.TrimSpace(secret.VerificationToken) != ""
}

func applyFeishuSecretsToConfig(cfg feishuChannelConfig, secret feishuChannelSecret) feishuChannelConfig {
	cfg.EncryptKey = strings.TrimSpace(secret.EncryptKey)
	cfg.VerificationToken = strings.TrimSpace(secret.VerificationToken)
	return cfg
}

func loadFeishuChannelSecret(ctx context.Context, secretsRepo *data.SecretsRepository, credentialsID *uuid.UUID) (feishuChannelSecret, error) {
	if secretsRepo == nil {
		return feishuChannelSecret{}, fmt.Errorf("secrets repo not configured")
	}
	if credentialsID == nil || *credentialsID == uuid.Nil {
		return feishuChannelSecret{}, fmt.Errorf("feishu credentials unavailable")
	}
	raw, err := secretsRepo.DecryptByID(ctx, *credentialsID)
	if err != nil {
		return feishuChannelSecret{}, err
	}
	if raw == nil {
		return feishuChannelSecret{}, fmt.Errorf("feishu credentials unavailable")
	}
	return decodeFeishuChannelSecret(*raw)
}

func normalizeFeishuStringList(values []string, lower bool) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			item := strings.TrimSpace(part)
			if lower {
				item = strings.ToLower(item)
			}
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func mustValidateFeishuActivation(
	ctx context.Context,
	accountID uuid.UUID,
	personasRepo *data.PersonasRepository,
	personaID *uuid.UUID,
	configJSON json.RawMessage,
	secret feishuChannelSecret,
) (*data.Persona, feishuChannelConfig, error) {
	cfg, err := resolveFeishuChannelConfig(configJSON)
	if err != nil {
		return nil, feishuChannelConfig{}, err
	}
	if strings.TrimSpace(secret.AppSecret) == "" {
		return nil, feishuChannelConfig{}, fmt.Errorf("feishu channel requires app_secret")
	}
	if !feishuWebhookAuthConfigured(secret) {
		return nil, feishuChannelConfig{}, fmt.Errorf("feishu channel requires verification_token and encrypt_key")
	}
	if personaID == nil || *personaID == uuid.Nil {
		return nil, feishuChannelConfig{}, fmt.Errorf("feishu channel requires persona_id")
	}
	if personasRepo == nil {
		return nil, feishuChannelConfig{}, fmt.Errorf("personas repo not configured")
	}
	persona, err := personasRepo.GetByIDForAccount(ctx, accountID, *personaID)
	if err != nil {
		return nil, feishuChannelConfig{}, err
	}
	if persona == nil || !persona.IsActive {
		return nil, feishuChannelConfig{}, fmt.Errorf("persona not found or inactive")
	}
	return persona, cfg, nil
}

func mergeFeishuBotProfile(raw json.RawMessage, info *feishuclient.BotInfo) (json.RawMessage, bool, error) {
	if info == nil {
		return raw, false, nil
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, false, fmt.Errorf("config_json must be a valid JSON object")
	}
	changed := setStringIfChanged(cfg, "bot_open_id", strings.TrimSpace(info.OpenID))
	changed = setStringIfChanged(cfg, "bot_user_id", strings.TrimSpace(info.UserID)) || changed
	changed = setStringIfChanged(cfg, "bot_name", strings.TrimSpace(info.AppName)) || changed
	if !changed {
		return raw, false, nil
	}
	merged, err := json.Marshal(cfg)
	if err != nil {
		return nil, false, err
	}
	normalized, _, err := normalizeFeishuChannelConfig(merged)
	return normalized, err == nil, err
}

func verifyFeishuChannelBotInfo(ctx context.Context, cfg feishuChannelConfig, secret feishuChannelSecret) (*feishuclient.BotInfo, error) {
	client := feishuclient.NewClient(feishuclient.Config{
		AppID:     cfg.AppID,
		AppSecret: secret.AppSecret,
		Domain:    cfg.Domain,
	}, nil)
	return client.GetBotInfo(ctx)
}

func feishuConfigMissingBotProfile(raw json.RawMessage) bool {
	cfg, err := resolveFeishuChannelConfig(raw)
	if err != nil {
		return false
	}
	return cfg.BotOpenID == "" && cfg.BotUserID == "" && cfg.BotName == ""
}

func feishuConfigPatchTouchesAppID(raw *json.RawMessage) bool {
	if raw == nil || len(*raw) == 0 {
		return false
	}
	var values map[string]any
	if err := json.Unmarshal(*raw, &values); err != nil {
		return false
	}
	_, ok := values["app_id"]
	return ok
}

func setStringIfChanged(values map[string]any, key, value string) bool {
	if value == "" {
		return false
	}
	if current, _ := values[key].(string); strings.TrimSpace(current) == value {
		return false
	}
	values[key] = value
	return true
}

func feishuWebhookEntry(
	channelsRepo *data.ChannelsRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	channelReceiptsRepo *data.ChannelMessageReceiptsRepository,
	secretsRepo *data.SecretsRepository,
	personasRepo *data.PersonasRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	runEventRepo *data.RunEventRepository,
	jobRepo *data.JobRepository,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	var channelLedgerRepo *data.ChannelMessageLedgerRepository
	if pool != nil {
		repo, err := data.NewChannelMessageLedgerRepository(pool)
		if err != nil {
			panic(err)
		}
		channelLedgerRepo = repo
	}
	connector := feishuConnector{
		channelsRepo:            channelsRepo,
		channelIdentitiesRepo:   channelIdentitiesRepo,
		channelDMThreadsRepo:    channelDMThreadsRepo,
		channelGroupThreadsRepo: channelGroupThreadsRepo,
		channelReceiptsRepo:     channelReceiptsRepo,
		channelLedgerRepo:       channelLedgerRepo,
		secretsRepo:             secretsRepo,
		personasRepo:            personasRepo,
		threadRepo:              threadRepo,
		messageRepo:             messageRepo,
		runEventRepo:            runEventRepo,
		jobRepo:                 jobRepo,
		pool:                    pool,
		inputNotify: func(ctx context.Context, runID uuid.UUID) {
			if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String()); err != nil {
				slog.Warn("feishu_active_run_notify_failed", "run_id", runID.String(), "error", err)
			}
		},
	}

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if channelsRepo == nil || channelIdentitiesRepo == nil || channelDMThreadsRepo == nil || channelGroupThreadsRepo == nil ||
			channelReceiptsRepo == nil || secretsRepo == nil || personasRepo == nil || threadRepo == nil || messageRepo == nil || runEventRepo == nil || jobRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		channelID, ok := parseFeishuWebhookChannelID(r.URL.Path)
		if !ok {
			httpkit.WriteNotFound(w, r)
			return
		}

		rawBody, err := io.ReadAll(io.LimitReader(r.Body, feishuWebhookMaxBodyBytes+1))
		if err != nil || len(rawBody) > feishuWebhookMaxBodyBytes {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid feishu payload", traceID, nil)
			return
		}

		ch, err := channelsRepo.GetByID(r.Context(), channelID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if ch == nil || ch.ChannelType != "feishu" {
			httpkit.WriteNotFound(w, r)
			return
		}
		cfg, err := resolveFeishuChannelConfig(ch.ConfigJSON)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		secret, err := loadFeishuChannelSecret(r.Context(), secretsRepo, ch.CredentialsID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		if !feishuWebhookAuthConfigured(secret) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "feishu channel requires verification_token and encrypt_key", traceID, nil)
			return
		}
		cfg = applyFeishuSecretsToConfig(cfg, secret)

		payloadBytes, err := decodeFeishuWebhookPayload(r, rawBody, cfg.EncryptKey, false)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", err.Error(), traceID, nil)
			return
		}
		var envelope feishuWebhookEnvelope
		if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid feishu payload", traceID, nil)
			return
		}
		if !feishuTokenMatches(envelope, cfg.VerificationToken) {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "channels.invalid_signature", "invalid feishu token", traceID, nil)
			return
		}
		if envelope.Type == "url_verification" {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]string{"challenge": envelope.Challenge})
			return
		}
		if !verifyFeishuSignature(r, rawBody, cfg.EncryptKey) {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "channels.invalid_signature", "invalid feishu signature", traceID, nil)
			return
		}
		if !ch.IsActive {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}
		if envelope.Header.EventType != "im.message.receive_v1" {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		incoming, err := normalizeFeishuIncoming(envelope.Event, cfg)
		if err != nil {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}
		if err := connector.HandleIncoming(r.Context(), traceID, *ch, cfg, incoming); err != nil {
			status := nethttp.StatusInternalServerError
			code := "internal.error"
			message := "internal error"
			if strings.Contains(err.Error(), "persona") || strings.Contains(err.Error(), "config") || strings.Contains(err.Error(), "feishu") {
				status = nethttp.StatusUnprocessableEntity
				code = "validation.error"
				message = err.Error()
			}
			httpkit.WriteError(w, status, code, message, traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	}
}

func parseFeishuWebhookChannelID(path string) (uuid.UUID, bool) {
	tail := strings.Trim(strings.TrimPrefix(path, "/v1/channels/feishu/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[1] != "webhook" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func decodeFeishuWebhookPayload(r *nethttp.Request, rawBody []byte, encryptKey string, requireSignature bool) ([]byte, error) {
	if requireSignature && strings.TrimSpace(encryptKey) != "" && !verifyFeishuSignature(r, rawBody, encryptKey) {
		return nil, fmt.Errorf("invalid feishu signature")
	}
	var outer feishuWebhookEnvelope
	if err := json.Unmarshal(rawBody, &outer); err != nil {
		return nil, fmt.Errorf("invalid feishu payload")
	}
	if outer.Encrypt != "" {
		if strings.TrimSpace(encryptKey) == "" {
			return nil, fmt.Errorf("feishu encrypt_key is required")
		}
		return decryptFeishuPayload(outer.Encrypt, encryptKey)
	}
	return rawBody, nil
}

func verifyFeishuSignature(r *nethttp.Request, rawBody []byte, encryptKey string) bool {
	return verifyFeishuSignatureAt(r, rawBody, encryptKey, time.Now())
}

func verifyFeishuSignatureAt(r *nethttp.Request, rawBody []byte, encryptKey string, now time.Time) bool {
	timestamp := strings.TrimSpace(r.Header.Get("X-Lark-Request-Timestamp"))
	nonce := strings.TrimSpace(r.Header.Get("X-Lark-Request-Nonce"))
	signature := strings.TrimSpace(r.Header.Get("X-Lark-Signature"))
	if timestamp == "" || nonce == "" || signature == "" || strings.TrimSpace(encryptKey) == "" {
		return false
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	signedAt := time.Unix(seconds, 0)
	if now.IsZero() {
		now = time.Now()
	}
	if now.Sub(signedAt) > feishuSignatureTolerance || signedAt.Sub(now) > feishuSignatureTolerance {
		return false
	}
	var buf bytes.Buffer
	buf.WriteString(timestamp)
	buf.WriteString(nonce)
	buf.WriteString(encryptKey)
	buf.Write(rawBody)
	sum := sha256.Sum256(buf.Bytes())
	expected := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

func decryptFeishuPayload(encrypted, encryptKey string) ([]byte, error) {
	cipherText, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encrypted))
	if err != nil {
		return nil, fmt.Errorf("invalid feishu encrypt payload")
	}
	if len(cipherText) <= aes.BlockSize || len(cipherText)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid feishu encrypt payload")
	}
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(cipherText)-aes.BlockSize)
	mode := cipher.NewCBCDecrypter(block, cipherText[:aes.BlockSize])
	mode.CryptBlocks(plain, cipherText[aes.BlockSize:])
	return pkcs7Unpad(plain)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

func feishuTokenMatches(envelope feishuWebhookEnvelope, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	token := strings.TrimSpace(envelope.Header.Token)
	if token == "" {
		token = strings.TrimSpace(envelope.Token)
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func normalizeFeishuIncoming(event feishuMessageEvent, cfg feishuChannelConfig) (feishuIncomingMessage, error) {
	msg := event.Message
	messageID := strings.TrimSpace(msg.MessageID)
	chatID := strings.TrimSpace(msg.ChatID)
	senderOpenID := strings.TrimSpace(event.Sender.SenderID.OpenID)
	senderUserID := strings.TrimSpace(event.Sender.SenderID.UserID)
	senderUnionID := strings.TrimSpace(event.Sender.SenderID.UnionID)
	senderType := strings.ToLower(strings.TrimSpace(event.Sender.SenderType))
	senderID := firstNonEmptyFeishu(senderOpenID, senderUserID, senderUnionID)
	text := extractFeishuMessageText(msg.MessageType, msg.Content)
	if messageID == "" || chatID == "" || senderID == "" || strings.TrimSpace(text) == "" {
		return feishuIncomingMessage{}, fmt.Errorf("empty feishu message")
	}
	conversationType := "group"
	switch strings.TrimSpace(msg.ChatType) {
	case "p2p", "private":
		conversationType = "private"
	}
	mentionsBot, mentionsAll := feishuMentionsTargetBot(msg.Mentions, cfg)
	return feishuIncomingMessage{
		MessageID:        messageID,
		ChatID:           chatID,
		ConversationType: conversationType,
		MessageType:      strings.TrimSpace(msg.MessageType),
		Text:             strings.TrimSpace(text),
		SenderID:         senderID,
		SenderOpenID:     senderOpenID,
		SenderUserID:     senderUserID,
		SenderUnionID:    senderUnionID,
		SenderType:       senderType,
		ThreadID:         firstNonEmptyFeishu(strings.TrimSpace(msg.ThreadID), strings.TrimSpace(msg.RootID)),
		ParentMessageID:  firstNonEmptyFeishu(strings.TrimSpace(msg.ReplyTargetMessageID), strings.TrimSpace(msg.ParentID)),
		MentionsBot:      mentionsBot,
		MentionsAll:      mentionsAll,
	}, nil
}

func extractFeishuMessageText(messageType string, rawContent string) string {
	switch strings.TrimSpace(messageType) {
	case "text":
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(rawContent), &content); err == nil {
			return strings.TrimSpace(content.Text)
		}
	case "post":
		return extractFeishuPostText(rawContent)
	}
	return ""
}

func extractFeishuPostText(rawContent string) string {
	var post struct {
		Title   string `json:"title"`
		Content [][]struct {
			Tag      string `json:"tag"`
			Text     string `json:"text"`
			UserName string `json:"user_name"`
			Href     string `json:"href"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawContent), &post); err != nil {
		return ""
	}
	var lines []string
	for _, line := range post.Content {
		var parts []string
		for _, item := range line {
			switch item.Tag {
			case "text":
				parts = append(parts, item.Text)
			case "a":
				if strings.TrimSpace(item.Text) != "" {
					parts = append(parts, item.Text)
				} else {
					parts = append(parts, item.Href)
				}
			case "at":
				if strings.TrimSpace(item.UserName) != "" {
					parts = append(parts, "@"+strings.TrimSpace(item.UserName))
				}
			}
		}
		if joined := strings.TrimSpace(strings.Join(parts, "")); joined != "" {
			lines = append(lines, joined)
		}
	}
	if title := strings.TrimSpace(post.Title); title != "" {
		lines = append([]string{title}, lines...)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func feishuMentionsTargetBot(mentions []feishuMention, cfg feishuChannelConfig) (bool, bool) {
	for _, mention := range mentions {
		name := strings.TrimSpace(firstNonEmptyFeishu(mention.Name, mention.UserName))
		key := strings.ToLower(strings.TrimSpace(mention.Key))
		openID := strings.TrimSpace(mention.ID.OpenID)
		userID := strings.TrimSpace(mention.ID.UserID)
		if key == "@_all" || strings.EqualFold(name, "all") || strings.EqualFold(name, "所有人") {
			return false, true
		}
		if cfg.BotOpenID != "" && openID == cfg.BotOpenID {
			return true, false
		}
		if cfg.BotUserID != "" && userID == cfg.BotUserID {
			return true, false
		}
		if cfg.BotName != "" && name != "" && strings.EqualFold(name, cfg.BotName) {
			return true, false
		}
	}
	return false, false
}

func (c *feishuConnector) HandleIncoming(ctx context.Context, traceID string, ch data.Channel, cfg feishuChannelConfig, incoming feishuIncomingMessage) error {
	if feishuIncomingFromSelf(cfg, incoming) {
		return nil
	}
	if !feishuIncomingAllowed(cfg, incoming) {
		return nil
	}
	if incoming.ConversationType != "private" && !incoming.MentionsBot && !incoming.MentionsAll && !feishuMessageMatchesKeyword(incoming.Text, cfg.TriggerKeywords) {
		return nil
	}

	freshChannel, ok, err := c.currentFeishuChannel(ctx, ch)
	if err != nil || !ok {
		return err
	}
	ch = freshChannel

	persona, personaRef, err := c.resolveFeishuPersona(ctx, ch)
	if err != nil {
		return err
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	commitTx := func() error { return tx.Commit(ctx) }

	received, err := c.channelReceiptsRepo.WithTx(tx).Record(ctx, ch.ID, incoming.ChatID, incoming.MessageID)
	if err != nil {
		return err
	}
	if !received {
		return commitTx()
	}

	displayName := incoming.SenderID
	identityMeta, _ := json.Marshal(map[string]any{
		"open_id":  incoming.SenderOpenID,
		"user_id":  incoming.SenderUserID,
		"union_id": incoming.SenderUnionID,
	})
	identity, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, "feishu", incoming.SenderID, &displayName, nil, identityMeta)
	if err != nil {
		return err
	}
	if incoming.ConversationType != "private" {
		if _, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, ch.ChannelType, incoming.ChatID, nil, nil, nil); err != nil {
			return err
		}
	}

	var ledgerMeta json.RawMessage
	if c.channelLedgerRepo != nil {
		ledgerMeta, _ = json.Marshal(map[string]any{
			"source":             "feishu",
			"conversation_type":  incoming.ConversationType,
			"mentions_bot":       incoming.MentionsBot,
			"mentions_all":       incoming.MentionsAll,
			"platform_thread_id": incoming.ThreadID,
			"platform_parent_id": incoming.ParentMessageID,
		})
		if _, err := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			PlatformConversationID:  incoming.ChatID,
			PlatformMessageID:       incoming.MessageID,
			PlatformParentMessageID: stringPtrOrNil(incoming.ParentMessageID),
			PlatformThreadID:        stringPtrOrNil(incoming.ThreadID),
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMeta,
		}); err != nil {
			return err
		}
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
			if pid, err := c.personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, ownerUserID); err == nil {
				threadProjectID = pid
			}
		}
	}
	if threadProjectID == uuid.Nil {
		return fmt.Errorf("cannot resolve project for persona %s", persona.ID)
	}
	threadID, err := c.resolveFeishuThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, incoming)
	if err != nil {
		return err
	}

	content, err := messagecontent.Normalize(messagecontent.FromText(incoming.Text).Parts)
	if err != nil {
		return err
	}
	contentJSON, err := content.JSON()
	if err != nil {
		return err
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"source":              "feishu",
		"channel_identity_id": identity.ID.String(),
		"platform_chat_id":    incoming.ChatID,
		"platform_message_id": incoming.MessageID,
		"platform_user_id":    incoming.SenderID,
		"chat_type":           incoming.ConversationType,
		"message_type":        incoming.MessageType,
		"platform_thread_id":  incoming.ThreadID,
	})
	msg, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(ctx, ch.AccountID, threadID, "user", incoming.Text, contentJSON, metadataJSON, identity.UserID)
	if err != nil {
		return err
	}
	if err := c.updateFeishuInboundLedger(ctx, tx, ch, incoming, &identity.ID, &threadID, nil, &msg.ID, ledgerMeta); err != nil {
		return err
	}

	runRepoTx := c.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, threadID); err != nil {
		return err
	}
	activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, threadID)
	if err != nil {
		return err
	}
	if activeRun != nil {
		delivered, err := c.deliverToActiveRun(ctx, runRepoTx, activeRun, incoming.Text, traceID, incoming)
		if err != nil {
			return err
		}
		if delivered {
			if err := c.updateFeishuInboundLedger(ctx, tx, ch, incoming, &identity.ID, &threadID, &activeRun.ID, &msg.ID, ledgerMeta); err != nil {
				return err
			}
			if err := commitTx(); err != nil {
				return err
			}
			c.notifyInput(ctx, activeRun.ID)
			return nil
		}
	}
	if !channelAgentTriggerConsume(ch.ID) {
		return commitTx()
	}

	channelDelivery := buildFeishuChannelDeliveryPayload(ch.ID, identity.ID, incoming)
	runData := map[string]any{
		"persona_id":             personaRef,
		"continuation_source":    "none",
		"continuation_loop":      false,
		"channel_delivery":       channelDelivery,
		"thread_tail_message_id": msg.ID.String(),
	}
	if model := strings.TrimSpace(cfg.DefaultModel); model != "" {
		runData["model"] = model
	}
	run, _, err := runRepoTx.CreateRunWithStartedEvent(ctx, ch.AccountID, threadID, channelOwnerUserID(ch), "run.started", runData)
	if err != nil {
		return err
	}
	if err := c.updateFeishuInboundLedger(ctx, tx, ch, incoming, &identity.ID, &threadID, &run.ID, &msg.ID, ledgerMeta); err != nil {
		return err
	}
	jobPayload := map[string]any{
		"source":           "feishu",
		"channel_delivery": channelDelivery,
		"message_id":       incoming.MessageID,
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(ctx, ch.AccountID, run.ID, traceID, data.RunExecuteJobType, jobPayload, nil); err != nil {
		return err
	}
	return commitTx()
}

func (c *feishuConnector) updateFeishuInboundLedger(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	incoming feishuIncomingMessage,
	senderIdentityID *uuid.UUID,
	threadID *uuid.UUID,
	runID *uuid.UUID,
	messageID *uuid.UUID,
	metadata json.RawMessage,
) error {
	if c == nil || c.channelLedgerRepo == nil {
		return nil
	}
	updated, err := c.channelLedgerRepo.WithTx(tx).UpdateInboundEntry(
		ctx,
		ch.ID,
		incoming.ChatID,
		incoming.MessageID,
		threadID,
		runID,
		messageID,
		metadata,
	)
	if err != nil {
		return err
	}
	if updated {
		return nil
	}
	_, err = c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:               ch.ID,
		ChannelType:             ch.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                threadID,
		RunID:                   runID,
		PlatformConversationID:  incoming.ChatID,
		PlatformMessageID:       incoming.MessageID,
		PlatformParentMessageID: stringPtrOrNil(incoming.ParentMessageID),
		PlatformThreadID:        stringPtrOrNil(incoming.ThreadID),
		SenderChannelIdentityID: senderIdentityID,
		MessageID:               messageID,
		MetadataJSON:            metadata,
	})
	return err
}

func feishuIncomingFromSelf(cfg feishuChannelConfig, incoming feishuIncomingMessage) bool {
	switch strings.ToLower(strings.TrimSpace(incoming.SenderType)) {
	case "bot", "app":
		return true
	}
	if cfg.BotOpenID != "" && incoming.SenderOpenID == cfg.BotOpenID {
		return true
	}
	if cfg.BotUserID != "" && incoming.SenderUserID == cfg.BotUserID {
		return true
	}
	return false
}

func (c *feishuConnector) currentFeishuChannel(ctx context.Context, ch data.Channel) (data.Channel, bool, error) {
	if c == nil || c.channelsRepo == nil || ch.ID == uuid.Nil {
		return ch, true, nil
	}
	latest, err := c.channelsRepo.GetByID(ctx, ch.ID)
	if err != nil {
		return data.Channel{}, false, err
	}
	if latest == nil || !latest.IsActive || latest.ChannelType != "feishu" {
		return data.Channel{}, false, nil
	}
	return *latest, true, nil
}

func (c *feishuConnector) resolveFeishuPersona(ctx context.Context, ch data.Channel) (*data.Persona, string, error) {
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return nil, "", fmt.Errorf("feishu channel requires persona_id")
	}
	persona, err := c.personasRepo.GetByIDForAccount(ctx, ch.AccountID, *ch.PersonaID)
	if err != nil {
		return nil, "", err
	}
	if persona == nil || !persona.IsActive {
		return nil, "", fmt.Errorf("persona not found or inactive")
	}
	return persona, buildPersonaRef(*persona), nil
}

func (c *feishuConnector) resolveFeishuThreadID(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	personaID uuid.UUID,
	projectID uuid.UUID,
	identity data.ChannelIdentity,
	incoming feishuIncomingMessage,
) (uuid.UUID, error) {
	threadRepoTx := c.threadRepo.WithTx(tx)
	buildTitle := func() *string {
		title := "飞书 " + incoming.ChatID
		if incoming.ConversationType == "private" {
			title = incoming.SenderID + " (飞书私聊)"
		}
		return &title
	}
	lockTitle := func(threadID uuid.UUID) {
		_, _ = threadRepoTx.UpdateFields(ctx, threadID, data.ThreadUpdateFields{
			SetTitleLocked: true,
			TitleLocked:    true,
		})
	}

	if incoming.ConversationType == "private" {
		dmRepo := c.channelDMThreadsRepo.WithTx(tx)
		threadMap, err := dmRepo.GetByBinding(ctx, ch.ID, identity.ID, personaID, "")
		if err != nil {
			return uuid.Nil, err
		}
		if threadMap != nil {
			if existing, _ := threadRepoTx.GetByID(ctx, threadMap.ThreadID); existing != nil {
				return threadMap.ThreadID, nil
			}
			_ = dmRepo.DeleteByBinding(ctx, ch.ID, identity.ID, personaID, "")
		}
		thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, buildTitle(), false)
		if err != nil {
			return uuid.Nil, err
		}
		lockTitle(thread.ID)
		if _, err := dmRepo.Create(ctx, ch.ID, identity.ID, personaID, "", thread.ID); err != nil {
			return uuid.Nil, err
		}
		return thread.ID, nil
	}

	groupKey := incoming.ChatID
	if incoming.ThreadID != "" {
		groupKey = incoming.ChatID + ":thread:" + incoming.ThreadID
	}
	groupRepo := c.channelGroupThreadsRepo.WithTx(tx)
	threadMap, err := groupRepo.GetByBinding(ctx, ch.ID, groupKey, personaID)
	if err != nil {
		return uuid.Nil, err
	}
	if threadMap != nil {
		if existing, _ := threadRepoTx.GetByID(ctx, threadMap.ThreadID); existing != nil {
			return threadMap.ThreadID, nil
		}
		_ = groupRepo.DeleteByBinding(ctx, ch.ID, groupKey, personaID)
	}
	thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, buildTitle(), false)
	if err != nil {
		return uuid.Nil, err
	}
	lockTitle(thread.ID)
	if _, err := groupRepo.Create(ctx, ch.ID, groupKey, personaID, thread.ID); err != nil {
		return uuid.Nil, err
	}
	return thread.ID, nil
}

func (c *feishuConnector) deliverToActiveRun(ctx context.Context, repo *data.RunEventRepository, run *data.Run, content, traceID string, incoming feishuIncomingMessage) (bool, error) {
	if run == nil || strings.TrimSpace(content) == "" {
		return false, nil
	}
	key := fmt.Sprintf("feishu:%s:%s", incoming.ChatID, incoming.MessageID)
	if _, err := repo.ProvideInputWithKey(ctx, run.ID, content, traceID, key); err != nil {
		var notActive data.RunNotActiveError
		if errors.As(err, &notActive) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *feishuConnector) notifyInput(ctx context.Context, runID uuid.UUID) {
	if c.inputNotify == nil || runID == uuid.Nil {
		return
	}
	c.inputNotify(ctx, runID)
}

func buildFeishuChannelDeliveryPayload(channelID, identityID uuid.UUID, incoming feishuIncomingMessage) map[string]any {
	payload := map[string]any{
		"channel_id":   channelID.String(),
		"channel_type": "feishu",
		"conversation_ref": map[string]any{
			"target": incoming.ChatID,
		},
		"inbound_message_ref": map[string]any{
			"message_id": incoming.MessageID,
		},
		"trigger_message_ref": map[string]any{
			"message_id": incoming.MessageID,
		},
		"platform_chat_id":           incoming.ChatID,
		"platform_message_id":        incoming.MessageID,
		"sender_channel_identity_id": identityID.String(),
		"conversation_type":          incoming.ConversationType,
		"mentions_bot":               incoming.MentionsBot,
	}
	if incoming.ThreadID != "" {
		payload["platform_thread_id"] = incoming.ThreadID
		payload["conversation_ref"].(map[string]any)["thread_id"] = incoming.ThreadID
	}
	return payload
}

func feishuIncomingAllowed(cfg feishuChannelConfig, incoming feishuIncomingMessage) bool {
	if cfg.AllowAllUsers {
		return true
	}
	for _, chatID := range cfg.AllowedChatIDs {
		if chatID == incoming.ChatID {
			return true
		}
	}
	for _, allowed := range cfg.AllowedUserIDs {
		if allowed == incoming.SenderID || allowed == incoming.SenderOpenID || allowed == incoming.SenderUserID || allowed == incoming.SenderUnionID {
			return true
		}
	}
	return false
}

func feishuMessageMatchesKeyword(text string, keywords []string) bool {
	lowerText := strings.ToLower(strings.TrimSpace(text))
	if lowerText == "" {
		return false
	}
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(lowerText, keyword) {
			return true
		}
	}
	return false
}

func firstNonEmptyFeishu(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
