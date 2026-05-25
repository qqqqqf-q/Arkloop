# Ask User Form-In-Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit `ask_user display_mode=form` flow that renders a full form card inside the Web chat stream, persists it as a thread message, and converts it to a read-only full-response card after submission while keeping the existing inline flow unchanged.

**Architecture:** Keep run input control on the existing `run.input_requested -> /v1/runs/{id}/input -> run.input_provided` path, and add a parallel persistence path that stores form-mode requests as structured thread messages. The worker emits enough metadata for form-mode requests, the worker/app event writer creates and finalizes form messages, the API/Web types widen to support a new content kind, and the Web renderer switches form-mode requests from temporary composer UI to message-stream UI.

**Tech Stack:** Go 1.26, React 19, TypeScript 5.9, Vitest, pgx, existing Arkloop message content and SSE run-event plumbing.

---

## File Structure

### Backend protocol and worker plumbing

- Modify: `src/services/worker/internal/tools/builtin/askuser/spec.go`
- Modify: `src/services/worker/internal/tools/builtin/askuser/executor.go`
- Modify: `src/services/worker/internal/agent/loop.go`
- Modify: `src/services/worker/internal/executor/lua_test.go`
- Modify: `src/services/worker/internal/agent/loop_test.go`

### Worker/app persistence and terminal state transitions

- Modify: `src/services/worker/internal/pipeline/handler_agent_loop.go`
- Modify: `src/services/worker/internal/app/composition_desktop.go`
- Modify: `src/services/worker/internal/app/composition_desktop_test.go`
- Modify: `src/services/worker/internal/app/composition_desktop_readonly_test.go`

### API-side message helpers and input submission update

- Modify: `src/services/api/internal/data/messages_repo.go`
- Modify: `src/services/api/internal/http/conversationapi/v1_runs.go`
- Modify: `src/services/api/internal/data/runs_repo_desktop_test.go`
- Modify: `src/services/api/internal/http/conversationapi/v1_messages.go`
- Modify: `src/services/api/internal/http/conversationapi/message_content.go`

### Web types, adapters, and rendering

- Modify: `src/apps/web/src/api.ts`
- Modify: `src/apps/web/src/agent-ui/contract.ts`
- Modify: `src/apps/web/src/agent-ui/arkloop-adapter.ts`
- Modify: `src/apps/web/src/messageContent.ts`
- Modify: `src/apps/web/src/hooks/useThreadSseEffect.ts`
- Modify: `src/apps/web/src/hooks/useChatActions.ts`
- Modify: `src/apps/web/src/components/ChatView.tsx`
- Modify: `src/apps/web/src/components/MessageList.tsx`
- Create: `src/apps/web/src/components/AskUserFormMessageCard.tsx`

### Web tests

- Modify: `src/apps/web/src/__tests__/userInputCard.test.tsx`
- Modify: `src/apps/web/src/__tests__/chatPageLoading.test.tsx`

---

### Task 1: Extend `ask_user` Contract for Explicit Form Mode

**Files:**
- Modify: `src/services/worker/internal/tools/builtin/askuser/spec.go`
- Modify: `src/services/worker/internal/tools/builtin/askuser/executor.go`
- Modify: `src/services/worker/internal/agent/loop.go`
- Test: `src/services/worker/internal/executor/lua_test.go`
- Test: `src/services/worker/internal/agent/loop_test.go`

- [ ] **Step 1: Write failing worker tests for `display_mode` validation and event emission**

```go
func TestValidateAndNormalizeAcceptsDisplayModeForm(t *testing.T) {
	args := map[string]any{
		"message": "Choose deployment options",
		"display_mode": "form",
		"fields": []any{
			map[string]any{
				"key":      "region",
				"type":     "string",
				"enum":     []any{"us-east-1", "ap-southeast-1"},
				"required": true,
			},
		},
	}

	message, schema, err := ValidateAndNormalize(args)
	if err != nil {
		t.Fatalf("ValidateAndNormalize returned error: %v", err)
	}
	if message != "Choose deployment options" {
		t.Fatalf("unexpected message: %q", message)
	}
	if got, _ := schema["display_mode"].(string); got != "form" {
		t.Fatalf("display_mode = %q, want form", got)
	}
}

func TestValidateAndNormalizeRejectsUnknownDisplayMode(t *testing.T) {
	_, _, err := ValidateAndNormalize(map[string]any{
		"message": "bad mode",
		"display_mode": "wizard",
		"fields": []any{
			map[string]any{"key": "ok", "type": "string"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "display_mode") {
		t.Fatalf("expected display_mode error, got %v", err)
	}
}
```

