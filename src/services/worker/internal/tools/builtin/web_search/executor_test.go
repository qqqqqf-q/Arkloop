package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/tools"
)

func TestParseArgsAcceptQueriesArray(t *testing.T) {
	queries, maxResults, err := parseArgs(map[string]any{
		"queries":     []any{"  q1 ", "q2"},
		"max_results": float64(3),
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if maxResults != 3 {
		t.Fatalf("expected maxResults=3, got %d", maxResults)
	}
	if len(queries) != 2 || queries[0] != "q1" || queries[1] != "q2" {
		t.Fatalf("unexpected queries: %#v", queries)
	}
}

func TestParseArgsAcceptQueriesWithBlankQuery(t *testing.T) {
	queries, maxResults, err := parseArgs(map[string]any{
		"query":       "  ",
		"queries":     []any{"q1", "q2"},
		"max_results": 2,
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if maxResults != 2 {
		t.Fatalf("expected maxResults=2, got %d", maxResults)
	}
	if len(queries) != 2 || queries[0] != "q1" || queries[1] != "q2" {
		t.Fatalf("unexpected queries: %#v", queries)
	}
}

func TestParseArgsRejectsQueryAndQueriesTogether(t *testing.T) {
	_, _, err := parseArgs(map[string]any{
		"query":   "q1",
		"queries": []any{"q2", "q3"},
	})
	if err == nil {
		t.Fatal("expected parseArgs to fail")
	}
	if err.ErrorClass != errorArgsInvalid {
		t.Fatalf("unexpected error class: %s", err.ErrorClass)
	}
	if !strings.Contains(err.Message, "either query or queries") {
		t.Fatalf("unexpected message: %q", err.Message)
	}
}

func TestParseArgsRejectTooManyQueries(t *testing.T) {
	_, _, err := parseArgs(map[string]any{
		"queries":     []any{"q1", "q2", "q3", "q4", "q5", "q6"},
		"max_results": 1,
	})
	if err == nil {
		t.Fatal("expected parseArgs to fail")
	}
	if err.ErrorClass != errorArgsInvalid {
		t.Fatalf("unexpected error class: %s", err.ErrorClass)
	}
}

func TestParseArgsRejectsMissingQueryAndQueries(t *testing.T) {
	_, _, err := parseArgs(map[string]any{
		"max_results": 5,
	})
	if err == nil {
		t.Fatal("expected error when query and queries omitted")
	}
	if err.ErrorClass != errorArgsInvalid {
		t.Fatalf("error class: %s", err.ErrorClass)
	}
}

func TestParseArgsDefaultsMaxResults(t *testing.T) {
	queries, maxResults, err := parseArgs(map[string]any{
		"query": "q1",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if maxResults != defaultMaxResults {
		t.Fatalf("expected maxResults=%d, got %d", defaultMaxResults, maxResults)
	}
	if len(queries) != 1 || queries[0] != "q1" {
		t.Fatalf("unexpected queries: %#v", queries)
	}
}

func TestExecuteMultiSearchPartialFailure(t *testing.T) {
	executor := &ToolExecutor{
		provider: stubProvider{
			resultsByQuery: map[string][]Result{
				"ok": {{Title: "A", URL: "https://a.example"}},
			},
			errorsByQuery: map[string]error{
				"bad": HttpError{StatusCode: 500},
			},
		},
		timeout: 2 * time.Second,
	}

	result := executor.Execute(
		context.Background(),
		"web_search",
		map[string]any{
			"queries":     []any{"ok", "bad"},
			"max_results": 5,
		},
		tools.ExecutionContext{},
		"call_1",
	)
	if result.Error != nil {
		t.Fatalf("expected partial success, got error: %#v", result.Error)
	}

	meta, ok := result.ResultJSON["meta"].(map[string]any)
	if !ok {
		t.Fatalf("missing meta in result: %#v", result.ResultJSON)
	}
	if meta["succeeded_queries"] != 1 {
		t.Fatalf("expected succeeded_queries=1, got %#v", meta["succeeded_queries"])
	}
	if meta["failed_queries"] != 1 {
		t.Fatalf("expected failed_queries=1, got %#v", meta["failed_queries"])
	}
}

func TestExecuteMultiSearchAllFailed(t *testing.T) {
	executor := &ToolExecutor{
		provider: stubProvider{
			errorsByQuery: map[string]error{
				"q1": errors.New("boom"),
				"q2": errors.New("boom"),
			},
		},
		timeout: 2 * time.Second,
	}

	result := executor.Execute(
		context.Background(),
		"web_search",
		map[string]any{
			"queries":     []any{"q1", "q2"},
			"max_results": 5,
		},
		tools.ExecutionContext{},
		"call_1",
	)
	if result.Error == nil {
		t.Fatal("expected error when all queries fail")
	}
	if result.Error.ErrorClass != errorSearchFailed {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func TestBuildSearchPayloadSlimByQuery(t *testing.T) {
	payload, err := buildSearchPayload([]searchJobResult{
		{
			Query: "ok",
			Results: []Result{
				{Title: "A", URL: "https://a.example", Snippet: "x"},
				{Title: "B", URL: "https://b.example", Snippet: "y"},
			},
		},
		{
			Query: "bad",
			Err:   HttpError{StatusCode: 500},
		},
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("buildSearchPayload returned error: %#v", err)
	}

	results, ok := payload["results"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected results type: %T", payload["results"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, item := range results {
		if item["query_index"] != 0 {
			t.Fatalf("expected query_index=0, got %#v", item["query_index"])
		}
	}

	byQuery, ok := payload["by_query"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected by_query type: %T", payload["by_query"])
	}
	if len(byQuery) != 2 {
		t.Fatalf("expected 2 by_query entries, got %d", len(byQuery))
	}
	if byQuery[0]["query_index"] != 0 || byQuery[1]["query_index"] != 1 {
		t.Fatalf("unexpected query_index in by_query: %#v", byQuery)
	}
	if _, has := byQuery[0]["results"]; has {
		t.Fatalf("by_query should not include results field: %#v", byQuery[0])
	}
	if byQuery[0]["result_count"] != 2 {
		t.Fatalf("expected result_count=2, got %#v", byQuery[0]["result_count"])
	}
	if _, has := byQuery[1]["results"]; has {
		t.Fatalf("by_query should not include results field: %#v", byQuery[1])
	}
	if byQuery[1]["result_count"] != 0 {
		t.Fatalf("expected result_count=0, got %#v", byQuery[1]["result_count"])
	}
	if _, has := byQuery[1]["error"]; !has {
		t.Fatalf("expected error payload in failed by_query: %#v", byQuery[1])
	}
}

func TestResultToJSONTruncatesAndNormalizes(t *testing.T) {
	item := Result{
		Title:   strings.Repeat("a", 200),
		URL:     "  https://x.example  ",
		Snippet: " \n\n" + strings.Repeat("b", 300) + "\n  c  ",
	}

	payload := item.ToJSON()
	title, _ := payload["title"].(string)
	if len(title) != 120 {
		t.Fatalf("expected title truncated to 120 chars, got %d", len(title))
	}

	urlText, _ := payload["url"].(string)
	if urlText != "https://x.example" {
		t.Fatalf("expected url trimmed, got %q", urlText)
	}

	snippet, _ := payload["snippet"].(string)
	if len(snippet) != 240 {
		t.Fatalf("expected snippet truncated to 240 chars, got %d", len(snippet))
	}
	if strings.Contains(snippet, "\n") || strings.Contains(snippet, "\t") {
		t.Fatalf("expected snippet whitespace normalized, got %q", snippet)
	}
	if strings.Contains(snippet, "  ") {
		t.Fatalf("expected snippet single-spaced, got %q", snippet)
	}
}

func TestBuildSearchPayloadJSONSizeBounded(t *testing.T) {
	results := make([]Result, 0, 5)
	for i := 0; i < 5; i++ {
		results = append(results, Result{
			Title:   strings.Repeat("t", 250),
			URL:     "https://example.com/" + strings.Repeat("p", i+1),
			Snippet: strings.Repeat("s", 2000),
		})
	}

	payload, err := buildSearchPayload([]searchJobResult{
		{Query: "q1", Results: results},
		{Query: "q2", Results: results},
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("buildSearchPayload returned error: %#v", err)
	}

	encoded, encErr := json.Marshal(payload)
	if encErr != nil {
		t.Fatalf("json.Marshal failed: %v", encErr)
	}
	if len(encoded) > 8000 {
		t.Fatalf("expected payload JSON to be small, got %d bytes", len(encoded))
	}
}

type stubProvider struct {
	resultsByQuery map[string][]Result
	errorsByQuery  map[string]error
}

func (s stubProvider) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	_ = ctx
	_ = maxResults
	if err, ok := s.errorsByQuery[query]; ok {
		return nil, err
	}
	if results, ok := s.resultsByQuery[query]; ok {
		return results, nil
	}
	return []Result{}, nil
}
