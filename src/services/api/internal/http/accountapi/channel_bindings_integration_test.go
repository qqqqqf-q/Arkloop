//go:build !desktop

package accountapi

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/discordbot"
)

func TestChannelBindingsEndpointsSupportOwnerTransferAndHeartbeat(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, discordbot.NewClient("", nil))
	channel := createActiveDiscordChannelWithConfig(t, env, "discord-bindings-token", map[string]any{})

	ownerCode, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create owner bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-owner-bind",
		channel.ID,
		"discord-bindings-token",
		newDiscordInteractionCommand("bind", "", "dm-owner", "u-owner", "owner-user", ownerCode.Token),
	); err != nil {
		t.Fatalf("owner bind interaction: %v", err)
	}

	userRepo, err := data.NewUserRepository(env.pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	secondUser, err := userRepo.Create(context.Background(), "discord-admin", "discord-admin@test.com", "zh")
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}
	adminCode, err := env.channelBindCodesRepo.Create(context.Background(), secondUser.ID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create admin bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-admin-bind",
		channel.ID,
		"discord-bindings-token",
		newDiscordInteractionCommand("bind", "", "dm-admin", "u-admin", "admin-user", adminCode.Token),
	); err != nil {
		t.Fatalf("admin bind interaction: %v", err)
	}

	listResp := doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings: %d %s", listResp.Code, listResp.Body.String())
	}
	var listBody []channelBindingResponse
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response: %v", err)
	}
	if len(listBody) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(listBody))
	}

	var ownerBinding channelBindingResponse
	var adminBinding channelBindingResponse
	for _, item := range listBody {
		switch item.PlatformSubjectID {
		case "u-owner":
			ownerBinding = item
		case "u-admin":
			adminBinding = item
		}
	}
	if ownerBinding.BindingID == "" || !ownerBinding.IsOwner {
		t.Fatalf("owner binding not marked as owner: %#v", ownerBinding)
	}
	if adminBinding.BindingID == "" || adminBinding.IsOwner {
		t.Fatalf("admin binding unexpected: %#v", adminBinding)
	}

	heartbeatReq := map[string]any{
		"heartbeat_enabled":          true,
		"heartbeat_interval_minutes": 12,
		"heartbeat_model":            "gpt-5.4",
	}
	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String()+"/bindings/"+adminBinding.BindingID,
		heartbeatReq,
		authHeader(env.accessToken),
	)
	if updateResp.Code != nethttp.StatusUnprocessableEntity {
		t.Fatalf("heartbeat binding update should be rejected: %d %s", updateResp.Code, updateResp.Body.String())
	}
	if got := countRows(t, env.pool, `SELECT COUNT(*) FROM scheduled_triggers`); got != 0 {
		t.Fatalf("scheduled triggers = %d, want 0", got)
	}

	makeOwnerResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String()+"/bindings/"+adminBinding.BindingID,
		map[string]any{"make_owner": true},
		authHeader(env.accessToken),
	)
	if makeOwnerResp.Code != nethttp.StatusOK {
		t.Fatalf("make owner: %d %s", makeOwnerResp.Code, makeOwnerResp.Body.String())
	}

	listResp = doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings after owner transfer: %d %s", listResp.Code, listResp.Body.String())
	}
	listBody = nil
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response after owner transfer: %v", err)
	}
	for _, item := range listBody {
		switch item.PlatformSubjectID {
		case "u-owner":
			ownerBinding = item
		case "u-admin":
			adminBinding = item
		}
	}
	if ownerBinding.IsOwner {
		t.Fatalf("expected former owner to be admin: %#v", ownerBinding)
	}
	if !adminBinding.IsOwner {
		t.Fatalf("expected admin to become owner: %#v", adminBinding)
	}
}