```go
func TestLuaExecutor_AgentLoop_AskUserFormIncludesDisplayMode(t *testing.T) {
	gw := &captureGateway{
		events: [][]llm.StreamEvent{{
			llm.ToolCall{
				ToolCallID: "call_ask_user_form",
				ToolName:   "ask_user",
				ArgumentsJSON: map[string]any{
					"message":      "Fill release form",
					"display_mode": "form",
					"fields": []any{
						map[string]any{"key": "version", "type": "string", "required": true},
					},
				},
			},
			llm.StreamRunCompleted{},
		}},
	}

	rc := buildLuaRC(gw)
	rc.WaitForInput = func(_ context.Context) (string, bool) { return `{"version":"1.2.3"}`, true }

	evs := runLuaScript(t, `local ok, err = agent.loop("system", "query"); if err then error(err) end`, rc)

	found := false
	for _, ev := range evs {
		if ev.Type == "run.input_requested" {
			if got, _ := ev.DataJSON["display_mode"].(string); got == "form" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected run.input_requested to include display_mode=form")
	}
}
```

- [ ] **Step 2: Run worker tests to verify they fail before implementation**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/tools/builtin/askuser ./internal/agent ./internal/executor -run 'TestValidateAndNormalize|TestLuaExecutor_AgentLoop_AskUserFormIncludesDisplayMode|TestAskUserLoopIntercept'
```

Expected:

```text
FAIL ... TestValidateAndNormalizeAcceptsDisplayModeForm
FAIL ... TestLuaExecutor_AgentLoop_AskUserFormIncludesDisplayMode
```

- [ ] **Step 3: Extend the tool schema and normalization result**

```go
// src/services/worker/internal/tools/builtin/askuser/spec.go
"display_mode": map[string]any{
	"type":        "string",
	"enum":        []string{"inline", "form"},
	"description": "How the client should present the question. inline keeps the temporary composer card, form persists a card in the chat stream.",
},
```

```go
// src/services/worker/internal/tools/builtin/askuser/executor.go
func ValidateAndNormalize(args map[string]any) (string, map[string]any, error) {
	message, _ := args["message"].(string)
	if message == "" {
		return "", nil, fmt.Errorf("missing required field: message")
	}

	displayMode, _ := args["display_mode"].(string)
	displayMode = strings.TrimSpace(displayMode)
	if displayMode == "" {
		displayMode = "inline"
	}
	if displayMode != "inline" && displayMode != "form" {
		return "", nil, fmt.Errorf("display_mode must be one of inline, form")
	}

	// existing fields validation stays in place...

	schema := map[string]any{
		"properties":   properties,
		"display_mode": displayMode,
	}
	if len(orderedKeys) > 0 {
		schema["_fieldOrder"] = orderedKeys
	}
	if len(requiredKeys) > 0 {
		schema["required"] = requiredKeys
	}
	return message, schema, nil
}
```

- [ ] **Step 4: Include `display_mode` in `run.input_requested` events**

```go
// src/services/worker/internal/agent/loop.go
displayMode := "inline"
if raw, _ := schema["display_mode"].(string); strings.TrimSpace(raw) != "" {
	displayMode = strings.TrimSpace(raw)
}

if err := yield(emitter.Emit("run.input_requested", map[string]any{
	"request_id":      requestID,
	"message":         message,
	"requestedSchema": schema,
	"display_mode":    displayMode,
}, nil, nil)); err != nil {
	return err
}
```

- [ ] **Step 5: Re-run worker tests to verify the contract change passes**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/tools/builtin/askuser ./internal/agent ./internal/executor -run 'TestValidateAndNormalize|TestLuaExecutor_AgentLoop_AskUserFormIncludesDisplayMode|TestAskUserLoopIntercept'
```

Expected:

```text
ok  	arkloop/services/worker/internal/tools/builtin/askuser	...
ok  	arkloop/services/worker/internal/agent	...
ok  	arkloop/services/worker/internal/executor	...
```

- [ ] **Step 6: Commit the protocol change**

