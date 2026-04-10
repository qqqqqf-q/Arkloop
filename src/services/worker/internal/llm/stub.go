package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

const (
	stubEnabledEnv           = "ARKLOOP_STUB_AGENT_ENABLED"
	stubDeltaCountEnv        = "ARKLOOP_STUB_AGENT_DELTA_COUNT"
	stubDeltaIntervalSeconds = "ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS"
	llmDebugEventsEnv        = "ARKLOOP_LLM_DEBUG_EVENTS"
	defaultStubEnabled       = true
	defaultStubDeltaCount    = 3
	defaultStubDeltaInterval = 0.02
	defaultStubProviderKind  = "stub"
	defaultStubAPIMode       = "stub"
	truthyValues             = "1,true,yes,y,on"
	falsyValues              = "0,false,no,n,off"
)

type AuxGatewayConfig struct {
	Enabled         bool
	DeltaCount      int
	DeltaInterval   time.Duration
	EmitDebugEvents bool
}

func AuxGatewayConfigFromEnv() (AuxGatewayConfig, error) {
	enabled := defaultStubEnabled
	if raw := strings.TrimSpace(os.Getenv(stubEnabledEnv)); raw != "" {
		value, err := parseBool(raw)
		if err != nil {
			return AuxGatewayConfig{}, fmt.Errorf("%s: %w", stubEnabledEnv, err)
		}
		enabled = value
	}

	deltaCount := defaultStubDeltaCount
	if raw := strings.TrimSpace(os.Getenv(stubDeltaCountEnv)); raw != "" {
		value, err := parsePositiveInt(raw)
		if err != nil {
			return AuxGatewayConfig{}, fmt.Errorf("%s: %w", stubDeltaCountEnv, err)
		}
		deltaCount = value
	}

	intervalSeconds := defaultStubDeltaInterval
	if raw := strings.TrimSpace(os.Getenv(stubDeltaIntervalSeconds)); raw != "" {
		value, err := parseNonNegativeFloat(raw)
		if err != nil {
			return AuxGatewayConfig{}, fmt.Errorf("%s: %w", stubDeltaIntervalSeconds, err)
		}
		intervalSeconds = value
	}

	emitDebugEvents := false
	if raw := strings.TrimSpace(os.Getenv(llmDebugEventsEnv)); raw != "" {
		value, err := parseBool(raw)
		if err != nil {
			return AuxGatewayConfig{}, fmt.Errorf("%s: %w", llmDebugEventsEnv, err)
		}
		emitDebugEvents = value
	}

	return AuxGatewayConfig{
		Enabled:         enabled,
		DeltaCount:      deltaCount,
		DeltaInterval:   time.Duration(intervalSeconds * float64(time.Second)),
		EmitDebugEvents: emitDebugEvents,
	}, nil
}

type AuxGateway struct {
	cfg AuxGatewayConfig
}

func NewAuxGateway(cfg AuxGatewayConfig) *AuxGateway {
	return &AuxGateway{cfg: cfg}
}

func (g *AuxGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if !g.cfg.Enabled {
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderNonRetryable,
				Message:    "aux gateway is disabled",
			},
		}
		return yield(failed)
	}

	llmCallID := uuid.NewString()
	stats := ComputeRequestStats(request)
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(request.ToJSON())
	if err := yield(StreamLlmRequest{
		LlmCallID:            llmCallID,
		ProviderKind:         defaultStubProviderKind,
		APIMode:              defaultStubAPIMode,
		PayloadJSON:          debugPayload,
		RedactedHints:        redactedHints,
		SystemBytes:          stats.SystemBytes,
		ToolsBytes:           stats.ToolsBytes,
		MessagesBytes:        stats.MessagesBytes,
		AbstractRequestBytes: stats.AbstractRequestBytes,
		ProviderPayloadBytes: stats.AbstractRequestBytes,
		ImagePartCount:       stats.ImagePartCount,
		Base64ImageBytes:     stats.Base64ImageBytes,
		RoleBytes:            stats.RoleBytes,
		ToolSchemaBytesMap:   stats.ToolSchemaBytesMap,
		StablePrefixHash:     stats.StablePrefixHash,
	}); err != nil {
		return err
	}

	for idx := 1; idx <= g.cfg.DeltaCount; idx++ {
		if err := sleepWithContext(ctx, g.cfg.DeltaInterval); err != nil {
			return err
		}
		delta := fmt.Sprintf("stub delta %d", idx)
		if g.cfg.EmitDebugEvents {
			chunkJSON := map[string]any{"content_delta": delta, "role": "assistant"}
			raw, _ := stablejson.Encode(chunkJSON)
			if raw == "" {
				raw = string(mustJSONMarshal(chunkJSON))
			}
			if err := yield(StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: defaultStubProviderKind,
				APIMode:      defaultStubAPIMode,
				Raw:          raw,
				ChunkJSON:    chunkJSON,
			}); err != nil {
				return err
			}
		}
		if err := yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant"}); err != nil {
			return err
		}
	}
	return yield(StreamRunCompleted{LlmCallID: llmCallID})
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseBool(raw string) (bool, error) {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	for _, item := range strings.Split(truthyValues, ",") {
		if cleaned == item {
			return true, nil
		}
	}
	for _, item := range strings.Split(falsyValues, ",") {
		if cleaned == item {
			return false, nil
		}
	}
	return false, fmt.Errorf("must be a boolean (0/1, true/false)")
}

func parsePositiveInt(raw string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be a positive integer")
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return parsed, nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("must be non-negative")
	}
	if parsed < 0 {
		return 0, fmt.Errorf("must be non-negative")
	}
	return parsed, nil
}

func mustJSONMarshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}
