# Ask User Form-In-Chat Design

## Goal

Add a new `ask_user` presentation mode that renders a full form card directly inside the chat message stream for larger, structured questionnaires. The form must remain in the thread after submission as a read-only record, survive page refresh, and coexist with the current inline `ask_user` flow for simpler prompts.

## Scope

This design covers:

- Extending `ask_user` to support an explicit `display_mode`
- Persisting form-mode `ask_user` interactions as thread messages
- Keeping the current run input submission pipeline unchanged
- Rendering pending and submitted form cards inside the Web chat stream
- Preserving existing inline `ask_user` behavior for non-form usage

This design does not cover:

- Auto-switching modes based on field count
- New agent-side planning heuristics for when to use form mode
- Editable history after a form has been submitted
- A new generalized workflow engine for multi-step surveys

## Product Decisions

- Mode selection is explicit. Agents choose `display_mode=form` or keep the existing inline behavior.
- Form cards are shown inside the chat message stream.
- After submission, the same form card remains in place and becomes read-only.
- Refreshing or reopening the thread must still show the submitted form card.
- Submitted form cards show the full field-by-field response, not a compact summary.
- Inline `ask_user` remains the default and continues to use the existing bottom-area interaction for simpler prompts.

## Current State

Today `ask_user` works as a live run interaction only:

- Worker emits `run.input_requested`
- Web stores a temporary `pendingUserInput`
- The UI renders `UserInputCard` above the composer
- Submission goes to `POST /v1/runs/{run_id}/input`
- API stores `run.input_provided`
- Worker resumes and forwards the answer into the next LLM turn

This flow is useful for live interaction, but it is not sufficient for persistent chat history because:

- The card is not represented as a thread message
- Refresh depends on transient run state rather than message history
- There is no persistent read-only artifact of the structured response in the thread

## High-Level Approach

Use a mixed architecture:

- `run_events` remain the control plane for pause, resume, timeout, and answer delivery to the worker
- `messages` become the persistence plane for form-mode chat rendering and replay

For `display_mode=form`, the system will:

1. Keep the existing `run.input_requested -> /runs/{id}/input -> run.input_provided` pipeline
2. Create a thread message that represents the form card
3. Update that thread message in place when the form is submitted, dismissed, or expires

For non-form `ask_user`, the system will keep the existing transient inline card behavior unchanged.

## Architecture

### Responsibility Split

#### Run layer

The run layer remains responsible for:

- Pausing the run while input is required
- Emitting `run.input_requested`
- Accepting serialized user input through `/v1/runs/{id}/input`
- Resuming the worker after input is received
- Preserving prompt scan and timeout semantics

#### Message layer

The message layer becomes responsible for:

- Showing a form card in the chat stream
- Preserving that card across refresh and thread reload
- Recording the final read-only response shape shown to the user

### Why this split

This keeps the existing worker protocol stable while giving the chat UI a durable, replayable source of truth. It avoids rebuilding history from run events and avoids overloading thread messages with execution control behavior.

## Ask User Contract Changes

### New argument

Extend `ask_user` to accept an optional `display_mode`.

Supported values:

- `inline`
- `form`

Behavior:

- Missing `display_mode` is treated as `inline`
- `inline` preserves current behavior
- `form` enables persistent form-in-chat rendering

### Validation

`ValidateAndNormalize` should:

- Accept `display_mode` only when it is `inline` or `form`
- Include the normalized value in the returned schema payload or a companion metadata payload
- Continue validating fields exactly as today

The normalized result emitted to the client for form mode must include enough data to:

- Render the form
- Associate it with a run and request id
- Persist it as a structured message

## Persistent Message Model

### New structured message content kind

Add a new thread message content kind for form-mode `ask_user` cards.

Recommended shape:

```json
{
  "kind": "ask_user_form",
  "display_mode": "form",
  "request_id": "call_123",
  "run_id": "run_uuid",
  "tool_call_id": "call_123",
  "message": "Please fill out the deployment checklist",
  "schema": {
    "properties": {},
    "required": [],
    "_fieldOrder": []
  },
  "status": "pending",
  "answers": null,
  "submitted_at": null
}
```

### Status lifecycle

Allowed states:

- `pending`
- `submitted`
- `dismissed`
- `expired`

Rules:

- `pending` is editable only when it belongs to the active waiting run
- `submitted` is always read-only
- `dismissed` is read-only and indicates the user intentionally skipped the form
- `expired` is read-only and indicates the run stopped waiting before submission

### Storage choice

Use existing thread message persistence and store this as structured `content_json` rather than inventing a second durable store. This makes form cards compatible with:

- Thread reload
- Existing message list APIs
- Chat rendering
- Potential future share/export behavior

## Backend Flow

### 1. Worker emits form request

When `ask_user` runs with `display_mode=form`:

1. Worker validates and normalizes input
2. Worker emits `run.input_requested` as it does today
3. The system creates a new thread message with `kind=ask_user_form` and `status=pending`

The message must be linked to:

- `thread_id`
- `run_id`
- `request_id`

There must be at most one persisted form card per `(run_id, request_id)` pair.

### 2. Frontend shows pending form card

The Web app receives either:

- The new thread message via thread message loading, or
- A thread-local message update event if added later

The card is rendered in the normal chat stream, not above the composer.

### 3. User submits the form

Submission remains unchanged at the run protocol level:

- Frontend serializes `answers` as JSON
- Frontend calls `POST /v1/runs/{run_id}/input`

After the API writes `run.input_provided`, it also updates the matching `ask_user_form` message:

- `status: submitted`
- `answers: { ... }`
- `submitted_at: now`

The worker then resumes as it does today.

### 4. User dismisses the form

If the user dismisses the form:

- Frontend submits the agreed dismissal payload through `/v1/runs/{run_id}/input`
- API updates the persisted message to `dismissed`

The card remains in chat as a read-only dismissed artifact.

### 5. Run times out or ends while still pending

If the run stops waiting without a submission:

- Pending form messages for that `(run_id, request_id)` become `expired`

This transition should happen from a reliable backend path so the frontend does not need to infer expiry solely from missing active run state.

## API Behavior

### Keep the current submission endpoint

Do not introduce a new form submission endpoint. Reuse:

- `POST /v1/runs/{run_id}/input`

Reason:

- The worker already understands this path
- Prompt scan and timeout semantics already exist
- This avoids diverging run input semantics between inline and form modes

### Needed API-side enhancement

`ProvideInput` handling must gain the ability to:

- Detect whether the active waiting request corresponds to a persisted form message
- Update that message atomically or near-atomically after writing `run.input_provided`

The API update path must be idempotent enough to safely tolerate retries.

## Frontend Rendering

### Mode split

#### Inline mode

Keep current behavior:

- `run.input_requested` sets `pendingUserInput`
- `UserInputCard` appears above the composer
- Submit or dismiss clears the temporary pending state

#### Form mode

New behavior:

- Render from thread messages, not temporary composer-adjacent state
- The card lives in the chat stream
- After submission, the card stays in place and becomes read-only

### New message component

Add a dedicated message renderer, for example:

- `AskUserFormMessageCard`

Responsibilities:

- Render editable form fields for `pending`
- Render full read-only field/value pairs for `submitted`
- Render state treatment for `dismissed` and `expired`

### Editable rules

A pending form card is editable only when all of the following are true:

- The card status is `pending`
- Its `run_id` matches the active waiting run
- The current run is actually waiting for input

Otherwise the card is rendered read-only to avoid submitting data to the wrong run.

### Submission UX

On submit:

- Call the existing `provideInput`
- Do not remove the card
- Wait for the persisted message state to become `submitted`
- Re-render the same message as read-only

This avoids local-only optimistic state that can drift from server truth.

### Full submitted display

For `submitted`, show:

- The original form prompt
- Every field in stable order
- Each selected or entered value

No compact summary is needed for the first version.

## Refresh and Replay

### Desired behavior

After refresh or reopening the thread:

- Submitted form cards appear from thread history
- Their full field values remain visible

### Pending historical forms

If a `pending` card exists in message history but there is no active compatible waiting run:

- Render the card as non-editable
- Show clear state language that it is no longer awaiting input

The backend should prefer converting truly abandoned pending cards to `expired`, but the frontend must still guard against stale editability.

## Event Handling Strategy

### `run.input_requested`

This event remains necessary, but its frontend meaning changes for form mode.

For `inline`:

- Continue creating temporary `pendingUserInput`

For `form`:

- Do not open the temporary bottom card UI
- Use the event only to know the run is waiting
- Let thread message rendering drive the visible card