```bash
git add src/services/worker/internal/tools/builtin/askuser/spec.go \
        src/services/worker/internal/tools/builtin/askuser/executor.go \
        src/services/worker/internal/agent/loop.go \
        src/services/worker/internal/executor/lua_test.go \
        src/services/worker/internal/agent/loop_test.go
git commit -m "feat: add ask_user display mode"
```

### Task 2: Persist Pending Form Requests as Thread Messages

**Files:**
- Modify: `src/services/api/internal/data/messages_repo.go`
- Modify: `src/services/worker/internal/pipeline/handler_agent_loop.go`
- Modify: `src/services/worker/internal/app/composition_desktop.go`
- Test: `src/services/worker/internal/app/composition_desktop_test.go`

- [ ] **Step 1: Write a failing integration test for persistent pending form messages**

```go
func TestAskUserFormCreatesPendingThreadMessage(t *testing.T) {
	// Arrange a run that emits ask_user with display_mode=form and then pauses.
	// Assert that one thread message exists with a structured ask_user_form payload.
	msgs, err := messageRepo.ListByThread(ctx, run.AccountID, run.ThreadID, 50, 0)
	if err != nil {
		t.Fatalf("ListByThread failed: %v", err)
	}

	var found bool
	for _, msg := range msgs {
		var payload map[string]any
		if err := json.Unmarshal(msg.ContentJSON, &payload); err != nil {
			continue
		}
		if payload["kind"] == "ask_user_form" && payload["status"] == "pending" {
			found = true
			if payload["run_id"] != run.ID.String() {
				t.Fatalf("run_id mismatch: %#v", payload)
			}
		}
	}
	if !found {
		t.Fatal("expected pending ask_user_form message")
	}
}
```

- [ ] **Step 2: Run the worker/app test to confirm no message is persisted yet**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/app -run TestAskUserFormCreatesPendingThreadMessage
```

Expected:

```text
FAIL ... expected pending ask_user_form message
```

- [ ] **Step 3: Add repository helpers for creating and looking up form messages**

```go
// src/services/api/internal/data/messages_repo.go
type AskUserFormMessage struct {
	RunID       uuid.UUID       `json:"run_id"`
	RequestID   string          `json:"request_id"`
	DisplayMode string          `json:"display_mode"`
	Message     string          `json:"message"`
	Schema      json.RawMessage `json:"schema"`
	Status      string          `json:"status"`
	Answers     json.RawMessage `json:"answers,omitempty"`
	SubmittedAt *time.Time      `json:"submitted_at,omitempty"`
}

func (r *MessageRepository) CreateAskUserFormMessage(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	requestID string,
	prompt string,
	schema json.RawMessage,
) (Message, error) {
	content := AskUserFormMessage{
		RunID:       runID,
		RequestID:   requestID,
		DisplayMode: "form",
		Message:     prompt,
		Schema:      schema,
		Status:      "pending",
	}
	contentJSON, _ := json.Marshal(map[string]any{
		"kind":         "ask_user_form",
		"display_mode": content.DisplayMode,
		"run_id":       content.RunID.String(),
		"request_id":   content.RequestID,
		"message":      content.Message,
		"schema":       json.RawMessage(content.Schema),
		"status":       content.Status,
		"answers":      nil,
		"submitted_at": nil,
	})
	metadataJSON, _ := json.Marshal(map[string]string{"run_id": runID.String()})
	return r.CreateStructuredWithMetadata(ctx, accountID, threadID, "assistant", prompt, contentJSON, metadataJSON, nil)
}

