//go:build !desktop

package accountapi

import (
	"context"
	"testing"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChannelInboundBurstRunnerScanEnqueuesSingleRunForDueBatch(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "burst-token", map[string]any{})
	identity := mustLinkDiscordIdentity(t, env, channel.ID, "u-burst", "burst-user")

	thread, err := env.threadRepo.Create(context.Background(), env.accountID, identity.UserID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	msg1, err := env.messageRepo.Create(context.Background(), env.accountID, thread.ID, "user", "first burst input", identity.UserID)
	if err != nil {
		t.Fatalf("create msg1: %v", err)
	}
	msg2, err := env.messageRepo.Create(context.Background(), env.accountID, thread.ID, "user", "second burst input", identity.UserID)
	if err != nil {
		t.Fatalf("create msg2: %v", err)
	}

	dispatchAfter := time.Now().UTC().Add(-1 * time.Second).UnixMilli()
	metadata := applyInboundBurstMetadata(
		inboundLedgerMetadata(map[string]any{
			"source":            "discord",
			"conversation_type": "private",
		}, inboundStatePendingDispatch),
		dispatchAfter,
	)
	if _, err := env.channelLedgerRepo.Record(context.Background(), data.ChannelMessageLedgerRecordInput{
		ChannelID:               channel.ID,
		ChannelType:             channel.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                &thread.ID,
		PlatformConversationID:  "dm-burst",
		PlatformMessageID:       "msg-burst-1",
		SenderChannelIdentityID: &identity.ID,
		MessageID:               &msg1.ID,
		MetadataJSON:            metadata,
	}); err != nil {
		t.Fatalf("record ledger msg1: %v", err)
	}
	if _, err := env.channelLedgerRepo.Record(context.Background(), data.ChannelMessageLedgerRecordInput{
		ChannelID:               channel.ID,
		ChannelType:             channel.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                &thread.ID,
		PlatformConversationID:  "dm-burst",
		PlatformMessageID:       "msg-burst-2",
		SenderChannelIdentityID: &identity.ID,
		MessageID:               &msg2.ID,
		MetadataJSON:            metadata,
	}); err != nil {
		t.Fatalf("record ledger msg2: %v", err)
	}

	runner := channelInboundBurstRunner{
		channelsRepo: env.channelsRepo,
		personasRepo: env.personasRepo,
		ledgerRepo:   env.channelLedgerRepo,
		runEventRepo: env.runEventRepo,
		jobRepo:      env.jobRepo,
		messageRepo:  env.messageRepo,
		pool:         env.pool,
	}
	if err := runner.scan(context.Background()); err != nil {
		t.Fatalf("runner scan: %v", err)
	}

	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM runs`); got != 1 {
		t.Fatalf("runs = %d, want 1", got)
	}
	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = $1`, data.RunExecuteJobType); got != 1 {
		t.Fatalf("run.execute jobs = %d, want 1", got)
	}
	if got := countRows(t, env.pool, `
		SELECT COUNT(*) FROM channel_message_ledger
		 WHERE channel_id = $1
		   AND direction = 'inbound'
		   AND metadata_json->>'ingress_state' = $2`,
		channel.ID,
		inboundStateEnqueuedNewRun,
	); got != 2 {
		t.Fatalf("enqueued inbound ledger rows = %d, want 2", got)
	}
}

