//go:build !desktop

package catalogapi

import (
	"context"

	"github.com/google/uuid"
)

func notifyMCPChangedLocal(context.Context, uuid.UUID) {}
