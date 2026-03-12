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
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotConfigured = errors.New("llm providers service not configured")

type Provider struct {
	Credential data.LlmCredential
	Models     []data.LlmRoute
}

type AvailableModel struct {
	ID         string
	Name       string
	Configured bool
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
	pool        *pgxpool.Pool
	credentials *data.LlmCredentialsRepository
	routes      *data.LlmRoutesRepository
	secrets     *data.SecretsRepository
}

func NewService(
	pool *pgxpool.Pool,
	credentials *data.LlmCredentialsRepository,
	routes *data.LlmRoutesRepository,
	secrets *data.SecretsRepository,
) *Service {
	return &Service{
		pool:        pool,
		credentials: credentials,
		routes:      routes,
		secrets:     secrets,
	}
}

func (s *Service) ListProviders(ctx context.Context, projectID uuid.UUID, scope string) ([]Provider, error) {
	if err := s.requireListReady(); err != nil {
		return nil, err
	}
	creds, err := s.credentials.ListByScope(ctx, projectID, scope)
	if err != nil {
		return nil, err
	}
	routes, err := s.routes.ListByScope(ctx, projectID, scope)
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

func (s *Service) GetProvider(ctx context.Context, projectID, providerID uuid.UUID, scope string) (Provider, error) {
	if err := s.requireListReady(); err != nil {
		return Provider{}, err
	}
	cred, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
	if err != nil {
		return Provider{}, err
	}
	if cred == nil {
		return Provider{}, ProviderNotFoundError{ID: providerID}
	}
	models, err := s.routes.ListByCredential(ctx, projectID, providerID, scope)
	if err != nil {
		return Provider{}, err
	}
	return Provider{Credential: *cred, Models: models}, nil
}

func (s *Service) CreateProvider(ctx context.Context, projectID uuid.UUID, scope string, input CreateProviderInput) (Provider, error) {
	if err := s.requireWriteReady(); err != nil {
		return Provider{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Provider{}, err
	}
	defer tx.Rollback(ctx)

	providerID := uuid.New()
	secretName := providerSecretName(providerID)
	secret, err := upsertProviderSecret(ctx, tx, s.secrets, projectID, scope, secretName, strings.TrimSpace(input.APIKey))
	if err != nil {
		return Provider{}, err
	}
	keyPrefix := computeKeyPrefix(input.APIKey)
	cred, err := s.credentials.WithTx(tx).Create(
		ctx,
		providerID,
		projectID,
		scope,
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
	return Provider{Credential: cred, Models: []data.LlmRoute{}}, nil
}

func (s *Service) UpdateProvider(ctx context.Context, projectID, providerID uuid.UUID, scope string, input UpdateProviderInput) (Provider, error) {
	if err := s.requireWriteReady(); err != nil {
		return Provider{}, err
	}
	current, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
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
	defer tx.Rollback(ctx)

	if input.APIKey != nil {
		trimmedKey := strings.TrimSpace(*input.APIKey)
		secret, err := upsertProviderSecret(ctx, tx, s.secrets, projectID, scope, providerSecretName(providerID), trimmedKey)
		if err != nil {
			return Provider{}, err
		}
		keyPrefix := computeKeyPrefix(trimmedKey)
		if err := s.credentials.WithTx(tx).UpdateSecret(ctx, projectID, providerID, scope, &secret.ID, &keyPrefix); err != nil {
			return Provider{}, err
		}
	}

	if _, err := s.credentials.WithTx(tx).Update(ctx, projectID, providerID, scope, provider, name, baseURL, openAIAPIMode, advancedJSON); err != nil {
		return Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Provider{}, err
	}
	return s.GetProvider(ctx, projectID, providerID, scope)
}

func (s *Service) DeleteProvider(ctx context.Context, projectID, providerID uuid.UUID, scope string) error {
	if err := s.requireWriteReady(); err != nil {
		return err
	}
	current, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
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
	defer tx.Rollback(ctx)

	if err := s.credentials.WithTx(tx).Delete(ctx, projectID, providerID, scope); err != nil {
		return err
	}
	if current.SecretID != nil {
		if err := deleteProviderSecret(ctx, tx, s.secrets, projectID, scope, providerSecretName(providerID)); err != nil {
			var notFound data.SecretNotFoundError
			if !errors.As(err, &notFound) {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) CreateModel(ctx context.Context, projectID, providerID uuid.UUID, scope string, input CreateModelInput) (data.LlmRoute, error) {
	if err := s.requireWriteReady(); err != nil {
		return data.LlmRoute{}, err
	}
	provider, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if provider == nil {
		return data.LlmRoute{}, ProviderNotFoundError{ID: providerID}
	}
	if err := ValidateAdvancedJSONForProvider(provider.Provider, input.AdvancedJSON); err != nil {
		return data.LlmRoute{}, err
	}
	existing, err := s.routes.ListByCredential(ctx, projectID, providerID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	hasDefault := hasDefaultModel(existing)
	desiredDefault := input.IsDefault || len(existing) == 0
	insertDefault := desiredDefault && len(existing) == 0
	multiplier := derefFloat(input.Multiplier, 1.0)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.LlmRoute{}, err
	}
	defer tx.Rollback(ctx)

	txRoutes := s.routes.WithTx(tx)
	created, err := txRoutes.Create(ctx, data.CreateLlmRouteParams{
		ProjectID:               projectID,
		Scope:               scope,
		CredentialID:        providerID,
		Model:               input.Model,
		Priority:            input.Priority,
		IsDefault:           insertDefault,
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
		if _, err := txRoutes.SetDefaultByCredential(ctx, projectID, providerID, created.ID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	} else if !desiredDefault && !hasDefault {
		if _, err := txRoutes.PromoteHighestPriorityToDefault(ctx, projectID, providerID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return data.LlmRoute{}, err
	}
	stored, err := s.routes.GetByID(ctx, projectID, created.ID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if stored == nil {
		return data.LlmRoute{}, ModelNotFoundError{ID: created.ID}
	}
	return *stored, nil
}

func (s *Service) UpdateModel(ctx context.Context, projectID, providerID, modelID uuid.UUID, scope string, input UpdateModelInput) (data.LlmRoute, error) {
	if err := s.requireWriteReady(); err != nil {
		return data.LlmRoute{}, err
	}
	provider, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if provider == nil {
		return data.LlmRoute{}, ProviderNotFoundError{ID: providerID}
	}
	current, err := s.routes.GetByID(ctx, projectID, modelID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if current == nil || current.CredentialID != providerID {
		return data.LlmRoute{}, ModelNotFoundError{ID: modelID}
	}

	model := current.Model
	if input.ModelSet {
		model = strings.TrimSpace(*input.Model)
	}
	priority := current.Priority
	if input.PrioritySet {
		priority = *input.Priority
	}
	isDefault := current.IsDefault
	if input.IsDefaultSet {
		isDefault = *input.IsDefault
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
	defer tx.Rollback(ctx)

	txRoutes := s.routes.WithTx(tx)
	if input.IsDefaultSet && *input.IsDefault && !current.IsDefault {
		if _, err := txRoutes.SetDefaultByCredential(ctx, projectID, providerID, modelID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	}
	if _, err := txRoutes.Update(ctx, data.UpdateLlmRouteParams{
		ProjectID:               projectID,
		Scope:               scope,
		RouteID:             modelID,
		Model:               model,
		Priority:            priority,
		IsDefault:           isDefault,
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
		if _, err := txRoutes.PromoteHighestPriorityToDefault(ctx, projectID, providerID, scope); err != nil {
			return data.LlmRoute{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return data.LlmRoute{}, err
	}
	stored, err := s.routes.GetByID(ctx, projectID, modelID, scope)
	if err != nil {
		return data.LlmRoute{}, err
	}
	if stored == nil {
		return data.LlmRoute{}, ModelNotFoundError{ID: modelID}
	}
	return *stored, nil
}

func (s *Service) DeleteModel(ctx context.Context, projectID, providerID, modelID uuid.UUID, scope string) error {
	if err := s.requireWriteReady(); err != nil {
		return err
	}
	provider, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
	if err != nil {
		return err
	}
	if provider == nil {
		return ProviderNotFoundError{ID: providerID}
	}
	current, err := s.routes.GetByID(ctx, projectID, modelID, scope)
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
	defer tx.Rollback(ctx)

	txRoutes := s.routes.WithTx(tx)
	if err := txRoutes.DeleteByID(ctx, projectID, modelID, scope); err != nil {
		return err
	}
	if current.IsDefault {
		if _, err := txRoutes.PromoteHighestPriorityToDefault(ctx, projectID, providerID, scope); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) ListAvailableModels(ctx context.Context, projectID, providerID uuid.UUID, scope string) ([]AvailableModel, error) {
	if err := s.requireWriteReady(); err != nil {
		return nil, err
	}
	provider, err := s.credentials.GetByID(ctx, projectID, providerID, scope)
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
	configuredRoutes, err := s.routes.ListByCredential(ctx, projectID, providerID, scope)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{}, len(configuredRoutes))
	for _, route := range configuredRoutes {
		configured[strings.ToLower(route.Model)] = struct{}{}
	}
	models, err := listUpstreamModels(ctx, *provider, strings.TrimSpace(*apiKey))
	if err != nil {
		return nil, err
	}
	for idx := range models {
		_, models[idx].Configured = configured[strings.ToLower(models[idx].ID)]
	}
	return models, nil
}

func (s *Service) requireListReady() error {
	if s.credentials == nil || s.routes == nil {
		return ErrNotConfigured
	}
	return nil
}

func (s *Service) requireWriteReady() error {
	if s.pool == nil || s.credentials == nil || s.routes == nil || s.secrets == nil {
		return ErrNotConfigured
	}
	return nil
}

func upsertProviderSecret(ctx context.Context, tx pgx.Tx, repo *data.SecretsRepository, accountID uuid.UUID, scope string, name string, plaintext string) (data.Secret, error) {
	if scope == data.LlmCredentialScopePlatform {
		return repo.WithTx(tx).UpsertPlatform(ctx, name, plaintext)
	}
	return repo.WithTx(tx).Upsert(ctx, accountID, name, plaintext)
}

func deleteProviderSecret(ctx context.Context, tx pgx.Tx, repo *data.SecretsRepository, accountID uuid.UUID, scope string, name string) error {
	if scope == data.LlmCredentialScopePlatform {
		return repo.WithTx(tx).DeletePlatform(ctx, name)
	}
	return repo.WithTx(tx).Delete(ctx, accountID, name)
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
