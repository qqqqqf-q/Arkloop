package accountapi

import (
	"context"
	"sort"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

func listPendingInboundBatchTx(
	ctx context.Context,
	repo *data.ChannelMessageLedgerRepository,
	channelID uuid.UUID,
	threadID uuid.UUID,
) ([]data.ChannelInboundLedgerEntry, error) {
	entries, err := repo.ListInboundEntriesByThreadState(ctx, channelID, threadID, inboundStatePendingDispatch, true)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	return entries, nil
}

func pendingBatchReady(entries []data.ChannelInboundLedgerEntry, now time.Time) bool {
	if len(entries) == 0 {
		return false
	}
	dispatchAfterUnixMs := int64(0)
	for _, entry := range entries {
		value, ok := inboundLedgerDispatchAfterUnixMs(entry.MetadataJSON)
		if !ok {
			continue
		}
		if value > dispatchAfterUnixMs {
			dispatchAfterUnixMs = value
		}
	}
	if dispatchAfterUnixMs == 0 {
		return true
	}
	return dispatchAfterUnixMs <= now.UnixMilli()
}

func markPendingBatchEnqueuedTx(
	ctx context.Context,
	repo *data.ChannelMessageLedgerRepository,
	channelID uuid.UUID,
	entries []data.ChannelInboundLedgerEntry,
	runID uuid.UUID,
) error {
	return markPendingBatchStateTx(ctx, repo, channelID, entries, &runID, inboundStateEnqueuedNewRun)
}
