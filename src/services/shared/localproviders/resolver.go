package localproviders

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrCredentialUnavailable = errors.New("local provider credential unavailable")

const (
	claudeConfigDirEnv        = "CLAUDE_CONFIG_DIR"
	claudeAnthropicAuthEnv    = "ANTHROPIC_AUTH_TOKEN"
	claudeAnthropicBaseURLEnv = "ANTHROPIC_BASE_URL"
	claudeOAuthTokenEnv       = "CLAUDE_CODE_OAUTH_TOKEN"
	claudeAnthropicAPIKeyEnv  = "ANTHROPIC_API_KEY"
	claudeModelEnv            = "ANTHROPIC_MODEL"
	claudeReasoningModelEnv   = "ANTHROPIC_REASONING_MODEL"
	claudeDefaultOpusEnv      = "ANTHROPIC_DEFAULT_OPUS_MODEL"
	claudeDefaultSonnetEnv    = "ANTHROPIC_DEFAULT_SONNET_MODEL"
	claudeDefaultHaikuEnv     = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
	claudeServiceBase         = "Claude Code"
	claudeOAuthServiceSuffix  = "-credentials"
	claudeOAuthClientID       = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeOAuthScopes         = "org:create_api_key user:profile user:inference"

	codexHomeEnv              = "CODEX_HOME"
	codexAPIKeyEnv            = "CODEX_API_KEY"
	codexAuthService          = "Codex Auth"
	codexOAuthClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthRefreshLeeway        = 5 * time.Minute
	defaultRefreshExpiry      = time.Hour
	defaultResolverCacheTTL   = 30 * time.Second
	claudeAPIKeyHelperSetting = "apiKeyHelper"
	arkloopDataDirEnv         = "ARKLOOP_DATA_DIR"
	localProviderPrefsFile    = "local-provider-preferences.json"
)

var (
	claudeOAuthTokenEndpoint = "https://platform.claude.com/v1/oauth/token"
	codexOAuthTokenEndpoint  = "https://auth.openai.com/oauth/token"
)

type CommandRunner func(ctx context.Context, name string, args ...string) (string, error)

type Options struct {
	HomeDir         string
	Platform        string
	DisableKeychain bool
	Env             map[string]string
	CommandRunner   CommandRunner
	HTTPClient      *http.Client
	Now             func() time.Time
	CacheTTL        time.Duration
}

type ResolveOptions struct {
	Refresh bool
}

type Credential struct {
	ProviderID   string
	ProviderKind string
	AuthMode     string
	APIKey       string
	BaseURL      string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountID    string
	IDToken      string

	store credentialStore
}

type Resolver struct {
	homeDir         string
	platform        string
	disableKeychain bool
	env             map[string]string
	runCommand      CommandRunner
	httpClient      *http.Client
	now             func() time.Time
	cacheTTL        time.Duration

	mu              sync.Mutex
	credentialCache map[string]cachedCredential
	keychainCache   map[keychainLookup]cachedKeychainValue
	refreshes       map[string]*refreshCall
}

type credentialStore struct {
	kind         string
	path         string
	service      string
	account      string
	root         map[string]any
	fingerprint  fileFingerprint
	fingerprints []fileFingerprint
}

type cachedCredential struct {
	credential   Credential
	expiresAt    time.Time
	fingerprints []fileFingerprint
}

type claudeSettings struct {
	env         map[string]string
	model       string
	fingerprint fileFingerprint
	ok          bool
}

type localProviderPreferences struct {
	HiddenModels map[string]map[string]bool `json:"hidden_models"`
}

type keychainLookup struct {
	service string
	account string
}

type cachedKeychainValue struct {
	value     string
	ok        bool
	expiresAt time.Time
}

type refreshCall struct {
	done       chan struct{}
	credential Credential
	err        error
}

type fileFingerprint struct {
	path    string
	size    int64
	modTime time.Time
	ok      bool
}

func NewResolver(options Options) *Resolver {
	homeDir := strings.TrimSpace(options.HomeDir)
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	platform := strings.TrimSpace(options.Platform)
	if platform == "" {
		platform = runtime.GOOS
	}
	runCommand := options.CommandRunner
	if runCommand == nil {
		runCommand = defaultCommandRunner
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	cacheTTL := options.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultResolverCacheTTL
	}
	return &Resolver{
		homeDir:         homeDir,
		platform:        platform,
		disableKeychain: options.DisableKeychain,
		env:             options.Env,
		runCommand:      runCommand,
		httpClient:      httpClient,
		now:             now,
		cacheTTL:        cacheTTL,
		credentialCache: make(map[string]cachedCredential),
		keychainCache:   make(map[keychainLookup]cachedKeychainValue),
		refreshes:       make(map[string]*refreshCall),
	}
}

