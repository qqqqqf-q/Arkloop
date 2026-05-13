package mcpoauth

import "time"

const (
	LatestProtocolVersion = "2025-11-25"

	ClientAuthNone  = "none"
	ClientAuthBasic = "client_secret_basic"
	ClientAuthPost  = "client_secret_post"
)

type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ResourceSigningAlg     string   `json:"resource_signing_alg,omitempty"`
}

type AuthorizationServerMetadata struct {
	Issuer                                             string   `json:"issuer"`
	AuthorizationEndpoint                              string   `json:"authorization_endpoint"`
	TokenEndpoint                                      string   `json:"token_endpoint"`
	RegistrationEndpoint                               string   `json:"registration_endpoint,omitempty"`
	ResponseTypesSupported                             []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported                                []string `json:"grant_types_supported,omitempty"`
	ScopesSupported                                    []string `json:"scopes_supported,omitempty"`
	CodeChallengeMethodsSupported                      []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported                  []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	ClientIDMetadataDocumentSupported                  bool     `json:"client_id_metadata_document_supported,omitempty"`
	RevocationEndpoint                                 string   `json:"revocation_endpoint,omitempty"`
	RevocationEndpointAuthMethodsSupported             []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	IntrospectionEndpoint                              string   `json:"introspection_endpoint,omitempty"`
	IntrospectionEndpointAuthMethodsSupported          []string `json:"introspection_endpoint_auth_methods_supported,omitempty"`
	TokenEndpointAuthSigningAlgValuesSupported         []string `json:"token_endpoint_auth_signing_alg_values_supported,omitempty"`
	RevocationEndpointAuthSigningAlgValuesSupported    []string `json:"revocation_endpoint_auth_signing_alg_values_supported,omitempty"`
	IntrospectionEndpointAuthSigningAlgValuesSupported []string `json:"introspection_endpoint_auth_signing_alg_values_supported,omitempty"`
}

type ClientMetadata struct {
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
}

type ClientInformation struct {
	ClientMetadata
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64  `json:"client_secret_expires_at,omitempty"`
	RegistrationAccessToken string `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string `json:"registration_client_uri,omitempty"`
}

type Tokens struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	ObtainedAt   time.Time `json:"obtained_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

type DiscoveryState struct {
	ServerURL                   string                       `json:"server_url,omitempty"`
	ResourceMetadataURL         string                       `json:"resource_metadata_url,omitempty"`
	AuthorizationServerURL      string                       `json:"authorization_server_url,omitempty"`
	ResourceMetadata            *ProtectedResourceMetadata   `json:"resource_metadata,omitempty"`
	AuthorizationServerMetadata *AuthorizationServerMetadata `json:"authorization_server_metadata,omitempty"`
}

type AuthState struct {
	Discovery        DiscoveryState    `json:"discovery"`
	Client           ClientInformation `json:"client,omitempty"`
	Tokens           Tokens            `json:"tokens,omitempty"`
	CodeVerifier     string            `json:"code_verifier,omitempty"`
	State            string            `json:"state,omitempty"`
	RedirectURI      string            `json:"redirect_uri,omitempty"`
	Scope            string            `json:"scope,omitempty"`
	AuthorizationURL string            `json:"authorization_url,omitempty"`
}
