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

	"github.com/google/uuid"
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
	membershipRepo, err := data.NewAccountMembershipRepository(env.pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	if _, err := membershipRepo.Create(context.Background(), env.accountID, secondUser.ID, auth.RoleAccountAdmin); err != nil {
		t.Fatalf("create second membership: %v", err)
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

	deleteResp := doJSONAccount(
		env.handler,
		nethttp.MethodDelete,
		"/v1/channels/"+channel.ID.String()+"/bindings/"+ownerBinding.BindingID,
		nil,
		authHeader(env.accessToken),
	)
	if deleteResp.Code != nethttp.StatusOK {
		t.Fatalf("delete non-owner binding: %d %s", deleteResp.Code, deleteResp.Body.String())
	}
	updatedChannel, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel after non-owner delete: %v", err)
	}
	if updatedChannel == nil || updatedChannel.OwnerUserID == nil || *updatedChannel.OwnerUserID != secondUser.ID {
		t.Fatalf("non-owner delete changed owner: %#v", updatedChannel)
	}
}

func TestChannelBindingsOwnerDeleteAllowed(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, discordbot.NewClient("", nil))
	channel := createActiveDiscordChannelWithConfig(t, env, "discord-owner-delete-token", map[string]any{})

	code, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-owner-delete",
		channel.ID,
		"discord-owner-delete-token",
		newDiscordInteractionCommand("bind", "", "dm-owner-delete", "u-owner-delete", "owner-delete", code.Token),
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
	if deleteResp.Code != nethttp.StatusOK {
		t.Fatalf("delete owner binding: %d %s", deleteResp.Code, deleteResp.Body.String())
	}

	listResp = doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings after delete: %d %s", listResp.Code, listResp.Body.String())
	}
	listBody = nil
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response after delete: %v", err)
	}
	if len(listBody) != 0 {
		t.Fatalf("expected 0 bindings after delete, got %d", len(listBody))
	}

	updatedChannel, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel after owner delete: %v", err)
	}
	if updatedChannel == nil || updatedChannel.OwnerUserID != nil {
		t.Fatalf("expected channel owner to be cleared, got %#v", updatedChannel)
	}

	nextCode, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create rebind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-owner-rebind",
		channel.ID,
		"discord-owner-delete-token",
		newDiscordInteractionCommand("bind", "", "dm-owner-delete", "u-owner-delete", "owner-delete", nextCode.Token),
	); err != nil {
		t.Fatalf("rebind interaction: %v", err)
	}
	updatedChannel, err = env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel after rebind: %v", err)
	}
	if updatedChannel == nil || updatedChannel.OwnerUserID == nil || *updatedChannel.OwnerUserID != env.userID {
		t.Fatalf("expected rebind to restore owner, got %#v", updatedChannel)
	}
	listResp = doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings after rebind: %d %s", listResp.Code, listResp.Body.String())
	}
	listBody = nil
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response after rebind: %v", err)
	}
	if len(listBody) != 1 || !listBody[0].IsOwner {
		t.Fatalf("expected owner binding after rebind, got %#v", listBody)
	}
}

func TestChannelIdentityUnbindClearsOwnedChannels(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, discordbot.NewClient("", nil))
	channel := createActiveDiscordChannelWithConfig(t, env, "discord-owner-unbind-token", map[string]any{})

	code, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, stringPtr("discord"), time.Hour)
	if err != nil {
		t.Fatalf("create bind code: %v", err)
	}
	if _, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-owner-unbind",
		channel.ID,
		"discord-owner-unbind-token",
		newDiscordInteractionCommand("bind", "", "dm-owner-unbind", "u-owner-unbind", "owner-unbind", code.Token),
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
	if len(listBody) != 1 || !listBody[0].IsOwner {
		t.Fatalf("expected owner binding, got %#v", listBody)
	}

	unbindResp := doJSONAccount(
		env.handler,
		nethttp.MethodDelete,
		"/v1/me/channel-identities/"+listBody[0].ChannelIdentityID,
		nil,
		authHeader(env.accessToken),
	)
	if unbindResp.Code != nethttp.StatusOK {
		t.Fatalf("unbind identity: %d %s", unbindResp.Code, unbindResp.Body.String())
	}

	updatedChannel, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel after identity unbind: %v", err)
	}
	if updatedChannel == nil || updatedChannel.OwnerUserID != nil {
		t.Fatalf("expected channel owner to be cleared, got %#v", updatedChannel)
	}
	identityID := listBody[0].ChannelIdentityID
	parsedIdentityID, err := uuid.Parse(identityID)
	if err != nil {
		t.Fatalf("parse identity id: %v", err)
	}
	identity, err := env.channelIdentitiesRepo.GetByID(context.Background(), parsedIdentityID)
	if err != nil {
		t.Fatalf("get identity after unbind: %v", err)
	}
	if identity == nil || identity.UserID != nil {
		t.Fatalf("expected identity user to be cleared, got %#v", identity)
	}
	listResp = doJSONAccount(env.handler, nethttp.MethodGet, "/v1/channels/"+channel.ID.String()+"/bindings", nil, authHeader(env.accessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings after identity unbind: %d %s", listResp.Code, listResp.Body.String())
	}
	listBody = nil
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode bindings response after identity unbind: %v", err)
	}
	if len(listBody) != 0 {
		t.Fatalf("expected no bindings after identity unbind, got %#v", listBody)
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
