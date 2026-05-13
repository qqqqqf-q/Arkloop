//go:build desktop

package catalogapi

import (
	"context"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"

	"github.com/google/uuid"
)

func notifyMCPChangedLocal(ctx context.Context, accountID uuid.UUID) {
	bus, ok := desktop.GetEventBus().(eventbus.EventBus)
	if !ok || bus == nil || accountID == uuid.Nil {
		return
	}
	_ = bus.Publish(ctx, "mcp_config_changed", accountID.String())
}
