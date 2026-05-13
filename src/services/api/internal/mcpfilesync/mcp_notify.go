//go:build !desktop

package mcpfilesync

import (
	"context"

	"github.com/google/uuid"
)

func notifyMCPChangedLocal(context.Context, uuid.UUID) {}
