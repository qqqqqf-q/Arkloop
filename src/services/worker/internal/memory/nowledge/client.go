package nowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/memory"
)

const (
	memoryURIPrefix = "nowledge://memory/"
	threadURIPrefix = "nowledge://thread/"
	defaultSource   = "arkloop"
)

type WorkingMemory struct {
	Content   string
	Available bool
}

type WorkingMemoryPatch struct {
	Content *string
	Append  *string
}

type Status struct {
	Mode                   string
	BaseURL                string
	APIKeyConfigured       bool
	Healthy                bool
	Version                string
	DatabaseConnected      *bool
	WorkingMemoryAvailable *bool
	Error                  string
}

type MemoryDetail struct {
	ID             string
	Title          string
	Content        string
	SourceThreadID string
}

type MemorySnippet struct {
	MemoryDetail
	Text       string
	StartLine  int
	EndLine    int
	TotalLines int
}

type SearchResult struct {
	Kind            string
	ID              string
	Title           string
	Content         string
	Score           float64
	Importance      float64
	RelevanceReason string
	Labels          []string
	SourceThreadID  string
	ThreadID        string
	MatchedSnippet  string
	Snippets        []string
	RelatedThreads  []ThreadSearchResult
}

type ListedMemory struct {
	ID         string
	Title      string
	Content    string
	Rating     float64
	Time       string
	LabelIDs   []string
	IsFavorite bool
	Confidence float64
	Source     string
}

type ThreadMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Timestamp string         `json:"timestamp,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ThreadSearchResult struct {
	ThreadID       string
	Title          string
	Source         string
	MessageCount   int
	Score          float64
	Snippets       []string
	MatchedSnippet string
}

type ThreadFetchResult struct {
	ThreadID     string
	Title        string
	Source       string
	MessageCount int
	Messages     []ThreadMessage
}

type TriageResult struct {
	ShouldDistill bool
	Reason        string
}

type DistillResult struct {
	MemoriesCreated int
}

type GraphConnection struct {
	MemoryID   string
	NodeID     string
	NodeType   string
	Title      string
	Snippet    string
	EdgeType   string
	Relation   string
	Weight     float64
	SourceType string
}

type TimelineEvent struct {
	ID               string
	EventType        string
	Label            string
	Title            string
	Description      string
	CreatedAt        string
	MemoryID         string
	RelatedMemoryIDs []string
}

type ThreadAppender interface {
	CreateThread(ctx context.Context, ident memory.MemoryIdentity, threadID, title, source string, messages []ThreadMessage) (string, error)
	AppendThread(ctx context.Context, ident memory.MemoryIdentity, threadID string, messages []ThreadMessage, idempotencyKey string) (int, error)
}

type ThreadReader interface {
	SearchThreads(ctx context.Context, ident memory.MemoryIdentity, query string, limit int) (map[string]any, error)
	FetchThread(ctx context.Context, ident memory.MemoryIdentity, threadID string, offset, limit int) (map[string]any, error)
}

type ContextReader interface {
	ReadWorkingMemory(ctx context.Context, ident memory.MemoryIdentity) (WorkingMemory, error)
	SearchRich(ctx context.Context, ident memory.MemoryIdentity, query string, limit int) ([]SearchResult, error)
}

type Distiller interface {
	TriageConversation(ctx context.Context, ident memory.MemoryIdentity, content string) (TriageResult, error)
	DistillThread(ctx context.Context, ident memory.MemoryIdentity, threadID, title, content string) (DistillResult, error)
}

type GraphReader interface {
	Connections(ctx context.Context, ident memory.MemoryIdentity, memoryID string, depth, limit int) ([]GraphConnection, error)
	Timeline(ctx context.Context, ident memory.MemoryIdentity, lastNDays int, dateFrom, dateTo, eventType string, tier1Only bool, limit int) ([]TimelineEvent, error)
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(cfg Config) *Client {
	baseURL, err := sharedoutbound.DefaultPolicy().NormalizeBaseURL(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		baseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		http:    sharedoutbound.DefaultPolicy().NewHTTPClient(time.Duration(cfg.resolvedTimeoutMs()) * time.Millisecond),
	}
}

func (c *Client) Find(ctx context.Context, ident memory.MemoryIdentity, _ string, query string, limit int) ([]memory.MemoryHit, error) {
	results, err := c.SearchRich(ctx, ident, query, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]memory.MemoryHit, 0, len(results))
	for _, result := range results {
		hits = append(hits, memory.MemoryHit{
			URI:         memoryURIPrefix + result.ID,
			Abstract:    firstNonEmpty(result.Title, result.Content),
			Score:       result.Score,
			MatchReason: result.RelevanceReason,
			IsLeaf:      true,
		})
	}
	return hits, nil
}

func (c *Client) Content(ctx context.Context, ident memory.MemoryIdentity, uri string, layer memory.MemoryLayer) (string, error) {
	detail, err := c.MemoryDetail(ctx, ident, uri)
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(detail.Title)
	content := strings.TrimSpace(detail.Content)
	if title == "" {
		return content, nil
	}
	switch layer {
	case memory.MemoryLayerAbstract:
		if content == "" {
			return title, nil
		}
		return title + "\n" + content, nil
	default:
		if content == "" {
			return title, nil
		}
		return title + "\n\n" + content, nil
	}
}

func (c *Client) MemoryDetail(ctx context.Context, ident memory.MemoryIdentity, uri string) (MemoryDetail, error) {
	memoryID, err := parseMemoryURI(uri)
	if err != nil {
		return MemoryDetail{}, err
	}
	var response struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Content      string `json:"content"`
		SourceThread any    `json:"source_thread"`
		Metadata     struct {
			SourceThreadID string `json:"source_thread_id"`
		} `json:"metadata"`
		SourceThreadID string `json:"source_thread_id"`
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, "/memories/"+url.PathEscape(memoryID), nil, &response); err != nil {
		return MemoryDetail{}, err
	}
	return MemoryDetail{
		ID:             strings.TrimSpace(response.ID),
		Title:          strings.TrimSpace(response.Title),
		Content:        strings.TrimSpace(response.Content),
		SourceThreadID: extractSourceThreadID(response.SourceThread, response.SourceThreadID, response.Metadata.SourceThreadID),
	}, nil
}

func (c *Client) MemorySnippet(ctx context.Context, ident memory.MemoryIdentity, uri string, fromLine, lineCount int) (MemorySnippet, error) {
	detail, err := c.MemoryDetail(ctx, ident, uri)
	if err != nil {
		return MemorySnippet{}, err
	}
	text, startLine, endLine, totalLines := sliceLines(detail.Content, fromLine, lineCount)
	return MemorySnippet{
		MemoryDetail: detail,
		Text:         text,
		StartLine:    startLine,
		EndLine:      endLine,
		TotalLines:   totalLines,
	}, nil
}

func (c *Client) ListDir(context.Context, memory.MemoryIdentity, string) ([]string, error) {
	return nil, nil
}

func (c *Client) ListMemories(ctx context.Context, ident memory.MemoryIdentity, limit int) ([]ListedMemory, error) {
	const maxPageSize = 100

	type listMemoriesResponse struct {
		Memories []struct {
			ID         string   `json:"id"`
			Title      string   `json:"title"`
			Content    string   `json:"content"`
			Rating     float64  `json:"rating"`
			Time       string   `json:"time"`
			LabelIDs   []string `json:"label_ids"`
			IsFavorite bool     `json:"is_favorite"`
			Confidence float64  `json:"confidence"`
			Source     string   `json:"source"`
		} `json:"memories"`
		Pagination struct {
			Total   int  `json:"total"`
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
	}

	target := limit
	if target < 0 {
		target = 0
	}
	offset := 0
	out := make([]ListedMemory, 0, min(limit, maxPageSize))
	for {
		pageSize := maxPageSize
		if target > 0 {
			remaining := target - len(out)
			if remaining <= 0 {
				break
			}
			pageSize = min(remaining, maxPageSize)
		}

		values := url.Values{}
		values.Set("limit", fmt.Sprintf("%d", pageSize))
		if offset > 0 {
			values.Set("offset", fmt.Sprintf("%d", offset))
		}

		var response listMemoriesResponse
		path := "/memories?" + values.Encode()
		if err := c.doJSON(ctx, ident, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}

		for _, item := range response.Memories {
			out = append(out, ListedMemory{
				ID:         strings.TrimSpace(item.ID),
				Title:      strings.TrimSpace(item.Title),
				Content:    strings.TrimSpace(item.Content),
				Rating:     item.Rating,
				Time:       strings.TrimSpace(item.Time),
				LabelIDs:   append([]string(nil), item.LabelIDs...),
				IsFavorite: item.IsFavorite,
				Confidence: item.Confidence,
				Source:     strings.TrimSpace(item.Source),
			})
		}

		offset += len(response.Memories)
		if len(response.Memories) == 0 {
			break
		}
		if target > 0 && len(out) >= target {
			break
		}
		if !response.Pagination.HasMore {
			if response.Pagination.Total <= 0 || offset >= response.Pagination.Total {
				break
			}
		}
	}
	return out, nil
}

func (c *Client) ListFragments(ctx context.Context, ident memory.MemoryIdentity, limit int) ([]memory.MemoryFragment, error) {
	listed, err := c.ListMemories(ctx, ident, limit)
	if err != nil {
		return nil, err
	}
	fragments := make([]memory.MemoryFragment, 0, len(listed))
	for _, item := range listed {
		score := item.Confidence
		if score == 0 {
			score = item.Rating
		}
		fragments = append(fragments, memory.MemoryFragment{
			ID:          item.ID,
			URI:         memoryURIPrefix + item.ID,
			Title:       item.Title,
			Content:     item.Content,
			Abstract:    firstNonEmpty(item.Title, compactContent(item.Content, 160)),
			Score:       score,
			Labels:      append([]string(nil), item.LabelIDs...),
			RecordedAt:  item.Time,
			IsEphemeral: false,
		})
	}
	return fragments, nil
}

func (c *Client) AppendSessionMessages(context.Context, memory.MemoryIdentity, string, []memory.MemoryMessage) error {
	return nil
}

func (c *Client) CommitSession(context.Context, memory.MemoryIdentity, string) error {
	return nil
}

func (c *Client) Write(ctx context.Context, ident memory.MemoryIdentity, _ memory.MemoryScope, entry memory.MemoryEntry) error {
	_, err := c.WriteReturningURI(ctx, ident, memory.MemoryScopeUser, entry)
	return err
}

func (c *Client) WriteReturningURI(ctx context.Context, ident memory.MemoryIdentity, _ memory.MemoryScope, entry memory.MemoryEntry) (string, error) {
	content, metadata := parseWritableEntry(entry.Content)
	if content == "" {
		return "", fmt.Errorf("nowledge write: content is empty")
	}
	body := map[string]any{
		"content": content,
	}
	if metadata.Title != "" {
		body["title"] = metadata.Title
	}
	if metadata.UnitType != "" {
		body["unit_type"] = metadata.UnitType
	}
	if len(metadata.Labels) > 0 {
		body["labels"] = metadata.Labels
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, ident, http.MethodPost, "/memories", body, &response); err != nil {
		return "", err
	}
	if response.ID == "" {
		return "", nil
	}
	return memoryURIPrefix + response.ID, nil
}

func (c *Client) Delete(ctx context.Context, ident memory.MemoryIdentity, uri string) error {
	memoryID, err := parseMemoryURI(uri)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, ident, http.MethodDelete, "/memories/"+url.PathEscape(memoryID), nil, nil)
}

func (c *Client) ReadWorkingMemory(ctx context.Context, ident memory.MemoryIdentity) (WorkingMemory, error) {
	var response struct {
		Content string `json:"content"`
		Exists  bool   `json:"exists"`
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, "/agent/working-memory", nil, &response); err != nil {
		return WorkingMemory{}, err
	}
	content := strings.TrimSpace(response.Content)
	return WorkingMemory{
		Content:   content,
		Available: response.Exists || content != "",
	}, nil
}

func (c *Client) UpdateWorkingMemory(ctx context.Context, ident memory.MemoryIdentity, content string) (WorkingMemory, error) {
	body := map[string]any{"content": strings.TrimSpace(content)}
	if err := c.doJSON(ctx, ident, http.MethodPut, "/agent/working-memory", body, nil); err != nil {
		return WorkingMemory{}, err
	}
	updated := strings.TrimSpace(content)
	return WorkingMemory{
		Content:   updated,
		Available: updated != "",
	}, nil
}

func (c *Client) PatchWorkingMemory(ctx context.Context, ident memory.MemoryIdentity, heading string, patch WorkingMemoryPatch) (WorkingMemory, error) {
	normalizedHeading := strings.TrimSpace(heading)
	if normalizedHeading == "" {
		return WorkingMemory{}, fmt.Errorf("nowledge working memory patch: heading is empty")
	}
	if patch.Content == nil && patch.Append == nil {
		return WorkingMemory{}, fmt.Errorf("nowledge working memory patch: content and append are both nil")
	}
	current, err := c.ReadWorkingMemory(ctx, ident)
	if err != nil {
		return WorkingMemory{}, err
	}
	if !current.Available || strings.TrimSpace(current.Content) == "" {
		return WorkingMemory{}, fmt.Errorf("nowledge working memory patch: working memory not available")
	}
	updated, ok := patchWorkingMemorySection(current.Content, normalizedHeading, patch)
	if !ok {
		return WorkingMemory{}, fmt.Errorf("nowledge working memory patch: section not found: %s", normalizedHeading)
	}
	return c.UpdateWorkingMemory(ctx, ident, updated)
}

func (c *Client) SearchRich(ctx context.Context, ident memory.MemoryIdentity, query string, limit int) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	values := url.Values{}
	values.Set("query", strings.TrimSpace(query))
	values.Set("limit", fmt.Sprintf("%d", limit))

	var response struct {
		Memories []struct {
			ID           string   `json:"id"`
			Title        string   `json:"title"`
			Content      string   `json:"content"`
			Confidence   float64  `json:"confidence"`
			Score        float64  `json:"score"`
			LabelIDs     []string `json:"label_ids"`
			Labels       []string `json:"labels"`
			SourceThread any      `json:"source_thread"`
			Metadata     struct {
				Importance      float64 `json:"importance"`
				SimilarityScore float64 `json:"similarity_score"`
				RelevanceReason string  `json:"relevance_reason"`
				SourceThreadID  string  `json:"source_thread_id"`
			} `json:"metadata"`
			RelevanceReason string `json:"relevance_reason"`
		} `json:"memories"`
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, "/memories/search?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	threadResults, err := c.searchThreadsResult(ctx, ident, query, limit, "")
	if err != nil {
		threadResults = nil
	}
	results := make([]SearchResult, 0, len(response.Memories))
	memoryThreadIDs := make(map[string]struct{}, len(response.Memories))
	for _, item := range response.Memories {
		sourceThreadID := extractSourceThreadID(item.SourceThread, "", item.Metadata.SourceThreadID)
		score := item.Score
		if score == 0 {
			score = item.Confidence
		}
		if score == 0 {
			score = item.Metadata.SimilarityScore
		}
		labels := append([]string(nil), item.Labels...)
		if len(labels) == 0 {
			labels = append(labels, item.LabelIDs...)
		}
		results = append(results, SearchResult{
			Kind:            "memory",
			ID:              strings.TrimSpace(item.ID),
			Title:           strings.TrimSpace(item.Title),
			Content:         strings.TrimSpace(item.Content),
			Score:           score,
			Importance:      item.Metadata.Importance,
			RelevanceReason: firstNonEmpty(item.RelevanceReason, item.Metadata.RelevanceReason),
			Labels:          labels,
			SourceThreadID:  sourceThreadID,
			RelatedThreads:  append([]ThreadSearchResult(nil), threadResults...),
		})
		if sourceThreadID != "" {
			memoryThreadIDs[sourceThreadID] = struct{}{}
		}
	}
	for _, thread := range threadResults {
		threadID := strings.TrimSpace(thread.ThreadID)
		if threadID == "" {
			continue
		}
		if _, duplicated := memoryThreadIDs[threadID]; duplicated {
			continue
		}
		title := firstNonEmpty(thread.Title, thread.MatchedSnippet)
		content := strings.TrimSpace(thread.MatchedSnippet)
		if content == "" && len(thread.Snippets) > 0 {
			content = strings.TrimSpace(thread.Snippets[0])
		}
		results = append(results, SearchResult{
			Kind:           "thread",
			ID:             threadID,
			ThreadID:       threadID,
			Title:          strings.TrimSpace(title),
			Content:        content,
			Score:          thread.Score,
			MatchedSnippet: strings.TrimSpace(thread.MatchedSnippet),
			Snippets:       append([]string(nil), thread.Snippets...),
		})
	}
	return results, nil
}

func (c *Client) CreateThread(ctx context.Context, ident memory.MemoryIdentity, threadID, title, source string, messages []ThreadMessage) (string, error) {
	body := map[string]any{
		"title":    strings.TrimSpace(title),
		"source":   firstNonEmpty(strings.TrimSpace(source), defaultSource),
		"messages": messages,
	}
	if strings.TrimSpace(threadID) != "" {
		body["thread_id"] = strings.TrimSpace(threadID)
	}
	var response struct {
		ID       string `json:"id"`
		ThreadID string `json:"thread_id"`
		Thread   struct {
			ThreadID string `json:"thread_id"`
		} `json:"thread"`
	}
	if err := c.doJSON(ctx, ident, http.MethodPost, "/threads", body, &response); err != nil {
		return "", err
	}
	return firstNonEmpty(response.ID, response.ThreadID, response.Thread.ThreadID, strings.TrimSpace(threadID)), nil
}

func (c *Client) AppendThread(ctx context.Context, ident memory.MemoryIdentity, threadID string, messages []ThreadMessage, idempotencyKey string) (int, error) {
	body := map[string]any{
		"messages":    messages,
		"deduplicate": true,
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		body["idempotency_key"] = strings.TrimSpace(idempotencyKey)
	}
	var response struct {
		MessagesAdded int `json:"messages_added"`
	}
	if err := c.doJSON(ctx, ident, http.MethodPost, "/threads/"+url.PathEscape(strings.TrimSpace(threadID))+"/append", body, &response); err != nil {
		return 0, err
	}
	return response.MessagesAdded, nil
}

func (c *Client) SearchThreads(ctx context.Context, ident memory.MemoryIdentity, query string, limit int) (map[string]any, error) {
	results, err := c.searchThreadsResult(ctx, ident, query, limit, "")
	if err != nil {
		return nil, err
	}
	return threadSearchPayload(results), nil
}

func (c *Client) SearchThreadsFull(ctx context.Context, ident memory.MemoryIdentity, query string, limit int, source string) (map[string]any, error) {
	results, err := c.searchThreadsResult(ctx, ident, query, limit, source)
	if err != nil {
		return nil, err
	}
	return threadSearchPayload(results), nil
}

func threadSearchPayload(results []ThreadSearchResult) map[string]any {
	threads := make([]map[string]any, 0, len(results))
	for _, item := range results {
		threads = append(threads, map[string]any{
			"thread_id":       item.ThreadID,
			"title":           item.Title,
			"source":          item.Source,
			"message_count":   item.MessageCount,
			"score":           item.Score,
			"matched_snippet": item.MatchedSnippet,
			"snippets":        item.Snippets,
		})
	}
	return map[string]any{"threads": threads, "total_found": len(threads)}
}

func (c *Client) searchThreadsResult(ctx context.Context, ident memory.MemoryIdentity, query string, limit int, source string) ([]ThreadSearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	values := url.Values{}
	values.Set("query", strings.TrimSpace(query))
	values.Set("mode", "full")
	values.Set("limit", fmt.Sprintf("%d", limit))
	if strings.TrimSpace(source) != "" {
		values.Set("source", strings.TrimSpace(source))
	}
	var response struct {
		Threads []struct {
			ID           string   `json:"id"`
			ThreadID     string   `json:"thread_id"`
			Title        string   `json:"title"`
			Source       string   `json:"source"`
			MessageCount int      `json:"message_count"`
			Score        float64  `json:"score"`
			Snippets     []string `json:"snippets"`
			Matches      []struct {
				Snippet string `json:"snippet"`
			} `json:"matches"`
		} `json:"threads"`
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, "/threads/search?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	results := make([]ThreadSearchResult, 0, len(response.Threads))
	for _, item := range response.Threads {
		snippets := append([]string(nil), item.Snippets...)
		if len(snippets) == 0 {
			for _, match := range item.Matches {
				if snippet := strings.TrimSpace(match.Snippet); snippet != "" {
					snippets = append(snippets, snippet)
				}
			}
		}
		results = append(results, ThreadSearchResult{
			ThreadID:       firstNonEmpty(item.ThreadID, item.ID),
			Title:          strings.TrimSpace(item.Title),
			Source:         strings.TrimSpace(item.Source),
			MessageCount:   item.MessageCount,
			Score:          item.Score,
			Snippets:       snippets,
			MatchedSnippet: firstNonEmpty(snippets...),
		})
	}
	return results, nil
}

func (c *Client) FetchThread(ctx context.Context, ident memory.MemoryIdentity, threadID string, offset, limit int) (map[string]any, error) {
	result, err := c.fetchThreadResult(ctx, ident, threadID, offset, limit)
	if err != nil {
		return nil, err
	}
	messages := make([]map[string]any, 0, len(result.Messages))
	for _, msg := range result.Messages {
		messages = append(messages, map[string]any{
			"role":      msg.Role,
			"content":   msg.Content,
			"timestamp": msg.Timestamp,
		})
	}
	return map[string]any{
		"thread_id":     result.ThreadID,
		"title":         result.Title,
		"source":        result.Source,
		"message_count": result.MessageCount,
		"messages":      messages,
	}, nil
}

func (c *Client) fetchThreadResult(ctx context.Context, ident memory.MemoryIdentity, threadID string, offset, limit int) (ThreadFetchResult, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		values.Set("offset", fmt.Sprintf("%d", offset))
	}
	var response struct {
		ID           string `json:"id"`
		ThreadID     string `json:"thread_id"`
		Title        string `json:"title"`
		Source       string `json:"source"`
		MessageCount int    `json:"message_count"`
		Messages     []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
			CreatedAt string `json:"created_at"`
		} `json:"messages"`
	}
	path := "/threads/" + url.PathEscape(strings.TrimSpace(threadID))
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, path, nil, &response); err != nil {
		return ThreadFetchResult{}, err
	}
	out := ThreadFetchResult{
		ThreadID:     firstNonEmpty(response.ThreadID, response.ID, strings.TrimSpace(threadID)),
		Title:        strings.TrimSpace(response.Title),
		Source:       strings.TrimSpace(response.Source),
		MessageCount: response.MessageCount,
		Messages:     make([]ThreadMessage, 0, len(response.Messages)),
	}
	for _, msg := range response.Messages {
		out.Messages = append(out.Messages, ThreadMessage{
			Role:      strings.TrimSpace(msg.Role),
			Content:   strings.TrimSpace(msg.Content),
			Timestamp: firstNonEmpty(msg.Timestamp, msg.CreatedAt),
		})
	}
	return out, nil
}

func (c *Client) TriageConversation(ctx context.Context, ident memory.MemoryIdentity, content string) (TriageResult, error) {
	var response struct {
		ShouldDistill bool   `json:"should_distill"`
		Reason        string `json:"reason"`
	}
	if err := c.doJSON(ctx, ident, http.MethodPost, "/memories/distill/triage", map[string]any{
		"thread_content": strings.TrimSpace(content),
	}, &response); err != nil {
		return TriageResult{}, err
	}
	return TriageResult{ShouldDistill: response.ShouldDistill, Reason: strings.TrimSpace(response.Reason)}, nil
}

func (c *Client) Connections(ctx context.Context, ident memory.MemoryIdentity, memoryID string, depth, limit int) ([]GraphConnection, error) {
	memoryID = strings.TrimSpace(memoryID)
	if memoryID == "" {
		return nil, fmt.Errorf("nowledge connections: memory_id is empty")
	}
	if depth <= 0 {
		depth = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var response struct {
		Neighbors []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Title       string `json:"title"`
			NodeType    string `json:"node_type"`
			Type        string `json:"type"`
			Content     string `json:"content"`
			Description string `json:"description"`
			Summary     string `json:"summary"`
			SourceType  string `json:"source_type"`
			Metadata    struct {
				Title      string `json:"title"`
				Content    string `json:"content"`
				SourceType string `json:"source_type"`
			} `json:"metadata"`
		} `json:"neighbors"`
		Edges []struct {
			Source     string  `json:"source"`
			Target     string  `json:"target"`
			EdgeType   string  `json:"edge_type"`
			Type       string  `json:"type"`
			Weight     float64 `json:"weight"`
			Label      string  `json:"label"`
			ContentRel string  `json:"content_relation"`
			Metadata   struct {
				RelationType string `json:"relation_type"`
			} `json:"metadata"`
		} `json:"edges"`
	}
	path := fmt.Sprintf("/graph/expand/%s?depth=%d&limit=%d", url.PathEscape(memoryID), depth, limit)
	if err := c.doJSON(ctx, ident, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	nodeByID := make(map[string]struct {
		Title      string
		NodeType   string
		Snippet    string
		SourceType string
	}, len(response.Neighbors))
	for _, node := range response.Neighbors {
		title := firstNonEmpty(node.Label, node.Metadata.Title, node.Title)
		snippet := compactContent(firstNonEmpty(node.Metadata.Content, node.Content, node.Summary, node.Description), 150)
		nodeByID[node.ID] = struct {
			Title      string
			NodeType   string
			Snippet    string
			SourceType string
		}{
			Title:      strings.TrimSpace(title),
			NodeType:   firstNonEmpty(node.NodeType, node.Type, "memory"),
			Snippet:    strings.TrimSpace(snippet),
			SourceType: firstNonEmpty(node.Metadata.SourceType, node.SourceType),
		}
	}
	out := make([]GraphConnection, 0, len(response.Edges))
	for _, edge := range response.Edges {
		neighborID := edge.Target
		if strings.TrimSpace(edge.Target) == memoryID {
			neighborID = edge.Source
		}
		node, ok := nodeByID[neighborID]
		if !ok {
			continue
		}
		out = append(out, GraphConnection{
			MemoryID:   memoryID,
			NodeID:     neighborID,
			NodeType:   node.NodeType,
			Title:      node.Title,
			Snippet:    node.Snippet,
			EdgeType:   firstNonEmpty(edge.EdgeType, edge.Type, "RELATED"),
			Relation:   firstNonEmpty(edge.Metadata.RelationType, edge.ContentRel, edge.Label),
			Weight:     edge.Weight,
			SourceType: node.SourceType,
		})
	}
	return out, nil
}

func (c *Client) Timeline(ctx context.Context, ident memory.MemoryIdentity, lastNDays int, dateFrom, dateTo, eventType string, tier1Only bool, limit int) ([]TimelineEvent, error) {
	if lastNDays <= 0 {
		lastNDays = 7
	}
	if limit <= 0 {
		limit = 100
	}
	values := url.Values{}
	values.Set("last_n_days", strconv.Itoa(lastNDays))
	values.Set("limit", strconv.Itoa(limit))
	if strings.TrimSpace(eventType) != "" {
		values.Set("event_type", strings.TrimSpace(eventType))
	}
	if strings.TrimSpace(dateFrom) != "" {
		values.Set("date_from", strings.TrimSpace(dateFrom))
	}
	if strings.TrimSpace(dateTo) != "" {
		values.Set("date_to", strings.TrimSpace(dateTo))
	}
	if !tier1Only {
		values.Set("tier1_only", "false")
	}
	var response struct {
		Events []struct {
			ID               string   `json:"id"`
			EventType        string   `json:"event_type"`
			Title            string   `json:"title"`
			Description      string   `json:"description"`
			Content          string   `json:"content"`
			CreatedAt        string   `json:"created_at"`
			Timestamp        string   `json:"timestamp"`
			MemoryID         string   `json:"memory_id"`
			RelatedMemoryIDs []string `json:"related_memory_ids"`
		} `json:"events"`
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, "/agent/feed/events?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	out := make([]TimelineEvent, 0, len(response.Events))
	for _, item := range response.Events {
		out = append(out, TimelineEvent{
			ID:               strings.TrimSpace(item.ID),
			EventType:        strings.TrimSpace(item.EventType),
			Label:            timelineLabelForType(item.EventType),
			Title:            strings.TrimSpace(firstNonEmpty(item.Title, item.Description, item.Content)),
			Description:      strings.TrimSpace(firstNonEmpty(item.Description, item.Content)),
			CreatedAt:        strings.TrimSpace(firstNonEmpty(item.CreatedAt, item.Timestamp)),
			MemoryID:         strings.TrimSpace(item.MemoryID),
			RelatedMemoryIDs: append([]string(nil), item.RelatedMemoryIDs...),
		})
	}
	return out, nil
}

func (c *Client) DistillThread(ctx context.Context, ident memory.MemoryIdentity, threadID, title, content string) (DistillResult, error) {
	var response struct {
		MemoriesCreated int   `json:"memories_created"`
		CreatedMemories []any `json:"created_memories"`
	}
	if err := c.doJSON(ctx, ident, http.MethodPost, "/memories/distill", map[string]any{
		"thread_id":         strings.TrimSpace(threadID),
		"thread_title":      strings.TrimSpace(title),
		"thread_content":    strings.TrimSpace(content),
		"distillation_type": "simple_llm",
		"extraction_level":  "swift",
	}, &response); err != nil {
		return DistillResult{}, err
	}
	count := response.MemoriesCreated
	if count == 0 {
		count = len(response.CreatedMemories)
	}
	return DistillResult{MemoriesCreated: count}, nil
}

func (c *Client) Status(ctx context.Context, ident memory.MemoryIdentity) (Status, error) {
	status := Status{
		Mode:             nowledgeMode(c.baseURL),
		BaseURL:          strings.TrimSpace(c.baseURL),
		APIKeyConfigured: strings.TrimSpace(c.apiKey) != "",
	}
	var response struct {
		Version           string `json:"version"`
		DatabaseConnected *bool  `json:"database_connected"`
	}
	if err := c.doJSON(ctx, ident, http.MethodGet, "/health", nil, &response); err != nil {
		status.Error = err.Error()
		return status, nil
	}
	wm, err := c.ReadWorkingMemory(ctx, ident)
	if err == nil {
		available := wm.Available
		status.WorkingMemoryAvailable = &available
	}
	status.Healthy = true
	status.Version = strings.TrimSpace(response.Version)
	status.DatabaseConnected = response.DatabaseConnected
	return status, nil
}

func (c *Client) doJSON(ctx context.Context, ident memory.MemoryIdentity, method, path string, body any, out any) error {
	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("nowledge marshal request: %w", err)
		}
		requestBody = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("nowledge build request: %w", err)
	}
	c.setHeaders(req, ident)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("nowledge %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("nowledge %s %s: status=%d body=%s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("nowledge decode response: %w", err)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request, ident memory.MemoryIdentity) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("x-nmem-api-key", c.apiKey)
	}
	req.Header.Set("X-Arkloop-Account", ident.AccountID.String())
	req.Header.Set("X-Arkloop-User", ident.UserID.String())
	req.Header.Set("X-Arkloop-Agent", sanitizeAgentID(ident.AgentID))
	req.Header.Set("X-Arkloop-App", defaultSource)
	if strings.TrimSpace(ident.ExternalUserID) != "" {
		req.Header.Set("X-Arkloop-External-User", strings.TrimSpace(ident.ExternalUserID))
	}
}

func BuildThreadMessageMetadata(source, sessionKey, sessionID, threadID, role, content string, index int, traceID string) map[string]any {
	externalID := buildExternalID(threadID, sessionKey, role, content, index)
	metadata := map[string]any{
		"external_id": externalID,
		"source":      firstNonEmpty(strings.TrimSpace(source), defaultSource),
	}
	if strings.TrimSpace(sessionKey) != "" {
		metadata["session_key"] = strings.TrimSpace(sessionKey)
	}
	if strings.TrimSpace(sessionID) != "" {
		metadata["session_id"] = strings.TrimSpace(sessionID)
	}
	if strings.TrimSpace(traceID) != "" {
		metadata["trace_id"] = strings.TrimSpace(traceID)
	}
	return metadata
}

func buildExternalID(threadID, sessionKey, role, content string, index int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(threadID),
		strings.TrimSpace(sessionKey),
		strings.TrimSpace(role),
		strconv.Itoa(index),
		strings.TrimSpace(content),
	}, "|")))
	return "arkloop_" + hex.EncodeToString(sum[:12])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func parseMemoryURI(uri string) (string, error) {
	value := strings.TrimSpace(uri)
	if !strings.HasPrefix(value, memoryURIPrefix) {
		return "", fmt.Errorf("invalid nowledge memory uri: %q", uri)
	}
	memoryID := strings.TrimSpace(strings.TrimPrefix(value, memoryURIPrefix))
	if memoryID == "" {
		return "", fmt.Errorf("invalid nowledge memory uri: %q", uri)
	}
	return memoryID, nil
}

func MemoryIDFromURI(uri string) (string, error) {
	return parseMemoryURI(uri)
}

func extractSourceThreadID(raw any, direct string, metadata string) string {
	if value := strings.TrimSpace(direct); value != "" {
		return value
	}
	if value := strings.TrimSpace(metadata); value != "" {
		return value
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		if rawID, ok := value["id"].(string); ok {
			return strings.TrimSpace(rawID)
		}
	}
	return ""
}

func timelineLabelForType(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "memory_created":
		return "Memory saved"
	case "insight_generated":
		return "Insight"
	case "pattern_detected":
		return "Pattern"
	case "agent_observation":
		return "Observation"
	case "daily_briefing":
		return "Daily briefing"
	case "crystal_created":
		return "Crystal"
	case "flag_contradiction":
		return "Flag: contradiction"
	case "flag_stale":
		return "Flag: stale"
	case "flag_merge_candidate":
		return "Flag: duplicate"
	case "flag_review":
		return "Flag: review"
	case "source_ingested":
		return "Document ingested"
	case "source_extracted":
		return "Knowledge extracted"
	case "working_memory_updated":
		return "Working Memory updated"
	case "evolves_detected":
		return "Knowledge evolution"
	case "kg_extraction":
		return "Entity extraction"
	case "url_captured":
		return "URL captured"
	default:
		return strings.TrimSpace(eventType)
	}
}

type writableMetadata struct {
	Title    string
	UnitType string
	Labels   []string
}

func parseWritableEntry(raw string) (string, writableMetadata) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", writableMetadata{}
	}
	meta := writableMetadata{UnitType: "context"}
	if strings.HasPrefix(text, "[") {
		if end := strings.Index(text, "] "); end > 1 {
			header := strings.TrimSpace(text[1:end])
			parts := strings.Split(header, "/")
			if len(parts) >= 3 {
				category := strings.TrimSpace(parts[1])
				key := strings.TrimSpace(parts[2])
				meta.Title = key
				if category != "" {
					meta.Labels = append(meta.Labels, category)
					meta.UnitType = categoryToUnitType(category)
				}
			}
			text = strings.TrimSpace(text[end+2:])
		}
	}
	if meta.Title == "" {
		meta.Title = firstLine(text)
	}
	return text, meta
}

func categoryToUnitType(category string) string {
	switch strings.TrimSpace(strings.ToLower(category)) {
	case "preferences", "profile":
		return "preference"
	case "events":
		return "event"
	case "cases":
		return "procedure"
	case "patterns":
		return "learning"
	case "entities":
		return "context"
	default:
		return "context"
	}
}

func sanitizeAgentID(value string) string {
	var builder strings.Builder
	for _, ch := range strings.TrimSpace(value) {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-' || ch == '_':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[:idx]
	}
	if len(text) > 80 {
		return text[:80]
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func compactContent(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func nowledgeMode(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	switch trimmed {
	case "", "http://127.0.0.1:14242", "http://localhost:14242":
		return "local"
	default:
		return "remote"
	}
}

func sliceLines(text string, fromLine, lineCount int) (string, int, int, int) {
	allLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start := fromLine
	if start <= 0 {
		start = 1
	}
	total := len(allLines)
	if total == 1 && allLines[0] == "" {
		total = 0
	}
	if total == 0 {
		return "", start, start, 0
	}
	maxLines := lineCount
	if maxLines <= 0 {
		maxLines = total
	}
	startIdx := start - 1
	if startIdx >= total {
		return "", start, start, total
	}
	endIdx := startIdx + maxLines
	if endIdx > total {
		endIdx = total
	}
	selected := allLines[startIdx:endIdx]
	endLine := start + len(selected) - 1
	if len(selected) == 0 {
		endLine = start
	}
	return strings.Join(selected, "\n"), start, endLine, total
}

func patchWorkingMemorySection(currentContent, heading string, patch WorkingMemoryPatch) (string, bool) {
	normalizedHeading := strings.ToLower(strings.TrimSpace(heading))
	lines := strings.Split(strings.ReplaceAll(currentContent, "\r\n", "\n"), "\n")
	startIdx := -1
	endIdx := len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "##") {
			continue
		}
		if startIdx == -1 && strings.Contains(strings.ToLower(trimmed), normalizedHeading) {
			startIdx = index
			continue
		}
		if startIdx != -1 {
			endIdx = index
			break
		}
	}
	if startIdx == -1 {
		return "", false
	}
	bodyStart := startIdx + 1
	for bodyStart < endIdx && strings.TrimSpace(lines[bodyStart]) == "" {
		bodyStart++
	}
	existingBody := strings.TrimSpace(strings.Join(lines[bodyStart:endIdx], "\n"))
	updatedBody := existingBody
	if patch.Content != nil {
		updatedBody = strings.TrimSpace(*patch.Content)
	}
	if patch.Append != nil {
		appendText := strings.TrimSpace(*patch.Append)
		switch {
		case updatedBody == "":
			updatedBody = appendText
		case appendText != "":
			updatedBody = updatedBody + "\n" + appendText
		}
	}
	replacement := []string{lines[startIdx]}
	if strings.TrimSpace(updatedBody) != "" {
		replacement = append(replacement, "")
		replacement = append(replacement, strings.Split(updatedBody, "\n")...)
	}
	updatedLines := append([]string{}, lines[:startIdx]...)
	updatedLines = append(updatedLines, replacement...)
	updatedLines = append(updatedLines, lines[endIdx:]...)
	return strings.Join(updatedLines, "\n"), true
}