func (r *MessageRepository) FindAskUserFormMessage(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	requestID string,
) (*Message, error) {
	// Query messages where metadata_json->>'run_id' matches and content_json->>'request_id' matches.
}
```

- [ ] **Step 4: Persist form-mode requests when `run.input_requested` events are appended**

```go
// src/services/worker/internal/pipeline/handler_agent_loop.go
if ev.Type == "run.input_requested" {
	displayMode, _ := ev.DataJSON["display_mode"].(string)
	if strings.TrimSpace(displayMode) == "form" {
		requestID, _ := ev.DataJSON["request_id"].(string)
		prompt, _ := ev.DataJSON["message"].(string)
		schemaJSON, _ := json.Marshal(ev.DataJSON["requestedSchema"])
		if _, err := messagesRepo.FindAskUserFormMessage(ctx, w.run.AccountID, w.run.ThreadID, runID, requestID); err != nil {
			return err
		}
		if _, err := messagesRepo.CreateAskUserFormMessage(ctx, w.run.AccountID, w.run.ThreadID, runID, requestID, prompt, schemaJSON); err != nil {
			return err
		}
	}
}
```

```go
// src/services/worker/internal/app/composition_desktop.go
if ev.Type == "run.input_requested" {
	displayMode, _ := ev.DataJSON["display_mode"].(string)
	if strings.TrimSpace(displayMode) == "form" {
		requestID, _ := ev.DataJSON["request_id"].(string)
		prompt, _ := ev.DataJSON["message"].(string)
		schemaJSON, _ := json.Marshal(ev.DataJSON["requestedSchema"])
		if _, err := w.messagesRepo.CreateAskUserFormMessage(ctx, run.AccountID, run.ThreadID, runID, requestID, prompt, schemaJSON); err != nil {
			return err
		}
	}
}
```

- [ ] **Step 5: Re-run the pending-message test**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/app -run TestAskUserFormCreatesPendingThreadMessage
```

Expected:

```text
ok  	arkloop/services/worker/internal/app	...
```

- [ ] **Step 6: Commit the pending-message persistence change**

```bash
git add src/services/api/internal/data/messages_repo.go \
        src/services/worker/internal/pipeline/handler_agent_loop.go \
        src/services/worker/internal/app/composition_desktop.go \
        src/services/worker/internal/app/composition_desktop_test.go
git commit -m "feat: persist ask_user form requests in thread messages"
```

### Task 3: Update `/runs/{id}/input` to Finalize Form Messages

**Files:**
- Modify: `src/services/api/internal/data/messages_repo.go`
- Modify: `src/services/api/internal/http/conversationapi/v1_runs.go`
- Test: `src/services/api/internal/data/runs_repo_desktop_test.go`

- [ ] **Step 1: Write failing API tests for submit and dismiss transitions**

```go
func TestProvideInputMarksAskUserFormSubmitted(t *testing.T) {
	// Seed a pending ask_user_form message tied to run_id + request_id.
	// Call the submit input handler with {"region":"us-east-1"}.
	// Reload the message and assert status=submitted and answers preserved.
}

func TestProvideInputMarksAskUserFormDismissed(t *testing.T) {
	// Seed the same pending form.
	// Submit {} and assert status=dismissed with no editable pending state left.
}
```

- [ ] **Step 2: Run the API tests to verify the message remains pending**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/api && go test ./internal/data ./internal/http/conversationapi -run 'TestProvideInputMarksAskUserFormSubmitted|TestProvideInputMarksAskUserFormDismissed'
```

Expected:

```text
FAIL ... status = pending, want submitted
FAIL ... status = pending, want dismissed
```

- [ ] **Step 3: Add repository helpers for form status transitions**

```go
// src/services/api/internal/data/messages_repo.go
func (r *MessageRepository) UpdateAskUserFormMessageStatus(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	status string,
	answers json.RawMessage,
	submittedAt *time.Time,
) (Message, error) {
	var payload map[string]any
	if err := json.Unmarshal(existing.ContentJSON, &payload); err != nil {
		return Message{}, err
	}
	payload["status"] = status
	payload["answers"] = json.RawMessage(answers)
	payload["submitted_at"] = submittedAt
	nextContentJSON, _ := json.Marshal(payload)
	return r.UpdateStructuredContent(ctx, accountID, threadID, messageID, strings.TrimSpace(existing.Content), nextContentJSON)
}
```

- [ ] **Step 4: Update the submit-input handler to finalize the persisted card**

```go
// src/services/api/internal/http/conversationapi/v1_runs.go
if _, err := txRepo.ProvideInput(r.Context(), run.ID, body.Content, traceID); err != nil {
	// existing error handling
}

	messageRepoTx := messageRepo.WithTx(tx)
	pendingForm, err := messageRepoTx.FindLatestPendingAskUserFormByRun(r.Context(), run.AccountID, run.ThreadID, run.ID)
	if err != nil {
		return writeInternalError(w, traceID, err)
	}
	if pendingForm != nil {
		now := time.Now().UTC()
		status := "submitted"
		trimmed := strings.TrimSpace(body.Content)
		answers := json.RawMessage(trimmed)
		if trimmed == "" || trimmed == "{}" {
			status = "dismissed"
			answers = nil
		}
		if _, err := messageRepoTx.UpdateAskUserFormMessageStatus(r.Context(), run.AccountID, run.ThreadID, pendingForm.ID, status, answers, &now); err != nil {
			return writeInternalError(w, traceID, err)
		}
	}