func (r *Resolver) ProviderStatuses(ctx context.Context) []ProviderStatus {
	resolver := r.ensure()
	statuses := make([]ProviderStatus, 0, 2)
	if credential, err := resolver.Resolve(ctx, ClaudeCodeProviderID, ResolveOptions{}); err == nil {
		statuses = append(statuses, ProviderStatus{
			ID:          ClaudeCodeProviderID,
			DisplayName: ClaudeCodeDisplayName,
			Provider:    ClaudeCodeProviderKind,
			AuthMode:    credential.AuthMode,
			Models:      resolver.applyModelPreferences(ClaudeCodeProviderID, resolver.claudeCodeModels()),
		})
	}
	if credential, err := resolver.Resolve(ctx, CodexProviderID, ResolveOptions{}); err == nil {
		statuses = append(statuses, ProviderStatus{
			ID:          CodexProviderID,
			DisplayName: CodexDisplayName,
			Provider:    CodexProviderKind,
			AuthMode:    credential.AuthMode,
			Models:      resolver.applyModelPreferences(CodexProviderID, resolver.codexModels()),
		})
	}
	return statuses
}

func (r *Resolver) codexModels() []Model {
	for _, path := range r.codexModelCatalogPaths() {
		if models := readCodexModelCatalog(path); len(models) > 0 {
			return models
		}
	}
	return CodexModels()
}

func (r *Resolver) claudeCodeModels() []Model {
	settings := r.readClaudeSettings()
	if models := r.claudeConfiguredModels(settings); len(models) > 0 {
		return models
	}
	if models := r.claudeGatewayModels(settings); len(models) > 0 {
		return models
	}
	return applyClaudeSelectedModel(ClaudeCodeModels(), settings.model)
}

func (r *Resolver) SetModelVisible(providerID string, modelID string, visible bool) error {
	resolver := r.ensure()
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return fmt.Errorf("%w: empty local provider model", ErrCredentialUnavailable)
	}
	prefs := resolver.readLocalProviderPreferences()
	if prefs.HiddenModels == nil {
		prefs.HiddenModels = map[string]map[string]bool{}
	}
	if visible {
		if models := prefs.HiddenModels[providerID]; models != nil {
			delete(models, modelID)
			if len(models) == 0 {
				delete(prefs.HiddenModels, providerID)
			}
		}
	} else {
		if prefs.HiddenModels[providerID] == nil {
			prefs.HiddenModels[providerID] = map[string]bool{}
		}
		prefs.HiddenModels[providerID][modelID] = true
	}
	return resolver.writeLocalProviderPreferences(prefs)
}

func (r *Resolver) applyModelPreferences(providerID string, models []Model) []Model {
	prefs := r.readLocalProviderPreferences()
	hidden := prefs.HiddenModels[providerID]
	if len(hidden) == 0 {
		return models
	}
	firstVisible := -1
	hasVisibleDefault := false
	for index := range models {
		models[index].Hidden = hidden[models[index].ID]
		if models[index].Hidden {
			models[index].Default = false
			continue
		}
		if firstVisible < 0 {
			firstVisible = index
		}
		if models[index].Default {
			hasVisibleDefault = true
		}
	}
	if !hasVisibleDefault && firstVisible >= 0 {
		models[firstVisible].Default = true
	}
	return models
}

func (r *Resolver) Resolve(ctx context.Context, providerID string, options ResolveOptions) (Credential, error) {
	resolver := r.ensure()
	providerID = strings.TrimSpace(providerID)
	credential, err := resolver.resolveProvider(ctx, providerID, false)
	if err != nil {
		return Credential{}, err
	}
	if !options.Refresh || !resolver.needsOAuthRefresh(credential) {
		return credential, nil
	}

	resolver.invalidateProvider(providerID)
	if fresh, freshErr := resolver.resolveProvider(ctx, providerID, true); freshErr == nil {
		if !resolver.needsOAuthRefresh(fresh) {
			return fresh, nil
		}
		credential = fresh
	}

	refreshed, err := resolver.refreshOAuthDedup(ctx, credential)
	if err == nil {
		resolver.storeCachedCredential(refreshed)
		return refreshed, nil
	}

	resolver.invalidateProvider(providerID)
	if fresh, freshErr := resolver.resolveProvider(ctx, providerID, true); freshErr == nil && !resolver.needsOAuthRefresh(fresh) {
		return fresh, nil
	}
	return Credential{}, fmt.Errorf("%w: oauth refresh failed", ErrCredentialUnavailable)
}

func (r *Resolver) resolveProvider(ctx context.Context, providerID string, forceRead bool) (Credential, error) {
	switch providerID {
	case ClaudeCodeProviderID:
		if !forceRead {
			if credential, ok := r.cachedCredential(providerID); ok {
				return credential, nil
			}
		}
		credential, err := r.resolveClaudeCode(ctx)
		if err != nil {
			return Credential{}, err
		}
		r.storeCachedCredential(credential)
		return credential, nil
	case CodexProviderID:
		if !forceRead {
			if credential, ok := r.cachedCredential(providerID); ok {
				return credential, nil
			}
		}
		credential, err := r.resolveCodex(ctx)
		if err != nil {
			return Credential{}, err
		}
		r.storeCachedCredential(credential)
		return credential, nil
	default:
		return Credential{}, fmt.Errorf("%w: %s", ErrCredentialUnavailable, providerID)
	}
}

