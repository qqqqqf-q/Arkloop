package llmproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrNotConfigured = errors.New("llm providers service not configured")

type Provider struct {
	Credential data.LlmCredential
	Models     []data.LlmRoute
}

type ProviderModelTestConfig struct {
	Credential data.LlmCredential
	Model      data.LlmRoute
	APIKey     string
}

type AvailableModel struct {
	ID                 string
	Name               string
	Configured         bool
	Type               string // "chat", "embedding", "moderation", "image", "audio", "other"
	ContextLength      *int
	MaxOutputTokens    *int
	InputModalities    []string // e.g. ["text","image"]
	OutputModalities   []string // e.g. ["text"] or ["embedding"]
	ToolCalling        *bool
	Reasoning          *bool
	DefaultTemperature *float64
}

type CreateProviderInput struct {
	Provider      string
	Name          string
	APIKey        string
	BaseURL       *string
	OpenAIAPIMode *string
	AdvancedJSON  map[string]any
}

type UpdateProviderInput struct {
	Provider         *string
	Name             *string
	BaseURLSet       bool
	BaseURL          *string
	OpenAIAPIModeSet bool
	OpenAIAPIMode    *string
	AdvancedJSONSet  bool
	AdvancedJSON     map[string]any
	APIKey           *string
}

type CreateModelInput struct {
	Model               string
	Priority            int
	IsDefault           bool
	ShowInPicker        bool
	Tags                []string
	WhenJSON            json.RawMessage
	AdvancedJSON        map[string]any
	Multiplier          *float64
	CostPer1kInput      *float64
	CostPer1kOutput     *float64
	CostPer1kCacheWrite *float64
	CostPer1kCacheRead  *float64
}

type UpdateModelInput struct {
	ModelSet               bool
	Model                  *string
	PrioritySet            bool
	Priority               *int
	IsDefaultSet           bool
	IsDefault              *bool
	ShowInPickerSet        bool
	ShowInPicker           *bool
	TagsSet                bool
	Tags                   []string
	WhenJSONSet            bool
	WhenJSON               json.RawMessage
	AdvancedJSONSet        bool
	AdvancedJSON           map[string]any
	MultiplierSet          bool
	Multiplier             *float64
	CostPer1kInputSet      bool
	CostPer1kInput         *float64
	CostPer1kOutputSet     bool
	CostPer1kOutput        *float64
	CostPer1kCacheWriteSet bool
	CostPer1kCacheWrite    *float64
	CostPer1kCacheReadSet  bool
	CostPer1kCacheRead     *float64
}

type ProviderNotFoundError struct {
	ID uuid.UUID
}

func (e ProviderNotFoundError) Error() string {
	return fmt.Sprintf("provider %s not found", e.ID)
}

type ModelNotFoundError struct {
	ID uuid.UUID
}

func (e ModelNotFoundError) Error() string {
	return fmt.Sprintf("model %s not found", e.ID)
}

type ProviderSecretMissingError struct {
	ProviderID uuid.UUID
}

func (e ProviderSecretMissingError) Error() string {
	return fmt.Sprintf("provider %s secret missing", e.ProviderID)
}

type Service struct {
	pool                 data.TxStarter
	credentials          *data.LlmCredentialsRepository
	routes               *data.LlmRoutesRepository
	secrets              *data.SecretsRepository
	projects             *data.ProjectRepository
	availableModelsCache *availableModelsCache
}

func NewService(
	pool data.TxStarter,
	credentials *data.LlmCredentialsRepository,
	routes *data.LlmRoutesRepository,
	secrets *data.SecretsRepository,
	projects *data.ProjectRepository,
) *Service {
	return &Service{
		pool:                 pool,
		credentials:          credentials,
		routes:               routes,
		secrets:              secrets,
		projects:             projects,
		availableModelsCache: newAvailableModelsCache(defaultAvailableModelsCacheTTL),
	}
}