```

- [ ] **Step 5: Re-run the API transition tests**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/api && go test ./internal/data ./internal/http/conversationapi -run 'TestProvideInputMarksAskUserFormSubmitted|TestProvideInputMarksAskUserFormDismissed'
```

Expected:

```text
ok  	arkloop/services/api/internal/data	...
ok  	arkloop/services/api/internal/http/conversationapi	...
```

- [ ] **Step 6: Commit the submit-state update**

```bash
git add src/services/api/internal/data/messages_repo.go \
        src/services/api/internal/http/conversationapi/v1_runs.go \
        src/services/api/internal/data/runs_repo_desktop_test.go \
        src/services/api/internal/http/conversationapi/v1_messages.go \
        src/services/api/internal/http/conversationapi/message_content.go
git commit -m "feat: finalize ask_user form messages on input"
```

### Task 4: Expire Pending Form Messages on Terminal Run End

**Files:**
- Modify: `src/services/api/internal/data/messages_repo.go`
- Modify: `src/services/worker/internal/pipeline/handler_agent_loop.go`
- Modify: `src/services/worker/internal/app/composition_desktop.go`
- Test: `src/services/worker/internal/app/composition_desktop_readonly_test.go`

- [ ] **Step 1: Write a failing terminal-state test for expiring leftover pending forms**

```go
func TestCompletedRunExpiresPendingAskUserForm(t *testing.T) {
	// Seed a pending ask_user_form message and then append a terminal event for the run.
	// Reload the message and assert status=expired.
}
```