func (r *Resolver) ensure() *Resolver {
	if r != nil {
		return r
	}
	return NewResolver(Options{})
}

func (r *Resolver) needsOAuthRefresh(credential Credential) bool {
	if credential.AuthMode != AuthModeOAuth || strings.TrimSpace(credential.RefreshToken) == "" {
		return false
	}
	return !credential.ExpiresAt.IsZero() && !r.now().Add(oauthRefreshLeeway).Before(credential.ExpiresAt)
}

func (r *Resolver) cachedCredential(providerID string) (Credential, bool) {
	r.mu.Lock()
	entry, ok := r.credentialCache[providerID]
	if !ok {
		r.mu.Unlock()
		return Credential{}, false
	}
	if !r.now().Before(entry.expiresAt) {
		delete(r.credentialCache, providerID)
		r.mu.Unlock()
		return Credential{}, false
	}
	r.mu.Unlock()

	for _, fingerprint := range entry.fingerprints {
		if fingerprint.ok && !fingerprint.matches() {
			r.invalidateProvider(providerID)
			return Credential{}, false
		}
	}
	return entry.credential, true
}

func (r *Resolver) storeCachedCredential(credential Credential) {
	if strings.TrimSpace(credential.ProviderID) == "" {
		return
	}
	r.mu.Lock()
	r.credentialCache[credential.ProviderID] = cachedCredential{
		credential:   credential,
		expiresAt:    r.now().Add(r.cacheTTL),
		fingerprints: credential.store.allFingerprints(),
	}
	r.mu.Unlock()
}

func (r *Resolver) invalidateProvider(providerID string) {
	r.mu.Lock()
	delete(r.credentialCache, providerID)
	r.keychainCache = make(map[keychainLookup]cachedKeychainValue)
	r.mu.Unlock()
}

func (r *Resolver) claudeEnvValue(settings claudeSettings, key string) (string, fileFingerprint) {
	if value := strings.TrimSpace(r.getenv(key)); value != "" {
		return value, fileFingerprint{}
	}
	if !settings.ok {
		return "", fileFingerprint{}
	}
	return strings.TrimSpace(settings.env[key]), settings.fingerprint
}

func (r *Resolver) applyClaudeEnvironment(credential Credential, settings claudeSettings) Credential {
	if baseURL, fingerprint := r.claudeEnvValue(settings, claudeAnthropicBaseURLEnv); baseURL != "" {
		credential.BaseURL = baseURL
		credential.store.addFingerprint(fingerprint)
	}
	return credential
}

func (r *Resolver) resolveClaudeCode(ctx context.Context) (Credential, error) {
	settings := r.readClaudeSettings()
	if token, fingerprint := r.claudeEnvValue(settings, claudeAnthropicAuthEnv); token != "" {
		credential := Credential{ProviderID: ClaudeCodeProviderID, ProviderKind: ClaudeCodeProviderKind, AuthMode: AuthModeOAuth, AccessToken: token}
		credential.store.addFingerprint(fingerprint)
		return r.applyClaudeEnvironment(credential, settings), nil
	}
	if token, fingerprint := r.claudeEnvValue(settings, claudeOAuthTokenEnv); token != "" {
		credential := Credential{ProviderID: ClaudeCodeProviderID, ProviderKind: ClaudeCodeProviderKind, AuthMode: AuthModeOAuth, AccessToken: token}
		credential.store.addFingerprint(fingerprint)
		return r.applyClaudeEnvironment(credential, settings), nil
	}
	if key, fingerprint := r.claudeEnvValue(settings, claudeAnthropicAPIKeyEnv); key != "" {
		credential := Credential{ProviderID: ClaudeCodeProviderID, ProviderKind: ClaudeCodeProviderKind, AuthMode: AuthModeAPIKey, APIKey: key}
		credential.store.addFingerprint(fingerprint)
		return r.applyClaudeEnvironment(credential, settings), nil
	}
	if r.claudeAPIKeyHelperConfigured() {
		return Credential{}, fmt.Errorf("%w: %s apiKeyHelper configured", ErrCredentialUnavailable, ClaudeCodeProviderID)
	}
	if oauth, ok := r.readClaudeOAuth(ctx); ok {
		return r.applyClaudeEnvironment(oauth, settings), nil
	}
	if credential, ok := r.readClaudeAPIKey(ctx); ok {
		return r.applyClaudeEnvironment(credential, settings), nil
	}
	return Credential{}, fmt.Errorf("%w: %s", ErrCredentialUnavailable, ClaudeCodeProviderID)
}

func (r *Resolver) resolveCodex(ctx context.Context) (Credential, error) {
	if key := strings.TrimSpace(r.getenv(codexAPIKeyEnv)); key != "" {
		return Credential{ProviderID: CodexProviderID, ProviderKind: CodexProviderKind, AuthMode: AuthModeAPIKey, APIKey: key}, nil
	}
	if credential, ok := r.readCodexAuth(ctx); ok {
		return credential, nil
	}
	return Credential{}, fmt.Errorf("%w: %s", ErrCredentialUnavailable, CodexProviderID)
}