// resolveProjectID is retained for backward compatibility but always returns
// uuid.Nil. User/BYOK routes are now account-scoped, not project-bound.
func (s *Service) resolveProjectID(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (s *Service) ListProviders(ctx context.Context, accountID uuid.UUID, scope string, userID *uuid.UUID) ([]Provider, error) {
	if err := s.requireListReady(); err != nil {
		return nil, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	creds, err := s.credentials.ListByOwner(ctx, ownerKind, ownerUserID)
	if err != nil {
		return nil, err
	}
	routes, err := s.routes.ListByScope(ctx, accountID, scope)
	if err != nil {
		return nil, err
	}
	modelsByProvider := make(map[uuid.UUID][]data.LlmRoute, len(creds))
	for _, route := range routes {
		modelsByProvider[route.CredentialID] = append(modelsByProvider[route.CredentialID], route)
	}
	providers := make([]Provider, 0, len(creds))
	for _, cred := range creds {
		providers = append(providers, Provider{
			Credential: cred,
			Models:     modelsByProvider[cred.ID],
		})
	}
	return providers, nil
}

func (s *Service) GetProvider(ctx context.Context, accountID, providerID uuid.UUID, scope string, userID *uuid.UUID) (Provider, error) {
	if err := s.requireListReady(); err != nil {
		return Provider{}, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	cred, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return Provider{}, err
	}
	if cred == nil {
		return Provider{}, ProviderNotFoundError{ID: providerID}
	}
	models, err := s.routes.ListByCredential(ctx, accountID, providerID, scope)
	if err != nil {
		return Provider{}, err
	}
	return Provider{Credential: *cred, Models: models}, nil
}

func (s *Service) CreateProvider(ctx context.Context, accountID uuid.UUID, scope string, userID *uuid.UUID, input CreateProviderInput) (Provider, error) {
	if err := s.requireWriteReady(); err != nil {
		return Provider{}, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Provider{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	providerID := uuid.New()
	secretName := providerSecretName(providerID)
	secret, err := upsertProviderSecret(ctx, tx, s.secrets, ownerKind, ownerUserID, secretName, strings.TrimSpace(input.APIKey))
	if err != nil {
		return Provider{}, err
	}
	keyPrefix := computeKeyPrefix(input.APIKey)
	cred, err := s.credentials.WithTx(tx).Create(
		ctx,
		providerID,
		ownerKind,
		ownerUserID,
		strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Name),
		&secret.ID,
		&keyPrefix,
		input.BaseURL,
		input.OpenAIAPIMode,
		input.AdvancedJSON,
	)
	if err != nil {
		return Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Provider{}, err
	}
	s.availableModelsCache.invalidateProvider(providerID)
	return Provider{Credential: cred, Models: []data.LlmRoute{}}, nil
}

func (s *Service) UpdateProvider(ctx context.Context, accountID, providerID uuid.UUID, scope string, userID *uuid.UUID, input UpdateProviderInput) (Provider, error) {
	if err := s.requireWriteReady(); err != nil {
		return Provider{}, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	current, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return Provider{}, err
	}
	if current == nil {
		return Provider{}, ProviderNotFoundError{ID: providerID}
	}

	provider := current.Provider
	if input.Provider != nil {
		provider = strings.TrimSpace(*input.Provider)
	}
	name := current.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	baseURL := current.BaseURL
	if input.BaseURLSet {
		baseURL = input.BaseURL
	}
	openAIAPIMode := current.OpenAIAPIMode
	if input.OpenAIAPIModeSet {
		openAIAPIMode = input.OpenAIAPIMode
	}
	advancedJSON := current.AdvancedJSON
	if input.AdvancedJSONSet {
		advancedJSON = input.AdvancedJSON
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Provider{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if input.APIKey != nil {
		trimmedKey := strings.TrimSpace(*input.APIKey)
		secret, err := upsertProviderSecret(ctx, tx, s.secrets, ownerKind, ownerUserID, providerSecretName(providerID), trimmedKey)
		if err != nil {
			return Provider{}, err
		}
		keyPrefix := computeKeyPrefix(trimmedKey)
		if err := s.credentials.WithTx(tx).UpdateSecret(ctx, ownerKind, ownerUserID, providerID, &secret.ID, &keyPrefix); err != nil {
			return Provider{}, err
		}
	}

	if _, err := s.credentials.WithTx(tx).Update(ctx, ownerKind, ownerUserID, providerID, provider, name, baseURL, openAIAPIMode, advancedJSON); err != nil {
		return Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Provider{}, err
	}
	s.availableModelsCache.invalidateProvider(providerID)
	return s.GetProvider(ctx, accountID, providerID, scope, userID)
}

func (s *Service) DeleteProvider(ctx context.Context, accountID, providerID uuid.UUID, scope string, userID *uuid.UUID) error {
	if err := s.requireWriteReady(); err != nil {
		return err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	current, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return err
	}
	if current == nil {
		return ProviderNotFoundError{ID: providerID}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := s.credentials.WithTx(tx).Delete(ctx, ownerKind, ownerUserID, providerID); err != nil {
		return err
	}
	if current.SecretID != nil {
		if err := deleteProviderSecret(ctx, tx, s.secrets, ownerKind, ownerUserID, providerSecretName(providerID)); err != nil {
			var notFound data.SecretNotFoundError
			if !errors.As(err, &notFound) {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.availableModelsCache.invalidateProvider(providerID)
	return nil
}

func (s *Service) CreateModel(ctx context.Context, accountID, providerID uuid.UUID, scope string, userID *uuid.UUID, input CreateModelInput) (data.LlmRoute, error) {
	if err := s.requireWriteReady(); err != nil {
		return data.LlmRoute{}, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	provider, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if provider == nil {
		return data.LlmRoute{}, ProviderNotFoundError{ID: providerID}
	}
	if err := ValidateAdvancedJSONForProvider(provider.Provider, input.AdvancedJSON); err != nil {
		return data.LlmRoute{}, err
	}
	existing, err := s.routes.ListByCredential(ctx, accountID, providerID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	model := CanonicalModelIdentifier(provider.Provider, input.Model)
	hasDefault := hasDefaultModel(existing)
	desiredDefault := input.IsDefault || len(existing) == 0
	insertDefault := desiredDefault && len(existing) == 0
	multiplier := derefFloat(input.Multiplier, 1.0)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.LlmRoute{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txRoutes := s.routes.WithTx(tx)
	routeAccountID := accountID
	if scope == "platform" {
		routeAccountID = uuid.Nil
	}
	created, err := txRoutes.Create(ctx, data.CreateLlmRouteParams{
		AccountID:           routeAccountID,
		ProjectID:           uuid.Nil,
		Scope:               scope,
		CredentialID:        providerID,
		Model:               model,
		Priority:            input.Priority,
		IsDefault:           insertDefault,
		ShowInPicker:        input.ShowInPicker,
		Tags:                input.Tags,
		WhenJSON:            input.WhenJSON,
		AdvancedJSON:        input.AdvancedJSON,
		Multiplier:          multiplier,
		CostPer1kInput:      input.CostPer1kInput,
		CostPer1kOutput:     input.CostPer1kOutput,
		CostPer1kCacheWrite: input.CostPer1kCacheWrite,
		CostPer1kCacheRead:  input.CostPer1kCacheRead,
	})
	if err != nil {
		return data.LlmRoute{}, err
	}
	if desiredDefault && len(existing) > 0 {
		if _, err := txRoutes.SetDefaultByCredential(ctx, accountID, providerID, created.ID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	} else if !desiredDefault && !hasDefault {
		if _, err := txRoutes.PromoteHighestPriorityToDefault(ctx, accountID, providerID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return data.LlmRoute{}, err
	}
	stored, err := s.routes.GetByID(ctx, accountID, created.ID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if stored == nil {
		return data.LlmRoute{}, ModelNotFoundError{ID: created.ID}
	}
	s.availableModelsCache.invalidateProvider(providerID)
	return *stored, nil
}

func (s *Service) UpdateModel(ctx context.Context, accountID, providerID, modelID uuid.UUID, scope string, userID *uuid.UUID, input UpdateModelInput) (data.LlmRoute, error) {
	if err := s.requireWriteReady(); err != nil {
		return data.LlmRoute{}, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	provider, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if provider == nil {
		return data.LlmRoute{}, ProviderNotFoundError{ID: providerID}
	}
	current, err := s.routes.GetByID(ctx, accountID, modelID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if current == nil || current.CredentialID != providerID {
		return data.LlmRoute{}, ModelNotFoundError{ID: modelID}
	}

	model := current.Model
	if input.ModelSet {
		model = CanonicalModelIdentifier(provider.Provider, *input.Model)
	}
	priority := current.Priority
	if input.PrioritySet {
		priority = *input.Priority
	}
	isDefault := current.IsDefault
	if input.IsDefaultSet {
		isDefault = *input.IsDefault
	}
	showInPicker := current.ShowInPicker
	if input.ShowInPickerSet {
		showInPicker = *input.ShowInPicker
	}
	tags := current.Tags
	if input.TagsSet {
		tags = input.Tags
	}
	whenJSON := current.WhenJSON
	if input.WhenJSONSet {
		whenJSON = input.WhenJSON
	}
	advancedJSON := current.AdvancedJSON
	if input.AdvancedJSONSet {
		advancedJSON = input.AdvancedJSON
	}
	if err := ValidateAdvancedJSONForProvider(provider.Provider, advancedJSON); err != nil {
		return data.LlmRoute{}, err
	}
	multiplier := current.Multiplier
	if input.MultiplierSet {
		multiplier = derefFloat(input.Multiplier, 1.0)
	}
	costPer1kInput := current.CostPer1kInput
	if input.CostPer1kInputSet {
		costPer1kInput = input.CostPer1kInput
	}
	costPer1kOutput := current.CostPer1kOutput
	if input.CostPer1kOutputSet {
		costPer1kOutput = input.CostPer1kOutput
	}
	costPer1kCacheWrite := current.CostPer1kCacheWrite
	if input.CostPer1kCacheWriteSet {
		costPer1kCacheWrite = input.CostPer1kCacheWrite
	}
	costPer1kCacheRead := current.CostPer1kCacheRead
	if input.CostPer1kCacheReadSet {
		costPer1kCacheRead = input.CostPer1kCacheRead
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.LlmRoute{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txRoutes := s.routes.WithTx(tx)
	if input.IsDefaultSet && *input.IsDefault && !current.IsDefault {
		if _, err := txRoutes.SetDefaultByCredential(ctx, accountID, providerID, modelID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	}
	if _, err := txRoutes.Update(ctx, data.UpdateLlmRouteParams{
		AccountID:           accountID,
		Scope:               scope,
		RouteID:             modelID,
		Model:               model,
		Priority:            priority,
		IsDefault:           isDefault,
		ShowInPicker:        showInPicker,
		Tags:                tags,
		WhenJSON:            whenJSON,
		AdvancedJSON:        advancedJSON,
		Multiplier:          multiplier,
		CostPer1kInput:      costPer1kInput,
		CostPer1kOutput:     costPer1kOutput,
		CostPer1kCacheWrite: costPer1kCacheWrite,
		CostPer1kCacheRead:  costPer1kCacheRead,
	}); err != nil {
		return data.LlmRoute{}, err
	}
	if current.IsDefault && input.IsDefaultSet && !*input.IsDefault {
		if _, err := txRoutes.PromoteHighestPriorityToDefault(ctx, accountID, providerID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return data.LlmRoute{}, err
	}
	stored, err := s.routes.GetByID(ctx, accountID, modelID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if stored == nil {
		return data.LlmRoute{}, ModelNotFoundError{ID: modelID}
	}
	s.availableModelsCache.invalidateProvider(providerID)
	return *stored, nil
}

func (s *Service) DeleteModel(ctx context.Context, accountID, providerID, modelID uuid.UUID, scope string, userID *uuid.UUID) error {
	if err := s.requireWriteReady(); err != nil {
		return err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	provider, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return err
	}
	if provider == nil {
		return ProviderNotFoundError{ID: providerID}
	}
	current, err := s.routes.GetByID(ctx, accountID, modelID, scope)
	if err != nil {
		return err
	}
	if current == nil || current.CredentialID != providerID {
		return ModelNotFoundError{ID: modelID}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txRoutes := s.routes.WithTx(tx)
	if err := txRoutes.DeleteByID(ctx, accountID, modelID, scope); err != nil {
		return err
	}
	if current.IsDefault {
		if _, err := txRoutes.PromoteHighestPriorityToDefault(ctx, accountID, providerID, scope); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.availableModelsCache.invalidateProvider(providerID)
	return nil
}

func (s *Service) ListAvailableModels(ctx context.Context, accountID, providerID uuid.UUID, scope string, userID *uuid.UUID) ([]AvailableModel, error) {
	if err := s.requireWriteReady(); err != nil {
		return nil, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	provider, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, ProviderNotFoundError{ID: providerID}
	}
	if provider.SecretID == nil {
		return nil, ProviderSecretMissingError{ProviderID: providerID}
	}
	apiKey, err := s.secrets.DecryptByID(ctx, *provider.SecretID)
	if err != nil {
		return nil, err
	}
	if apiKey == nil || strings.TrimSpace(*apiKey) == "" {
		return nil, ProviderSecretMissingError{ProviderID: providerID}
	}
	configuredRoutes, err := s.routes.ListByCredential(ctx, accountID, providerID, scope)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{}, len(configuredRoutes))
	for _, route := range configuredRoutes {
		modelID := CanonicalModelIdentifier(provider.Provider, route.Model)
		if modelID == "" {
			continue
		}
		configured[strings.ToLower(modelID)] = struct{}{}
	}
	cacheKey := makeAvailableModelsCacheKey(accountID, providerID, scope, userID)
	models, err := s.availableModelsCache.getOrLoad(ctx, cacheKey, func(ctx context.Context) ([]AvailableModel, error) {
		return listUpstreamModels(ctx, *provider, strings.TrimSpace(*apiKey))
	})
	if err != nil {
		return nil, err
	}
	for idx := range models {
		modelID := CanonicalModelIdentifier(provider.Provider, models[idx].ID)
		models[idx].ID = modelID
		_, models[idx].Configured = configured[strings.ToLower(modelID)]
	}
	return models, nil
}

func (s *Service) ResolveModelTestConfig(ctx context.Context, accountID, providerID, modelID uuid.UUID, scope string, userID *uuid.UUID) (ProviderModelTestConfig, error) {
	if err := s.requireSecretReady(); err != nil {
		return ProviderModelTestConfig{}, err
	}
	ownerKind, ownerUserID := credentialOwner(scope, userID)
	provider, err := s.credentials.GetByID(ctx, ownerKind, ownerUserID, providerID)
	if err != nil {
		return ProviderModelTestConfig{}, err
	}
	if provider == nil {
		return ProviderModelTestConfig{}, ProviderNotFoundError{ID: providerID}
	}
	if provider.SecretID == nil {
		return ProviderModelTestConfig{}, ProviderSecretMissingError{ProviderID: providerID}
	}
	apiKey, err := s.secrets.DecryptByID(ctx, *provider.SecretID)
	if err != nil {
		return ProviderModelTestConfig{}, err
	}
	if apiKey == nil || strings.TrimSpace(*apiKey) == "" {
		return ProviderModelTestConfig{}, ProviderSecretMissingError{ProviderID: providerID}
	}
	model, err := s.routes.GetByID(ctx, accountID, modelID, scope)
	if err != nil {
		return ProviderModelTestConfig{}, err
	}
	if model == nil || model.CredentialID != providerID {
		return ProviderModelTestConfig{}, ModelNotFoundError{ID: modelID}
	}
	return ProviderModelTestConfig{
		Credential: *provider,
		Model:      *model,
		APIKey:     strings.TrimSpace(*apiKey),
	}, nil
}

func (s *Service) requireListReady() error {
	if s.credentials == nil || s.routes == nil {
		return ErrNotConfigured
	}
	return nil
}

func (s *Service) requireWriteReady() error {
	if s.pool == nil || s.credentials == nil || s.routes == nil || s.secrets == nil || s.projects == nil {
		return ErrNotConfigured
	}
	return nil
}

func (s *Service) requireSecretReady() error {
	if s.credentials == nil || s.routes == nil || s.secrets == nil {
		return ErrNotConfigured
	}
	return nil
}

func credentialOwner(scope string, userID *uuid.UUID) (string, *uuid.UUID) {
	if scope == "platform" {
		return "platform", nil
	}
	return "user", userID
}

func upsertProviderSecret(ctx context.Context, tx pgx.Tx, repo *data.SecretsRepository, ownerKind string, ownerUserID *uuid.UUID, name string, plaintext string) (data.Secret, error) {
	if ownerKind == "platform" {
		return repo.WithTx(tx).UpsertPlatform(ctx, name, plaintext)
	}
	return repo.WithTx(tx).Upsert(ctx, *ownerUserID, name, plaintext)
}

func deleteProviderSecret(ctx context.Context, tx pgx.Tx, repo *data.SecretsRepository, ownerKind string, ownerUserID *uuid.UUID, name string) error {
	if ownerKind == "platform" {
		return repo.WithTx(tx).DeletePlatform(ctx, name)
	}
	return repo.WithTx(tx).Delete(ctx, *ownerUserID, name)
}

func providerSecretName(providerID uuid.UUID) string {
	return "llm_cred:" + providerID.String()
}

func computeKeyPrefix(apiKey string) string {
	runes := []rune(strings.TrimSpace(apiKey))
	if len(runes) <= 8 {
		return string(runes)
	}
	return string(runes[:8])
}

func hasDefaultModel(routes []data.LlmRoute) bool {
	for _, route := range routes {
		if route.IsDefault {
			return true
		}
	}
	return false
}

func derefFloat(value *float64, fallback float64) float64 {
	if value == nil || *value <= 0 {
		return fallback
	}
	return *value
}
