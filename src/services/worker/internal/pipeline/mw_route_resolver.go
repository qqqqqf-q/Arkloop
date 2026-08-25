package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
)

type entitlementRouteResolution struct {
	Selected *routing.SelectedProviderRoute
	Gateway  llm.Gateway
}


// resolveVisionRoute 解析图像理解模型路由:仅取 persona.ImageModel
// (provider^model selector)。失败时不兜底,返回 false。
func resolveVisionRoute(
	ctx context.Context,
	pool CompactPersistDB,
	accountID uuid.UUID,
	personaImageModel *string,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
	byokEnabled bool,
) (*entitlementRouteResolution, bool) {
	// persona.ImageModel 优先
	if personaImageModel != nil {
		selector := strings.TrimSpace(*personaImageModel)
		if selector != "" && configLoader != nil {
			aid := accountID
			routingCfg, err := configLoader.Load(ctx, &aid)
			if err != nil {
				slog.Warn("vision_route: load routing config for persona.image_model failed", "err", err.Error())
			} else {
				selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, byokEnabled)
				if err != nil {
					slog.Warn("vision_route: persona.image_model resolve failed", "selector", selector, "err", err.Error())
				} else if selected != nil {
					gw, err := gatewayFromSelectedRoute(*selected, auxGateway, emitDebugEvents, llmMaxResponseBytes)
					if err != nil {
						slog.Warn("vision_route: persona.image_model build gateway failed", "err", err.Error())
					} else {
						return &entitlementRouteResolution{
							Selected: selected,
							Gateway:  gw,
						}, true
					}
				}
			}
		}
	}

	return nil, false
}

// messageContainsImage 检测 messages 中是否包含 image part。
func messageContainsImage(messages []llm.Message) bool {
	for _, msg := range messages {
		for _, part := range msg.Content {
			if part.Kind() == "image" {
				return true
			}
		}
	}
	return false
}

// routeSupportsVision 检测 selected route 是否支持 image input。
// 仅依赖 available_catalog.input_modalities，不做模型名硬编码猜测。
func routeSupportsVision(selected *routing.SelectedProviderRoute) bool {
	if selected == nil {
		return false
	}
	caps, ok := routing.SelectedRouteCatalogModelCapabilities(selected)
	return ok && caps.SupportsInputModality("image")
}

// swapRunContextRoute 将 RunContext 的 gateway/selectedRoute/contextWindow 切换到新 route。
// 如果 estimateFn 非 nil，同步更新 EstimateProviderRequestBytes。
func swapRunContextRoute(rc *RunContext, resolution *entitlementRouteResolution, estimateFn func(llm.Request) (int, error)) {
	rc.Gateway = resolution.Gateway
	rc.SelectedRoute = resolution.Selected
	rc.ContextWindowTokens = routing.RouteContextWindowTokens(resolution.Selected.Route)
	if rc.Temperature == nil {
		rc.Temperature = routing.RouteDefaultTemperature(resolution.Selected.Route)
	}
	if estimateFn != nil {
		rc.EstimateProviderRequestBytes = estimateFn
	}
}



// publishRunEventFromRC 通知 run event channel。
func publishRunEventFromRC(ctx context.Context, rc *RunContext) {
	if rc == nil {
		return
	}
	channel := fmt.Sprintf("run_events:%s", rc.Run.ID.String())
	if rc.EventBus != nil {
		_ = rc.EventBus.Publish(ctx, channel, "")
	}
	if rc.BroadcastRDB != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", rc.Run.ID.String())
		_, _ = rc.BroadcastRDB.Publish(ctx, redisChannel, "").Result()
	}
}
