package catalogapi

import (
	"strings"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

func normalizeOptionalBaseURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	normalized, err := sharedoutbound.DefaultPolicy().NormalizeBaseURL(trimmed)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeOptionalInternalBaseURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	normalized, err := sharedoutbound.DefaultPolicy().NormalizeInternalBaseURL(trimmed)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}