func TestChannelBindingsOwnerDeleteBlocked(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, discordbot.NewClient("", nil))
	channel := createActiveDiscordChannelWithConfig(t, env, "discord-owner-block-token", map[string]any{})

	code, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-owner-block",
		channel.ID,
		"discord-owner-block-token",
		newDiscordInteractionCommand("bind", "", "dm-owner-block", "u-owner-block", "owner-block", code.Token),
	); err != nil {
		t.Fatalf("bind interaction: %v", err)
	}

	listResp := doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings: %d %s", listResp.Code, listResp.Body.String())
	}
	var listBody []channelBindingResponse
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response: %v", err)
	}
	if len(listBody) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(listBody))
	}

	deleteResp := doJSONAccount(
		env.handler,
		nethttp.MethodDelete,
		"/v1/channels/"+channel.ID.String()+"/bindings/"+listBody[0].BindingID,
		nil,
		authHeader(env.accessToken),
	)
	if deleteResp.Code != nethttp.StatusConflict {
		t.Fatalf("delete owner binding: %d %s", deleteResp.Code, deleteResp.Body.String())
	}
}

func TestChannelDeleteStillWorksAfterOwnerTransferAndAdminTokenRotation(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, discordbot.NewClient("", nil))
	channel := createActiveDiscordChannelWithConfig(t, env, "discord-secret-owner-token", map[string]any{})

	ownerCode, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create owner bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-secret-owner",
		channel.ID,
		"discord-secret-owner-token",
		newDiscordInteractionCommand("bind", "", "dm-owner-secret", "u-owner-secret", "owner-secret", ownerCode.Token),
	); err != nil {
		t.Fatalf("owner bind interaction: %v", err)
	}

	userRepo, err := data.NewUserRepository(env.pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(env.pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	secondUser, err := userRepo.Create(context.Background(), "discord-secret-admin", "discord-secret-admin@test.com", "zh")
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, err := membershipRepo.Create(context.Background(), env.accountID, secondUser.ID, auth.RoleAccountAdmin); err != nil {
		t.Fatalf("create second membership: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("token service: %v", err)
	}
	secondAccessToken, err := tokenService.Issue(secondUser.ID, env.accountID, auth.RoleAccountAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue second token: %v", err)
	}

	adminCode, err := env.channelBindCodesRepo.Create(context.Background(), secondUser.ID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create admin bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-secret-admin",
		channel.ID,
		"discord-secret-owner-token",
		newDiscordInteractionCommand("bind", "", "dm-admin-secret", "u-admin-secret", "admin-secret", adminCode.Token),
	); err != nil {
		t.Fatalf("admin bind interaction: %v", err)
	}

	listResp := doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings: %d %s", listResp.Code, listResp.Body.String())
	}
	var listBody []channelBindingResponse
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response: %v", err)
	}
	var adminBinding channelBindingResponse
	for _, item := range listBody {
		if item.PlatformSubjectID == "u-admin-secret" {
			adminBinding = item
			break
		}
	}
	if adminBinding.BindingID == "" {
		t.Fatalf("admin binding missing: %#v", listBody)
	}

	makeOwnerResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String()+"/bindings/"+adminBinding.BindingID,
		map[string]any{"make_owner": true},
		authHeader(env.accessToken),
	)
	if makeOwnerResp.Code != nethttp.StatusOK {
		t.Fatalf("make owner: %d %s", makeOwnerResp.Code, makeOwnerResp.Body.String())
	}

	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String(),
		map[string]any{"bot_token": "discord-secret-rotated-token"},
		authHeader(secondAccessToken),
	)
	if updateResp.Code != nethttp.StatusOK {
		t.Fatalf("rotate token as second admin: %d %s", updateResp.Code, updateResp.Body.String())
	}

	deleteResp := doJSONAccount(
		env.handler,
		nethttp.MethodDelete,
		"/v1/channels/"+channel.ID.String(),
		nil,
		authHeader(env.accessToken),
	)
	if deleteResp.Code != nethttp.StatusOK {
		t.Fatalf("delete channel after owner transfer: %d %s", deleteResp.Code, deleteResp.Body.String())
	}
}

func stringPtr(value string) *string {
	return &value
}