- [ ] **Step 2: Run the terminal-state test to confirm pending cards are not finalized**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/app -run TestCompletedRunExpiresPendingAskUserForm
```

Expected:

```text
FAIL ... status = pending, want expired
```

- [ ] **Step 3: Add a repository helper to bulk-expire pending form messages for a run**

```go
// src/services/api/internal/data/messages_repo.go
func (r *MessageRepository) ExpirePendingAskUserFormsByRun(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
) error {
	msgs, err := r.ListPendingAskUserFormsByRun(ctx, accountID, threadID, runID)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		if _, err := r.UpdateAskUserFormMessageStatus(ctx, accountID, threadID, msg.ID, "expired", nil, nil); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Call the expiry helper when terminal events are appended**

```go
// src/services/worker/internal/pipeline/handler_agent_loop.go
if status, ok := TerminalStatuses[ev.Type]; ok {
	// existing status update logic...
	if err := messagesRepo.ExpirePendingAskUserFormsByRun(ctx, w.run.AccountID, w.run.ThreadID, runID); err != nil {
		return err
	}
}
```

```go
// src/services/worker/internal/app/composition_desktop.go
if status, ok := desktopTerminalStatuses[ev.Type]; ok {
	// existing desktop terminal logic...
	if err := w.messagesRepo.ExpirePendingAskUserFormsByRun(ctx, run.AccountID, run.ThreadID, runID); err != nil {
		return err
	}
}
```

- [ ] **Step 5: Re-run the terminal-state test**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/app -run TestCompletedRunExpiresPendingAskUserForm
```

Expected:

```text
ok  	arkloop/services/worker/internal/app	...
```

- [ ] **Step 6: Commit the expiry logic**

```bash
git add src/services/api/internal/data/messages_repo.go \
        src/services/worker/internal/pipeline/handler_agent_loop.go \
        src/services/worker/internal/app/composition_desktop.go \
        src/services/worker/internal/app/composition_desktop_readonly_test.go
git commit -m "feat: expire pending ask_user form messages on run end"
```

### Task 5: Widen Web Message Types for Structured Form Cards

**Files:**
- Modify: `src/apps/web/src/api.ts`
- Modify: `src/apps/web/src/agent-ui/contract.ts`
- Modify: `src/apps/web/src/agent-ui/arkloop-adapter.ts`
- Modify: `src/apps/web/src/messageContent.ts`
- Test: `src/apps/web/src/__tests__/chatPageLoading.test.tsx`

- [ ] **Step 1: Write failing Web tests for `ask_user_form` message decoding**

```ts
it('maps ask_user_form content_json into an agent message without dropping the custom payload', () => {
  const apiMessage = {
    id: 'm1',
    account_id: 'a1',
    thread_id: 't1',
    created_by_user_id: 'u1',
    role: 'assistant',
    content: 'Fill release checklist',
    content_json: {
      kind: 'ask_user_form',
      display_mode: 'form',
      request_id: 'call_1',
      run_id: 'run_1',
      message: 'Fill release checklist',
      schema: { properties: { version: { type: 'string' } } },
      status: 'pending',
      answers: null,
      submitted_at: null,
    },
    created_at: '2026-05-25T00:00:00Z',
  }

  const agentMessage = toAgentMessage(apiMessage as MessageResponse)
  expect(agentMessage.contentJson).toMatchObject({ kind: 'ask_user_form', status: 'pending' })
})
```

- [ ] **Step 2: Run the Web test to confirm `MessageContent` rejects the custom shape**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- src/__tests__/chatPageLoading.test.tsx
```

Expected:

```text
FAIL ... Property 'parts' does not exist
```

- [ ] **Step 3: Convert message content types from a single `parts` object to a tagged union**

```ts
// src/apps/web/src/api.ts
export type AskUserFormContent = {
  kind: 'ask_user_form'
  display_mode: 'form'
  request_id: string
  run_id: string
  tool_call_id?: string
  message: string
  schema: RequestedSchema
  status: 'pending' | 'submitted' | 'dismissed' | 'expired'
  answers: Record<string, unknown> | null
  submitted_at: string | null
}

export type MessageContent =
  | { parts: MessageContentPart[] }
  | AskUserFormContent
```

```ts
// src/apps/web/src/agent-ui/contract.ts
export type AgentAskUserFormContent = {
  kind: 'ask_user_form'
  displayMode: 'form'
  requestId: string
  runId: string
  toolCallId?: string
  message: string
  schema: RequestedSchema
  status: 'pending' | 'submitted' | 'dismissed' | 'expired'
  answers: Record<string, unknown> | null
  submittedAt: string | null
}

export type AgentMessageContent =
  | { parts: AgentMessageContentPart[] }
  | AgentAskUserFormContent
```

- [ ] **Step 4: Update adapters and helper functions to branch on `parts` vs `kind`**

```ts
// src/apps/web/src/agent-ui/arkloop-adapter.ts
function toAgentContent(content: MessageContent | undefined): AgentMessageContent | undefined {
  if (!content) return undefined
  if ('parts' in content) return { parts: content.parts.map(toAgentContentPart) }
  return {
    kind: 'ask_user_form',
    displayMode: 'form',
    requestId: content.request_id,
    runId: content.run_id,
    toolCallId: content.tool_call_id,
    message: content.message,
    schema: content.schema,
    status: content.status,
    answers: content.answers,
    submittedAt: content.submitted_at,
  }
}
```

```ts
// src/apps/web/src/messageContent.ts
export function messageTextContent(message: Pick<AgentMessage, 'content' | 'contentJson'>): string {
  if (message.contentJson && 'kind' in message.contentJson && message.contentJson.kind === 'ask_user_form') {
    return message.contentJson.message
  }
  if (message.contentJson && 'parts' in message.contentJson && message.contentJson.parts.length) {
    return message.contentJson.parts
      .filter((part): part is Extract<AgentMessageContentPart, { type: 'text' }> => part.type === 'text')
      .map((part) => part.text)
      .join('\n\n')
      .trim()
  }
  return extractLegacyFilesFromContent(message.content).text
}
```

- [ ] **Step 5: Re-run the Web type/adaptor test**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- src/__tests__/chatPageLoading.test.tsx
```

Expected:

```text
✓ ... ask_user_form content_json ...
```

- [ ] **Step 6: Commit the Web type-union change**

```bash
git add src/apps/web/src/api.ts \
        src/apps/web/src/agent-ui/contract.ts \
        src/apps/web/src/agent-ui/arkloop-adapter.ts \
        src/apps/web/src/messageContent.ts \
        src/apps/web/src/__tests__/chatPageLoading.test.tsx
git commit -m "refactor: support ask_user form message content"
```

### Task 6: Render Form Cards in the Chat Stream and Keep Inline Mode Unchanged

**Files:**
- Create: `src/apps/web/src/components/AskUserFormMessageCard.tsx`
- Modify: `src/apps/web/src/components/MessageList.tsx`
- Modify: `src/apps/web/src/components/ChatView.tsx`
- Modify: `src/apps/web/src/hooks/useThreadSseEffect.ts`
- Modify: `src/apps/web/src/hooks/useChatActions.ts`
- Test: `src/apps/web/src/__tests__/userInputCard.test.tsx`
- Test: `src/apps/web/src/__tests__/chatPageLoading.test.tsx`

- [ ] **Step 1: Write failing UI tests for pending, submitted, and inline-compat modes**

```ts
it('renders a pending ask_user_form card inside the message list', () => {
  render(<MessageList {...propsWithFormMessage} />)
  expect(screen.getByText('Fill release checklist')).toBeInTheDocument()
  expect(screen.getByRole('textbox', { name: /version/i })).toBeInTheDocument()
})

it('renders submitted ask_user_form cards as read-only full-field output', () => {
  render(<AskUserFormMessageCard message={submittedMessage} activeRunId={null} onSubmit={vi.fn()} />)
  expect(screen.getByText('version')).toBeInTheDocument()
  expect(screen.getByText('1.2.3')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /submit/i })).not.toBeInTheDocument()
})

