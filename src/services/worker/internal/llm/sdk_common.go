package llm

import (
	"math"
	"net/http"
)

func sdkHTTPClient(t protocolTransport) *http.Client {
	return t.client
}

func sdkBaseURL(t protocolTransport) string {
	return t.cfg.BaseURL
}

func classifyHTTPStatus(status int) string {
	switch status {
	case 400, 408, 425, 429:
		return ErrorClassProviderRetryable
	default:
		if status >= 500 && status <= 599 {
			return ErrorClassProviderRetryable
		}
		return ErrorClassProviderNonRetryable
	}
}

func errorClassFromStatus(status int) string {
	return classifyHTTPStatus(status)
}

func costFromFloat64(value *float64) *Cost {
	if value == nil || *value <= 0 {
		return nil
	}
	return &Cost{
		Currency:     "USD",
		AmountMicros: int(math.Round(*value * 1_000_000)),
	}
}