func TestChannelInboundBurstRunnerScanDeliversToActiveRun(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "burst-token-active", map[string]any{})
	identity := mustLinkDiscordIdentity(t, env, channel.ID, "u-active", "active-user")

	thread, err := env.threadRepo.Create(context.Background(), env.accountID, identity.UserID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	activeRun, _, err := env.runEventRepo.CreateRunWithStartedEvent(
		context.Background(),
		env.accountID,
		thread.ID,
		identity.UserID,
		"run.started",
		map[string]any{"persona_id": "discord-persona@1"},
	)
	if err != nil {
		t.Fatalf("create active run: %v", err)
	}

	msg, err := env.messageRepo.Create(context.Background(), env.accountID, thread.ID, "user", "append to active run", identity.UserID)
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}
	metadata := applyInboundBurstMetadata(
		inboundLedgerMetadata(map[string]any{"source": "discord"}, inboundStatePendingDispatch),
		time.Now().UTC().Add(-1*time.Second).UnixMilli(),
	)
	if _, err := env.channelLedgerRepo.Record(context.Background(), data.ChannelMessageLedgerRecordInput{
		ChannelID:               channel.ID,
		ChannelType:             channel.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                &thread.ID,
		PlatformConversationID:  "dm-active",
		PlatformMessageID:       "msg-active-1",
		SenderChannelIdentityID: &identity.ID,
		MessageID:               &msg.ID,
		MetadataJSON:            metadata,
	}); err != nil {
		t.Fatalf("record pending ledger: %v", err)
	}

	runner := channelInboundBurstRunner{
		channelsRepo: env.channelsRepo,
		personasRepo: env.personasRepo,
		ledgerRepo:   env.channelLedgerRepo,
		runEventRepo: env.runEventRepo,
		jobRepo:      env.jobRepo,
		messageRepo:  env.messageRepo,
		pool:         env.pool,
	}
	if err := runner.scan(context.Background()); err != nil {
		t.Fatalf("runner scan: %v", err)
	}

	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM runs`); got != 1 {
		t.Fatalf("runs = %d, want 1", got)
	}
	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = $1`, data.RunExecuteJobType); got != 0 {
		t.Fatalf("run.execute jobs = %d, want 0", got)
	}
	if got := countRows(t, env.pool, `
		SELECT COUNT(*) FROM run_events
		 WHERE run_id = $1
		   AND type = 'run.input_provided'`,
		activeRun.ID,
	); got != 1 {
		t.Fatalf("run.input_provided = %d, want 1", got)
	}
	if got := countRows(t, env.pool, `
		SELECT COUNT(*) FROM channel_message_ledger
		 WHERE channel_id = $1
		   AND direction = 'inbound'
		   AND run_id = $2
		   AND metadata_json->>'ingress_state' = $3`,
		channel.ID,
		activeRun.ID,
		inboundStateDeliveredToRun,
	); got != 1 {
		t.Fatalf("delivered inbound ledger rows = %d, want 1", got)
	}
}

func TestChannelInboundBurstRunnerScanSkipsFutureBatch(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "burst-token-future", map[string]any{})
	identity := mustLinkDiscordIdentity(t, env, channel.ID, "u-future", "future-user")

	thread, err := env.threadRepo.Create(context.Background(), env.accountID, identity.UserID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	msg, err := env.messageRepo.Create(context.Background(), env.accountID, thread.ID, "user", "future burst input", identity.UserID)
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}
	metadata := applyInboundBurstMetadata(
		inboundLedgerMetadata(map[string]any{"source": "discord"}, inboundStatePendingDispatch),
		time.Now().UTC().Add(2*time.Minute).UnixMilli(),
	)
	if _, err := env.channelLedgerRepo.Record(context.Background(), data.ChannelMessageLedgerRecordInput{
		ChannelID:               channel.ID,
		ChannelType:             channel.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                &thread.ID,
		PlatformConversationID:  "dm-future",
		PlatformMessageID:       "msg-future-1",
		SenderChannelIdentityID: &identity.ID,
		MessageID:               &msg.ID,
		MetadataJSON:            metadata,
	}); err != nil {
		t.Fatalf("record future pending ledger: %v", err)
	}

	runner := channelInboundBurstRunner{
		channelsRepo: env.channelsRepo,
		personasRepo: env.personasRepo,
		ledgerRepo:   env.channelLedgerRepo,
		runEventRepo: env.runEventRepo,
		jobRepo:      env.jobRepo,
		messageRepo:  env.messageRepo,
		pool:         env.pool,
	}
	if err := runner.scan(context.Background()); err != nil {
		t.Fatalf("runner scan: %v", err)
	}

	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM runs`); got != 0 {
		t.Fatalf("runs = %d, want 0", got)
	}
	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = $1`, data.RunExecuteJobType); got != 0 {
		t.Fatalf("run.execute jobs = %d, want 0", got)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}