func (r *Resolver) readClaudeOAuth(ctx context.Context) (Credential, bool) {
	if !r.disableKeychain && r.platform == "darwin" {
		service := r.claudeKeychainServiceName(claudeOAuthServiceSuffix)
		account := currentUsername()
		root, ok := r.readKeychainJSON(ctx, service, account)
		if ok {
			return parseClaudeOAuth(root, credentialStore{kind: "keychain", service: service, account: account, root: root})
		}
	}
	path := filepath.Join(r.claudeConfigDir(), ".credentials.json")
	root, fingerprint, ok := readJSONFileWithFingerprint(path)
	if !ok {
		return Credential{}, false
	}
	return parseClaudeOAuth(root, credentialStore{kind: "file", path: path, root: root, fingerprint: fingerprint})
}

func parseClaudeOAuth(root map[string]any, store credentialStore) (Credential, bool) {
	raw, _ := root["claudeAiOauth"].(map[string]any)
	if len(raw) == 0 {
		return Credential{}, false
	}
	access := stringValue(raw["accessToken"])
	if access == "" {
		return Credential{}, false
	}
	credential := Credential{
		ProviderID:   ClaudeCodeProviderID,
		ProviderKind: ClaudeCodeProviderKind,
		AuthMode:     AuthModeOAuth,
		AccessToken:  access,
		RefreshToken: stringValue(raw["refreshToken"]),
		ExpiresAt:    millisTime(raw["expiresAt"]),
		store:        store,
	}
	return credential, true
}

func (r *Resolver) readClaudeAPIKey(ctx context.Context) (Credential, bool) {
	if !r.disableKeychain && r.platform == "darwin" {
		if key, ok := r.readKeychainString(ctx, r.claudeKeychainServiceName(""), currentUsername()); ok {
			return Credential{
				ProviderID:   ClaudeCodeProviderID,
				ProviderKind: ClaudeCodeProviderKind,
				AuthMode:     AuthModeAPIKey,
				APIKey:       key,
				store: credentialStore{
					kind:    "keychain",
					service: r.claudeKeychainServiceName(""),
					account: currentUsername(),
				},
			}, true
		}
	}
	for _, path := range r.claudeGlobalConfigPaths() {
		root, fingerprint, ok := readJSONFileWithFingerprint(path)
		if !ok {
			continue
		}
		if key := stringValue(root["primaryApiKey"]); key != "" {
			return Credential{
				ProviderID:   ClaudeCodeProviderID,
				ProviderKind: ClaudeCodeProviderKind,
				AuthMode:     AuthModeAPIKey,
				APIKey:       key,
				store:        credentialStore{kind: "file", path: path, root: root, fingerprint: fingerprint},
			}, true
		}
	}
	return Credential{}, false
}

func (r *Resolver) readCodexAuth(ctx context.Context) (Credential, bool) {
	codexHome := r.codexHome()
	if !r.disableKeychain && r.platform == "darwin" {
		account := codexKeychainAccount(codexHome)
		if root, ok := r.readKeychainJSON(ctx, codexAuthService, account); ok {
			if credential, ok := parseCodexAuth(root, credentialStore{kind: "keychain", service: codexAuthService, account: account, root: root}); ok {
				return credential, true
			}
		}
	}
	path := filepath.Join(codexHome, "auth.json")
	root, fingerprint, ok := readJSONFileWithFingerprint(path)
	if !ok {
		return Credential{}, false
	}
	return parseCodexAuth(root, credentialStore{kind: "file", path: path, root: root, fingerprint: fingerprint})
}

func parseCodexAuth(root map[string]any, store credentialStore) (Credential, bool) {
	mode := strings.ToLower(strings.TrimSpace(stringValue(root["auth_mode"])))
	apiKey := stringValue(root["OPENAI_API_KEY"])
	if mode == "" && apiKey != "" {
		mode = "apikey"
	}
	switch mode {
	case "apikey", "api_key":
		if apiKey == "" {
			return Credential{}, false
		}
		return Credential{ProviderID: CodexProviderID, ProviderKind: CodexProviderKind, AuthMode: AuthModeAPIKey, APIKey: apiKey, store: store}, true
	case "", "chatgpt", "chatgptauthtokens":
		rawTokens, _ := root["tokens"].(map[string]any)
		access := stringValue(rawTokens["access_token"])
		refresh := stringValue(rawTokens["refresh_token"])
		if access == "" {
			return Credential{}, false
		}
		accountID := stringValue(rawTokens["account_id"])
		if accountID == "" {
			accountID = codexAccountIDFromToken(access)
		}
		return Credential{
			ProviderID:   CodexProviderID,
			ProviderKind: CodexProviderKind,
			AuthMode:     AuthModeOAuth,
			AccessToken:  access,
			RefreshToken: refresh,
			ExpiresAt:    jwtExpiry(access).Add(0),
			AccountID:    accountID,
			IDToken:      stringValue(rawTokens["id_token"]),
			store:        store,
		}, true
	default:
		return Credential{}, false
	}
}