it('keeps inline ask_user requests on the temporary composer card path', async () => {
  // Simulate run.input_requested with display_mode=inline.
  // Assert pendingUserInput is set and MessageList does not render an ask_user_form message.
})
```

- [ ] **Step 2: Run the Web UI tests to capture current failures**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- src/__tests__/userInputCard.test.tsx src/__tests__/chatPageLoading.test.tsx
```

Expected:

```text
FAIL ... Unable to find pending ask_user_form card
FAIL ... expected read-only submitted display
```

- [ ] **Step 3: Build the dedicated in-stream form card component**

```tsx
// src/apps/web/src/components/AskUserFormMessageCard.tsx
export function AskUserFormMessageCard({
  content,
  activeRunId,
  onSubmit,
}: {
  content: AgentAskUserFormContent
  activeRunId: string | null
  onSubmit: (requestId: string, answers: Record<string, FieldValue>) => Promise<void>
}) {
  const editable = content.status === 'pending' && activeRunId === content.runId

  if (!editable) {
    return (
      <div data-testid="ask-user-form-readonly">
        <h2>{content.message}</h2>
        {Object.entries(content.answers ?? {}).map(([key, value]) => (
          <div key={key}>
            <strong>{key}</strong>
            <span>{String(value)}</span>
          </div>
        ))}
      </div>
    )
  }

  return (
    <UserInputCard
      request={{
        request_id: content.requestId,
        message: content.message,
        requestedSchema: content.schema,
      }}
      onSubmit={(response) => onSubmit(content.requestId, response.answers)}
      onDismiss={() => onSubmit(content.requestId, {})}
    />
  )
}
```

- [ ] **Step 4: Switch MessageList and ChatView to route form-mode through the message stream**

```tsx
// src/apps/web/src/components/MessageList.tsx
if (
  msg.role === 'assistant' &&
  msg.contentJson &&
  'kind' in msg.contentJson &&
  msg.contentJson.kind === 'ask_user_form'
) {
  return (
    <AskUserFormMessageCard
      key={msg.id}
      content={msg.contentJson}
      activeRunId={run.activeRunId}
      onSubmit={handleAskUserFormSubmit}
    />
  )
}
```

```ts
// src/apps/web/src/hooks/useThreadSseEffect.ts
if (event.type === 'input-request') {
  const data = agentEventDataRecord(event.data)
  const displayMode = typeof data?.display_mode === 'string' ? data.display_mode : 'inline'
  if (displayMode === 'form') {
    setAwaitingInput(true)
    continue
  }
  // existing inline pendingUserInput behavior remains here
}
```

```ts
// src/apps/web/src/hooks/useChatActions.ts
const handleAskUserFormSubmit = useCallback(async (requestId: string, answers: Record<string, FieldValue>) => {
  if (!activeRunId) return
  await agentClient.provideInput(activeRunId, JSON.stringify(answers))
  setPendingUserInput(null)
}, [agentClient, activeRunId, setPendingUserInput])
```

