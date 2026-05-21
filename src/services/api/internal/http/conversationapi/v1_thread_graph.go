package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"fmt"
	"sort"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type threadGraphResponse struct {
	RootThreadID   string               `json:"root_thread_id"`
	ActiveThreadID string               `json:"active_thread_id"`
	Threads        []threadResponse     `json:"threads"`
	Messages       []threadGraphMessage `json:"messages"`
	Edges          []threadGraphEdge    `json:"edges"`
}

type threadGraphMessage struct {
	GraphNodeID       string                       `json:"graph_node_id"`
	ParentGraphNodeID *string                      `json:"parent_graph_node_id,omitempty"`
	Message           messageResponse              `json:"message"`
	Instances         []threadGraphMessageInstance `json:"instances"`
}

type threadGraphMessageInstance struct {
	ThreadID  string `json:"thread_id"`
	MessageID string `json:"message_id"`
	ThreadSeq int64  `json:"thread_seq"`
}

type threadGraphEdge struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Source         string `json:"source"`
	Target         string `json:"target"`
	SourceThreadID string `json:"source_thread_id,omitempty"`
	TargetThreadID string `json:"target_thread_id,omitempty"`
}

func getThreadGraph(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			writeInternalError(w, traceID, err)
			return
		}
		if thread == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}
		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.graph", thread, auditWriter) {
			return
		}

		threads, err := threadRepo.ListForkTree(r.Context(), thread.AccountID, threadID)
		if err != nil {
			writeInternalError(w, traceID, err)
			return
		}
		if len(threads) == 0 {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		threadIDs := make([]uuid.UUID, 0, len(threads))
		for _, item := range threads {
			threadIDs = append(threadIDs, item.ID)
		}
		messages, err := messageRepo.ListVisibleByThreads(r.Context(), thread.AccountID, threadIDs)
		if err != nil {
			writeInternalError(w, traceID, err)
			return
		}

		resp := buildThreadGraphResponse(threadID, threads, messages)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func buildThreadGraphResponse(activeThreadID uuid.UUID, threads []data.Thread, messages []data.Message) threadGraphResponse {
	threadsByID := make(map[uuid.UUID]data.Thread, len(threads))
	childrenByParent := make(map[uuid.UUID][]data.Thread)
	for _, thread := range threads {
		threadsByID[thread.ID] = thread
		if thread.ParentThreadID != nil {
			childrenByParent[*thread.ParentThreadID] = append(childrenByParent[*thread.ParentThreadID], thread)
		}
	}

	rootThreadID := threads[0].ID
	for _, thread := range threads {
		if thread.ParentThreadID == nil {
			rootThreadID = thread.ID
			break
		}
		if _, ok := threadsByID[derefUUID(thread.ParentThreadID)]; !ok {
			rootThreadID = thread.ID
			break
		}
	}

	messagesByThread := make(map[uuid.UUID][]data.Message)
	messageByID := make(map[uuid.UUID]data.Message, len(messages))
	for _, message := range messages {
		messagesByThread[message.ThreadID] = append(messagesByThread[message.ThreadID], message)
		messageByID[message.ID] = message
	}
	for threadID := range messagesByThread {
		sort.Slice(messagesByThread[threadID], func(i, j int) bool {
			return messagesByThread[threadID][i].ThreadSeq < messagesByThread[threadID][j].ThreadSeq
		})
	}

	for parentID := range childrenByParent {
		sort.Slice(childrenByParent[parentID], func(i, j int) bool {
			return childrenByParent[parentID][i].CreatedAt.Before(childrenByParent[parentID][j].CreatedAt)
		})
	}

	graphMessages := make(map[string]*threadGraphMessage)
	graphOrder := make([]string, 0, len(messages))
	edges := make(map[string]threadGraphEdge)
	graphByThreadVisibleIndex := make(map[uuid.UUID][]string)

	var visitThread func(thread data.Thread)
	visitThread = func(thread data.Thread) {
		visibleGraphIDs := make([]string, 0, len(messagesByThread[thread.ID]))
		parentVisibleGraphIDs := []string(nil)
		var branchSeq int64
		var branchGraphID string
		var branchVisibleCount int

		if thread.ParentThreadID != nil {
			parentVisibleGraphIDs = graphByThreadVisibleIndex[*thread.ParentThreadID]
		}
		if thread.BranchedFromMessageID != nil {
			if branchMessage, ok := messageByID[*thread.BranchedFromMessageID]; ok {
				branchSeq = branchMessage.ThreadSeq
				if thread.ParentThreadID != nil {
					for _, parentMessage := range messagesByThread[*thread.ParentThreadID] {
						if parentMessage.ThreadSeq > branchSeq {
							break
						}
						branchVisibleCount++
					}
				}
				if branchVisibleCount > 0 && branchVisibleCount <= len(parentVisibleGraphIDs) {
					branchGraphID = parentVisibleGraphIDs[branchVisibleCount-1]
				}
			}
		}

		var previousGraphID string
		for index, message := range messagesByThread[thread.ID] {
			graphID := ""
			if parentVisibleGraphIDs != nil && branchVisibleCount > 0 && index < branchVisibleCount {
				graphID = parentVisibleGraphIDs[index]
			}
			if graphID == "" {
				graphID = fmt.Sprintf("%s:%d", thread.ID.String(), message.ThreadSeq)
			}
			parentGraphID := previousGraphID
			if parentGraphID == "" && message.ThreadSeq > branchSeq && branchGraphID != "" {
				parentGraphID = branchGraphID
			}
			if _, ok := graphMessages[graphID]; !ok {
				var parent *string
				if parentGraphID != "" {
					value := parentGraphID
					parent = &value
				}
				graphMessages[graphID] = &threadGraphMessage{
					GraphNodeID:       graphID,
					ParentGraphNodeID: parent,
					Message:           toMessageResponse(message),
				}
				graphOrder = append(graphOrder, graphID)
				if parentGraphID != "" {
					edgeID := parentGraphID + "->" + graphID
					edges[edgeID] = threadGraphEdge{
						ID:             edgeID,
						Kind:           "message",
						Source:         parentGraphID,
						Target:         graphID,
						SourceThreadID: thread.ID.String(),
						TargetThreadID: thread.ID.String(),
					}
				}
			}
			graphMessages[graphID].Instances = append(graphMessages[graphID].Instances, threadGraphMessageInstance{
				ThreadID:  thread.ID.String(),
				MessageID: message.ID.String(),
				ThreadSeq: message.ThreadSeq,
			})
			previousGraphID = graphID
			visibleGraphIDs = append(visibleGraphIDs, graphID)
		}

		graphByThreadVisibleIndex[thread.ID] = visibleGraphIDs
		for _, child := range childrenByParent[thread.ID] {
			visitThread(child)
		}
	}
	if root, ok := threadsByID[rootThreadID]; ok {
		visitThread(root)
	}

	threadResponses := make([]threadResponse, 0, len(threads))
	for _, thread := range threads {
		threadResponses = append(threadResponses, toThreadResponse(thread))
	}
	messageResponses := make([]threadGraphMessage, 0, len(graphOrder))
	for _, id := range graphOrder {
		messageResponses = append(messageResponses, *graphMessages[id])
	}
	edgeResponses := make([]threadGraphEdge, 0, len(edges))
	for _, edge := range edges {
		edgeResponses = append(edgeResponses, edge)
	}
	sort.Slice(edgeResponses, func(i, j int) bool {
		return edgeResponses[i].ID < edgeResponses[j].ID
	})

	return threadGraphResponse{
		RootThreadID:   rootThreadID.String(),
		ActiveThreadID: activeThreadID.String(),
		Threads:        threadResponses,
		Messages:       messageResponses,
		Edges:          edgeResponses,
	}
}

func derefUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}
