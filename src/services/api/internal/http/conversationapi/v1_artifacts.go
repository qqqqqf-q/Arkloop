package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

func artifactsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	runRepo *data.RunEventRepository,
	shellSessionRepo *data.ShellSessionRepository,
	threadShareRepo *data.ThreadShareRepository,
	auditWriter *audit.Writer,
	store artifactStore,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if store == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "artifacts.not_configured", "artifact storage not configured", traceID, nil)
			return
		}
		if runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		key := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
		if key == "" || strings.Contains(key, "..") {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "artifacts.invalid_key", "invalid artifact key", traceID, nil)
			return
		}

		info, err := store.Head(r.Context(), key)
		if err != nil {
			if objectstore.IsNotFound(err) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "artifacts.not_found", "artifact not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		run, ok := resolveArtifactRun(r.Context(), runRepo, shellSessionRepo, key, info)
		if !ok || run == nil {
			httpkit.WriteError(w, nethttp.StatusForbidden, "artifacts.forbidden", "access denied", traceID, nil)
			return
		}

		shareToken := strings.TrimSpace(r.URL.Query().Get("share_token"))
		hasAuthorization := strings.TrimSpace(r.Header.Get("Authorization")) != ""
		if !hasAuthorization && shareToken != "" {
			if !authorizeArtifactShare(w, r, traceID, threadShareRepo, shareToken, run) {
				return
			}
		} else {
			actor, authenticated := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
			if !authenticated {
				return
			}
			if !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
				return
			}
			if !authorizeRunOrAudit(w, r, traceID, actor, "artifacts.get", run, auditWriter) {
				return
			}
		}

		blobData, contentType, err := store.GetWithContentType(r.Context(), key)
		if err != nil {
			if objectstore.IsNotFound(err) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "artifacts.not_found", "artifact not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(blobData)
	}
}

func resolveArtifactRun(
	ctx context.Context,
	runRepo *data.RunEventRepository,
	shellSessionRepo *data.ShellSessionRepository,
	key string,
	info objectstore.ObjectInfo,
) (*data.Run, bool) {
	runID, ok := resolveArtifactRunID(ctx, shellSessionRepo, key, info.Metadata)
	if !ok {
		return nil, false
	}
	run, err := runRepo.GetRun(ctx, runID)
	if err != nil || run == nil {
		return nil, false
	}
	if metadataAccountID := strings.TrimSpace(info.Metadata[objectstore.ArtifactMetaAccountID]); metadataAccountID != "" && metadataAccountID != run.AccountID.String() {
		return nil, false
	}
	return run, true
}

func authorizeArtifactShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	threadShareRepo *data.ThreadShareRepository,
	shareToken string,
	run *data.Run,
) bool {
	if threadShareRepo == nil || run == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return false
	}

	share, err := threadShareRepo.GetByToken(r.Context(), shareToken)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if share == nil || share.ThreadID != run.ThreadID {
		httpkit.WriteError(w, nethttp.StatusForbidden, "artifacts.forbidden", "access denied", traceID, nil)
		return false
	}
	if share.AccessType == "password" {
		sessionToken := strings.TrimSpace(r.URL.Query().Get("session_token"))
		if sessionToken == "" || !validateShareSession(sessionToken, share) {
			httpkit.WriteError(w, nethttp.StatusForbidden, "artifacts.forbidden", "access denied", traceID, nil)
			return false
		}
	}
	return true
}

func resolveArtifactRunID(
	ctx context.Context,
	shellSessionRepo *data.ShellSessionRepository,
	key string,
	metadata map[string]string,
) (uuid.UUID, bool) {
	if len(metadata) > 0 {
		ownerKind := strings.TrimSpace(metadata[objectstore.ArtifactMetaOwnerKind])
		ownerID := strings.TrimSpace(metadata[objectstore.ArtifactMetaOwnerID])
		if ownerKind != objectstore.ArtifactOwnerKindRun || ownerID == "" {
			return uuid.Nil, false
		}
		parsed, err := uuid.Parse(ownerID)
		if err == nil {
			return parsed, true
		}
		if shellSessionRepo != nil {
			accountID, accountErr := uuid.Parse(strings.TrimSpace(metadata[objectstore.ArtifactMetaAccountID]))
			if accountErr == nil {
				runID, lookupErr := shellSessionRepo.GetRunIDBySessionRef(ctx, accountID, ownerID)
				if lookupErr == nil && runID != nil && *runID != uuid.Nil {
					return *runID, true
				}
			}
		}
		return uuid.Nil, false
	}
	return resolveLegacyArtifactRunID(key)
}

func resolveLegacyArtifactRunID(key string) (uuid.UUID, bool) {
	parts := strings.Split(key, "/")
	if len(parts) < 3 {
		return uuid.Nil, false
	}
	parsed, err := uuid.Parse(strings.TrimSpace(parts[1]))
	if err != nil {
		return uuid.Nil, false
	}
	return parsed, true
}