- [ ] **Step 5: Re-run the Web UI tests and a type-check**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- src/__tests__/userInputCard.test.tsx src/__tests__/chatPageLoading.test.tsx
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm type-check
```

Expected:

```text
✓ ... pending ask_user_form card inside the message list
✓ ... submitted ask_user_form cards as read-only full-field output
✓ ... keeps inline ask_user requests on the temporary composer card path
```

```text
Done in ...
```

- [ ] **Step 6: Commit the renderer and regression coverage**

```bash
git add src/apps/web/src/components/AskUserFormMessageCard.tsx \
        src/apps/web/src/components/MessageList.tsx \
        src/apps/web/src/components/ChatView.tsx \
        src/apps/web/src/hooks/useThreadSseEffect.ts \
        src/apps/web/src/hooks/useChatActions.ts \
        src/apps/web/src/__tests__/userInputCard.test.tsx \
        src/apps/web/src/__tests__/chatPageLoading.test.tsx
git commit -m "feat: render ask_user forms in chat history"
```

### Task 7: Run End-to-End Regression Checks

**Files:**
- Modify: none
- Test: existing touched test files across worker, api, and web

- [ ] **Step 1: Run the consolidated Go regression suite**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/services/worker && go test ./internal/tools/builtin/askuser ./internal/agent ./internal/executor ./internal/app ./internal/pipeline
cd /Users/huhui/Projects/Arkloop/src/services/api && go test ./internal/data ./internal/http/conversationapi
```

Expected:

```text
ok  	arkloop/services/worker/internal/tools/builtin/askuser	...
ok  	arkloop/services/worker/internal/agent	...
ok  	arkloop/services/worker/internal/executor	...
ok  	arkloop/services/worker/internal/app	...
ok  	arkloop/services/worker/internal/pipeline	...
ok  	arkloop/services/api/internal/data	...
ok  	arkloop/services/api/internal/http/conversationapi	...
```

- [ ] **Step 2: Run the consolidated Web regression suite**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- src/__tests__/userInputCard.test.tsx src/__tests__/chatPageLoading.test.tsx
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm type-check
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm lint
```

Expected:

```text
✓ all selected vitest cases passed
```

```text
Found 0 errors.
```

```text
Done with no ESLint errors.
```

- [ ] **Step 3: Manual verification in the browser**

Run:

```bash
cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm dev
```

Verify:

```text
1. A normal inline ask_user request still appears above the composer and disappears after submit.
2. A form-mode ask_user request appears as a chat message card.
3. Submitting the form keeps the card in place and turns it read-only.
4. Refreshing the page still shows the submitted card with all field values.
5. A timed-out form request reappears as a non-editable expired card.
```

- [ ] **Step 4: Commit any final fixes from the regression pass**

```bash
git add src/services/worker/internal/tools/builtin/askuser/spec.go \
        src/services/worker/internal/tools/builtin/askuser/executor.go \
        src/services/worker/internal/agent/loop.go \
        src/services/worker/internal/pipeline/handler_agent_loop.go \
        src/services/worker/internal/app/composition_desktop.go \
        src/services/api/internal/data/messages_repo.go \
        src/services/api/internal/http/conversationapi/v1_runs.go \
        src/apps/web/src/api.ts \
        src/apps/web/src/agent-ui/contract.ts \
        src/apps/web/src/agent-ui/arkloop-adapter.ts \
        src/apps/web/src/messageContent.ts \
        src/apps/web/src/components/AskUserFormMessageCard.tsx \
        src/apps/web/src/components/MessageList.tsx \
        src/apps/web/src/components/ChatView.tsx \
        src/apps/web/src/hooks/useThreadSseEffect.ts \
        src/apps/web/src/hooks/useChatActions.ts \
        src/apps/web/src/__tests__/userInputCard.test.tsx \
        src/apps/web/src/__tests__/chatPageLoading.test.tsx
git commit -m "test: close ask_user form in chat regressions"
```

## Self-Review Notes

- Spec coverage: the plan covers explicit `display_mode`, pending-message persistence, submit/dismiss/expired transitions, message-stream rendering, refresh durability, and inline compatibility.
- Placeholder scan: no `TBD`, `TODO`, or deferred implementation notes remain.
- Type consistency: the plan consistently uses `display_mode=form`, `kind=ask_user_form`, statuses `pending/submitted/dismissed/expired`, and the existing `/v1/runs/{id}/input` submission path.