func (r *Resolver) refreshOAuth(ctx context.Context, credential Credential) (Credential, error) {
	switch credential.ProviderID {
	case ClaudeCodeProviderID:
		return r.refreshClaudeOAuth(ctx, credential)
	case CodexProviderID:
		return r.refreshCodexOAuth(ctx, credential)
	default:
		return Credential{}, fmt.Errorf("%w: %s", ErrCredentialUnavailable, credential.ProviderID)
	}
}

func (r *Resolver) refreshOAuthDedup(ctx context.Context, credential Credential) (Credential, error) {
	key := credential.ProviderID
	r.mu.Lock()
	if call := r.refreshes[key]; call != nil {
		r.mu.Unlock()
		select {
		case <-call.done:
			return call.credential, call.err
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	r.refreshes[key] = call
	r.mu.Unlock()

	call.credential, call.err = r.refreshOAuthWithRaceCheck(ctx, credential)

	r.mu.Lock()
	delete(r.refreshes, key)
	close(call.done)
	r.mu.Unlock()
	return call.credential, call.err
}

func (r *Resolver) refreshOAuthWithRaceCheck(ctx context.Context, credential Credential) (Credential, error) {
	r.invalidateProvider(credential.ProviderID)
	if fresh, err := r.resolveProvider(ctx, credential.ProviderID, true); err == nil {
		if !r.needsOAuthRefresh(fresh) {
			return fresh, nil
		}
		credential = fresh
	}
	refreshed, err := r.refreshOAuth(ctx, credential)
	if err == nil {
		return refreshed, nil
	}
	r.invalidateProvider(credential.ProviderID)
	if fresh, freshErr := r.resolveProvider(ctx, credential.ProviderID, true); freshErr == nil && !r.needsOAuthRefresh(fresh) {
		return fresh, nil
	}
	return Credential{}, err
}

func (r *Resolver) refreshClaudeOAuth(ctx context.Context, credential Credential) (Credential, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     claudeOAuthClientID,
		"refresh_token": credential.RefreshToken,
		"scope":         claudeOAuthScopes,
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := r.postJSON(ctx, claudeOAuthTokenEndpoint, payload, &out); err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(out.AccessToken) == "" || strings.TrimSpace(out.RefreshToken) == "" {
		return Credential{}, errors.New("claude oauth refresh response missing token")
	}
	credential.AccessToken = strings.TrimSpace(out.AccessToken)
	credential.RefreshToken = strings.TrimSpace(out.RefreshToken)
	credential.ExpiresAt = r.now().Add(time.Duration(out.ExpiresIn)*time.Second - oauthRefreshLeeway)
	if err := r.writeClaudeOAuth(ctx, credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (r *Resolver) refreshCodexOAuth(ctx context.Context, credential Credential) (Credential, error) {
	values := url.Values{
		"grant_type":    []string{"refresh_token"},
		"refresh_token": []string{credential.RefreshToken},
		"client_id":     []string{codexOAuthClientID},
	}.Encode()
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexOAuthTokenEndpoint, strings.NewReader(values))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.doJSON(req, &out); err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(out.AccessToken) == "" || strings.TrimSpace(out.RefreshToken) == "" {
		return Credential{}, errors.New("codex oauth refresh response missing token")
	}
	credential.AccessToken = strings.TrimSpace(out.AccessToken)
	credential.RefreshToken = strings.TrimSpace(out.RefreshToken)
	if strings.TrimSpace(out.IDToken) != "" {
		credential.IDToken = strings.TrimSpace(out.IDToken)
	}
	if exp := jwtExpiry(credential.AccessToken); !exp.IsZero() {
		credential.ExpiresAt = exp
	} else if out.ExpiresIn > 0 {
		credential.ExpiresAt = r.now().Add(time.Duration(out.ExpiresIn) * time.Second)
	} else {
		credential.ExpiresAt = r.now().Add(defaultRefreshExpiry)
	}
	if accountID := codexAccountIDFromToken(credential.AccessToken); accountID != "" {
		credential.AccountID = accountID
	}
	if err := r.writeCodexOAuth(ctx, credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (r *Resolver) writeClaudeOAuth(ctx context.Context, credential Credential) error {
	root := cloneMap(credential.store.root)
	raw, _ := root["claudeAiOauth"].(map[string]any)
	if raw == nil {
		return errors.New("claude oauth store missing existing oauth object")
	}
	raw["accessToken"] = credential.AccessToken
	raw["refreshToken"] = credential.RefreshToken
	raw["expiresAt"] = credential.ExpiresAt.UnixMilli()
	root["claudeAiOauth"] = raw
	if err := r.writeStore(ctx, credential.store, root); err != nil {
		return err
	}
	r.invalidateProvider(credential.ProviderID)
	return nil
}

func (r *Resolver) writeCodexOAuth(ctx context.Context, credential Credential) error {
	root := cloneMap(credential.store.root)
	raw, _ := root["tokens"].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
	}
	raw["access_token"] = credential.AccessToken
	raw["refresh_token"] = credential.RefreshToken
	if credential.IDToken != "" {
		raw["id_token"] = credential.IDToken
	}
	if credential.AccountID != "" {
		raw["account_id"] = credential.AccountID
	}
	root["tokens"] = raw
	root["last_refresh"] = r.now().UTC().Format(time.RFC3339)
	if root["auth_mode"] == nil {
		root["auth_mode"] = "chatgpt"
	}
	if err := r.writeStore(ctx, credential.store, root); err != nil {
		return err
	}
	r.invalidateProvider(credential.ProviderID)
	return nil
}

func (r *Resolver) writeStore(ctx context.Context, store credentialStore, root map[string]any) error {
	switch store.kind {
	case "file":
		return writeJSONFile(store.path, root)
	case "keychain":
		encoded, err := json.Marshal(root)
		if err != nil {
			return err
		}
		hexValue := hex.EncodeToString(encoded)
		args := []string{"add-generic-password", "-U", "-s", store.service, "-X", hexValue}
		if strings.TrimSpace(store.account) != "" {
			args = []string{"add-generic-password", "-U", "-s", store.service, "-a", store.account, "-X", hexValue}
		}
		_, err = r.runCommand(ctx, "security", args...)
		return err
	default:
		return errors.New("credential store is not writable")
	}
}

func (r *Resolver) postJSON(ctx context.Context, url string, payload any, out any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return r.doJSON(req, out)
}

func (r *Resolver) doJSON(req *http.Request, out any) error {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("oauth refresh failed: status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func (r *Resolver) claudeConfigDir() string {
	if configured := strings.TrimSpace(r.getenv(claudeConfigDirEnv)); configured != "" {
		return expandHome(configured, r.homeDir)
	}
	return filepath.Join(r.homeDir, ".claude")
}

func (r *Resolver) claudeGlobalConfigPaths() []string {
	configDir := r.claudeConfigDir()
	if strings.TrimSpace(r.getenv(claudeConfigDirEnv)) != "" {
		return []string{
			filepath.Join(configDir, ".config.json"),
			filepath.Join(configDir, ".claude.json"),
		}
	}
	return []string{
		filepath.Join(configDir, ".config.json"),
		filepath.Join(r.homeDir, ".claude.json"),
	}
}

func (r *Resolver) claudeSettingsPaths() []string {
	return []string{filepath.Join(r.claudeConfigDir(), "settings.json")}
}

func (r *Resolver) readClaudeSettings() claudeSettings {
	for _, path := range r.claudeSettingsPaths() {
		root, fingerprint, ok := readJSONFileWithFingerprint(path)
		if !ok {
			continue
		}
		settings := claudeSettings{
			env:         map[string]string{},
			model:       stringValue(root["model"]),
			fingerprint: fingerprint,
			ok:          true,
		}
		rawEnv, _ := root["env"].(map[string]any)
		for key, value := range rawEnv {
			if text := stringValue(value); text != "" {
				settings.env[key] = text
			}
		}
		return settings
	}
	return claudeSettings{env: map[string]string{}}
}

func (r *Resolver) claudeConfiguredModels(settings claudeSettings) []Model {
	keys := []string{
		claudeModelEnv,
		claudeReasoningModelEnv,
		claudeDefaultOpusEnv,
		claudeDefaultSonnetEnv,
		claudeDefaultHaikuEnv,
	}
	models := make([]Model, 0, len(keys))
	seen := make(map[string]int, len(keys))
	for _, key := range keys {
		id, _ := r.claudeEnvValue(settings, key)
		if id == "" {
			continue
		}
		if index, exists := seen[id]; exists {
			models[index].Reasoning = true
			continue
		}
		seen[id] = len(models)
		models = append(models, Model{
			ID:              id,
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Default:         len(models) == 0,
			Priority:        1000 - len(models)*10,
		})
	}
	return models
}

func (r *Resolver) claudeGatewayModels(settings claudeSettings) []Model {
	baseURL, _ := r.claudeEnvValue(settings, claudeAnthropicBaseURLEnv)
	if baseURL == "" {
		return nil
	}
	path := filepath.Join(r.claudeConfigDir(), "cache", "gateway-models.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache struct {
		BaseURL string `json:"baseUrl"`
		Models  []struct {
			ID              string `json:"id"`
			ContextLength   int    `json:"context_length"`
			MaxOutputTokens int    `json:"max_output_tokens"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil
	}
	if normalizeClaudeBaseURL(cache.BaseURL) != normalizeClaudeBaseURL(baseURL) {
		return nil
	}
	models := make([]Model, 0, len(cache.Models))
	seen := make(map[string]struct{}, len(cache.Models))
	for _, entry := range cache.Models {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		contextLength := entry.ContextLength
		if contextLength <= 0 {
			contextLength = 200000
		}
		maxOutputTokens := entry.MaxOutputTokens
		if maxOutputTokens <= 0 {
			maxOutputTokens = 8192
		}
		models = append(models, Model{
			ID:              id,
			ContextLength:   contextLength,
			MaxOutputTokens: maxOutputTokens,
			ToolCalling:     true,
			Reasoning:       true,
			Default:         len(models) == 0,
			Priority:        1000 - len(models)*10,
		})
	}
	return applyClaudeSelectedModel(models, settings.model)
}

func (r *Resolver) claudeAPIKeyHelperConfigured() bool {
	for _, path := range r.claudeSettingsPaths() {
		root, ok := readJSONFile(path)
		if !ok {
			continue
		}
		if stringValue(root[claudeAPIKeyHelperSetting]) != "" {
			return true
		}
	}
	for _, path := range r.claudeGlobalConfigPaths() {
		root, ok := readJSONFile(path)
		if !ok {
			continue
		}
		if stringValue(root[claudeAPIKeyHelperSetting]) != "" {
			return true
		}
	}
	return false
}

func (r *Resolver) claudeKeychainServiceName(serviceSuffix string) string {
	if strings.TrimSpace(r.getenv(claudeConfigDirEnv)) == "" {
		return claudeServiceBase + serviceSuffix
	}
	sum := sha256.Sum256([]byte(r.claudeConfigDir()))
	return claudeServiceBase + serviceSuffix + "-" + hex.EncodeToString(sum[:])[:8]
}

func (r *Resolver) codexHome() string {
	if configured := strings.TrimSpace(r.getenv(codexHomeEnv)); configured != "" {
		return realpathOrSelf(expandHome(configured, r.homeDir))
	}
	return realpathOrSelf(filepath.Join(r.homeDir, ".codex"))
}

func (r *Resolver) arkloopDataDir() string {
	if configured := strings.TrimSpace(r.getenv(arkloopDataDirEnv)); configured != "" {
		return expandHome(configured, r.homeDir)
	}
	return filepath.Join(r.homeDir, ".arkloop")
}

func (r *Resolver) localProviderPreferencesPath() string {
	return filepath.Join(r.arkloopDataDir(), localProviderPrefsFile)
}

func (r *Resolver) readLocalProviderPreferences() localProviderPreferences {
	raw, err := os.ReadFile(r.localProviderPreferencesPath())
	if err != nil {
		return localProviderPreferences{HiddenModels: map[string]map[string]bool{}}
	}
	var prefs localProviderPreferences
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return localProviderPreferences{HiddenModels: map[string]map[string]bool{}}
	}
	if prefs.HiddenModels == nil {
		prefs.HiddenModels = map[string]map[string]bool{}
	}
	return prefs
}

func (r *Resolver) writeLocalProviderPreferences(prefs localProviderPreferences) error {
	path := r.localProviderPreferencesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func (r *Resolver) codexModelCatalogPaths() []string {
	codexHome := r.codexHome()
	paths, _ := filepath.Glob(filepath.Join(codexHome, "model-catalog*.json"))
	sort.SliceStable(paths, func(i, j int) bool {
		left, leftErr := os.Stat(paths[i])
		right, rightErr := os.Stat(paths[j])
		if leftErr == nil && rightErr == nil {
			return left.ModTime().After(right.ModTime())
		}
		return paths[i] > paths[j]
	})
	if _, err := os.Stat(filepath.Join(codexHome, "models_cache.json")); err == nil {
		paths = append(paths, filepath.Join(codexHome, "models_cache.json"))
	}
	return paths
}

type codexModelCatalogFile struct {
	Models []codexModelCatalogEntry `json:"models"`
}

type codexModelCatalogEntry struct {
	Slug               string `json:"slug"`
	ContextWindow      int    `json:"context_window"`
	MaxContextWindow   int    `json:"max_context_window"`
	MaxOutputTokens    int    `json:"max_output_tokens"`
	Priority           *int   `json:"priority"`
	Visibility         string `json:"visibility"`
	SupportedReasoning []any  `json:"supported_reasoning_levels"`
}

func readCodexModelCatalog(path string) []Model {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var catalog codexModelCatalogFile
	if err := json.Unmarshal(raw, &catalog); err != nil || len(catalog.Models) == 0 {
		return nil
	}
	entries := append([]codexModelCatalogEntry(nil), catalog.Models...)
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := codexCatalogPriority(entries[i]), codexCatalogPriority(entries[j])
		if left == right {
			return entries[i].Slug < entries[j].Slug
		}
		return left < right
	})
	models := make([]Model, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.Slug)
		if id == "" || strings.EqualFold(strings.TrimSpace(entry.Visibility), "hide") {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		contextLength := entry.ContextWindow
		if contextLength <= 0 {
			contextLength = entry.MaxContextWindow
		}
		if contextLength <= 0 {
			contextLength = 272000
		}
		models = append(models, Model{
			ID:              id,
			ContextLength:   contextLength,
			MaxOutputTokens: entry.MaxOutputTokens,
			ToolCalling:     true,
			Reasoning:       codexCatalogReasoning(entry),
			Default:         len(models) == 0,
			Priority:        1000 - len(models)*10,
		})
	}
	return models
}

func codexCatalogPriority(entry codexModelCatalogEntry) int {
	if entry.Priority == nil {
		return 1_000_000
	}
	return *entry.Priority
}

func codexCatalogReasoning(entry codexModelCatalogEntry) bool {
	if len(entry.SupportedReasoning) > 0 {
		return true
	}
	return strings.HasPrefix(entry.Slug, "gpt-5")
}

func (r *Resolver) readKeychainJSON(ctx context.Context, service string, account string) (map[string]any, bool) {
	text, ok := r.readKeychainString(ctx, service, account)
	if !ok {
		return nil, false
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return nil, false
	}
	return root, true
}

func (r *Resolver) readKeychainString(ctx context.Context, service string, account string) (string, bool) {
	lookup := keychainLookup{service: service, account: account}
	r.mu.Lock()
	if cached, ok := r.keychainCache[lookup]; ok && r.now().Before(cached.expiresAt) {
		r.mu.Unlock()
		return cached.value, cached.ok
	}
	r.mu.Unlock()

	args := []string{"find-generic-password", "-s", service, "-w"}
	if strings.TrimSpace(account) != "" {
		args = []string{"find-generic-password", "-s", service, "-a", account, "-w"}
	}
	text, err := r.runCommand(ctx, "security", args...)
	if err != nil {
		r.mu.Lock()
		if cached, ok := r.keychainCache[lookup]; ok && cached.ok {
			cached.expiresAt = r.now().Add(r.cacheTTL)
			r.keychainCache[lookup] = cached
			r.mu.Unlock()
			return cached.value, true
		}
		r.keychainCache[lookup] = cachedKeychainValue{ok: false, expiresAt: r.now().Add(r.cacheTTL)}
		r.mu.Unlock()
		return "", false
	}
	text = strings.TrimSpace(text)
	ok := text != ""
	r.mu.Lock()
	r.keychainCache[lookup] = cachedKeychainValue{value: text, ok: ok, expiresAt: r.now().Add(r.cacheTTL)}
	r.mu.Unlock()
	return text, ok
}

func (r *Resolver) getenv(key string) string {
	if r.env != nil {
		return r.env[key]
	}
	return os.Getenv(key)
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func readJSONFile(path string) (map[string]any, bool) {
	root, _, ok := readJSONFileWithFingerprint(path)
	return root, ok
}

func readJSONFileWithFingerprint(path string) (map[string]any, fileFingerprint, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fileFingerprint{}, false
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fileFingerprint{}, false
	}
	fingerprint, ok := fingerprintFile(path)
	if !ok {
		return nil, fileFingerprint{}, false
	}
	return root, fingerprint, true
}

func writeJSONFile(path string, root map[string]any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func fingerprintFile(path string) (fileFingerprint, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fileFingerprint{}, false
	}
	return fileFingerprint{
		path:    path,
		size:    info.Size(),
		modTime: info.ModTime(),
		ok:      true,
	}, true
}

func (f fileFingerprint) matches() bool {
	if !f.ok {
		return true
	}
	current, ok := fingerprintFile(f.path)
	if !ok {
		return false
	}
	return current.size == f.size && current.modTime.Equal(f.modTime)
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func millisTime(value any) time.Time {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return time.UnixMilli(int64(v))
		}
	case int64:
		if v > 0 {
			return time.UnixMilli(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			return time.UnixMilli(n)
		}
	}
	return time.Time{}
}

func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var payload struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(payload.Exp, 0)
}

func codexAccountIDFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	claims, _ := payload["https://api.openai.com/auth"].(map[string]any)
	return stringValue(claims["chatgpt_account_id"])
}

func codexKeychainAccount(codexHome string) string {
	sum := sha256.Sum256([]byte(codexHome))
	return "cli|" + hex.EncodeToString(sum[:])[:16]
}

func realpathOrSelf(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func expandHome(path string, homeDir string) string {
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func normalizeClaudeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func applyClaudeSelectedModel(models []Model, selected string) []Model {
	selected = strings.ToLower(strings.TrimSpace(selected))
	if selected == "" || len(models) == 0 {
		return models
	}
	match := -1
	for index, model := range models {
		if claudeModelMatchesSelection(model.ID, selected) {
			match = index
			break
		}
	}
	if match < 0 {
		return models
	}
	for index := range models {
		models[index].Default = index == match
	}
	return models
}

func claudeModelMatchesSelection(modelID string, selected string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == selected {
		return true
	}
	switch selected {
	case "opus", "sonnet", "haiku":
		return strings.Contains(modelID, selected)
	default:
		return false
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if nested, ok := v.(map[string]any); ok {
			out[k] = cloneMap(nested)
		} else {
			out[k] = v
		}
	}
	return out
}

func (s *credentialStore) addFingerprint(fingerprint fileFingerprint) {
	if fingerprint.ok {
		s.fingerprints = append(s.fingerprints, fingerprint)
	}
}

func (s credentialStore) allFingerprints() []fileFingerprint {
	fingerprints := make([]fileFingerprint, 0, 1+len(s.fingerprints))
	if s.fingerprint.ok {
		fingerprints = append(fingerprints, s.fingerprint)
	}
	for _, fingerprint := range s.fingerprints {
		if fingerprint.ok {
			fingerprints = append(fingerprints, fingerprint)
		}
	}
	return fingerprints
}

func currentUsername() string {
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user
	}
	if current, err := osuser.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		return strings.TrimSpace(current.Username)
	}
	return ""
}