### Why not reconstruct from SSE alone

SSE is not a sufficient durable source for this feature because:

- Reconnect behavior is not the same as loading thread history
- Refresh should not require replaying the live run stream
- Thread messages are the correct replay surface for chat artifacts

## Data Consistency Requirements

### Correlation

Every form-mode request must be correlatable by:

- `run_id`
- `request_id`

Optional but useful:

- `tool_call_id`
- `message_id`

### Invariants

- A form-mode `ask_user` request creates exactly one persisted form message
- A submitted or dismissed form message is never editable again
- The final displayed answers must match the payload sent to `/runs/{id}/input`
- Inline-mode requests must not create persistent form messages

## Backward Compatibility

This design is backward-compatible because:

- Existing `ask_user` calls without `display_mode` remain inline
- Existing run input submission API remains unchanged
- Existing worker wait/resume logic remains unchanged
- Existing `UserInputCard` stays in place for simple flows

No migration of old `run.input_requested` events is required. This is a forward-only feature addition.

## Error Handling

### Submission failure

If `/runs/{id}/input` fails:

- Keep the form card visible and editable
- Show inline error feedback
- Do not locally mark the card as submitted

### Duplicate submission

If the user retries after a network error:

- API-side message update must be safe to repeat
- The final persisted message should still end in a single `submitted` state

### Stale active run

If the run is no longer active:

- Submission should fail with the existing run-not-active behavior
- Frontend should stop treating the card as editable after refresh or fresh run state is known

## Testing Strategy

### Go tests

Add coverage for:

- `ask_user` with `display_mode=form` validating successfully
- Form-mode request creating one persistent form message
- `/runs/{id}/input` transitioning the message from `pending` to `submitted`
- Dismiss transition to `dismissed`
- Timeout or terminal run transition to `expired`
- Inline mode preserving existing behavior and not creating persistent form messages

### Web tests

Add coverage for:

- Rendering a pending form card inside the chat stream
- Submitting and re-rendering the same card as read-only
- Refresh/reload showing submitted cards from message history
- Inline mode continuing to render via the existing temporary card
- Stale pending cards being non-editable when no active waiting run exists

## File Impact

Expected main touchpoints:

### Backend

- `src/services/worker/internal/tools/builtin/askuser/`
- `src/services/worker/internal/agent/loop.go`
- `src/services/api/internal/http/conversationapi/v1_runs.go`
- `src/services/api/internal/data/messages_repo.go`
- Message content normalization and response serialization paths

### Frontend

- `src/apps/web/src/components/ChatView.tsx`
- `src/apps/web/src/components/UserInputCard.tsx`
- New `src/apps/web/src/components/AskUserFormMessageCard.tsx`
- `src/apps/web/src/hooks/useThreadSseEffect.ts`
- `src/apps/web/src/hooks/useChatActions.ts`
- `src/apps/web/src/agent-ui/event-data.ts`
- Message content rendering and type definitions

## Open Decisions Resolved

- Mode selection: explicit by agent, no auto-threshold logic
- Form persistence: yes, via thread messages
- Post-submit behavior: remain in place and become read-only
- Submitted display: full form replay, not compact summary
- Delivery path: reuse existing run input endpoint

## Recommended Implementation Order

1. Extend `ask_user` contract to accept `display_mode`
2. Add persistent message schema for `ask_user_form`
3. Create backend correlation and state transition logic
4. Add frontend message renderer for form cards
5. Rewire SSE handling so form mode does not use temporary composer-adjacent UI
6. Add tests for both the new form path and the unchanged inline path

## Risks

### Message/run coordination risk

There is new coordination between execution state and message state. If correlation is weak, the wrong card could be updated. This is why `(run_id, request_id)` must be treated as the primary identity.

### Refresh-state mismatch risk

If the backend does not reliably close out abandoned pending forms, refresh may show stale pending cards. Frontend guards reduce the impact, but backend expiry transitions are still important.

### Renderer drift risk

If form cards and inline cards diverge too much, future maintenance will become awkward. Shared field rendering helpers should be reused where practical even if the message container is different.

## Recommendation

Proceed with the mixed architecture. It preserves the stable run control path while introducing a durable, replayable chat artifact for form-mode `ask_user`. This is the cleanest way to satisfy in-chat form UX, read-only post-submit history, and refresh-safe thread replay without regressing the current inline experience.
