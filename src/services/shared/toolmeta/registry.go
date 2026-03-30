package toolmeta

import "fmt"

const (
	GroupWebSearch          = "web_search"
	GroupWebFetch           = "web_fetch"
	GroupImageUnderstanding = "image_understanding"
	GroupSandbox            = "sandbox"
	GroupMemory             = "memory"
	GroupDocument           = "document"
	GroupOrchestration      = "orchestration"
	GroupDiscovery          = "discovery"
	GroupFilesystem         = "filesystem"

	WebSearchDefaultMaxResults = 5
	WebSearchMaxResultsLimit   = 20
	WebSearchMaxQueriesLimit   = 5
	WebFetchMaxLengthLimit     = 200000
)

type ToolMeta struct {
	Name           string
	Group          string
	Label          string
	ShortDesc      string // ~20 tokens, always injected into context for search_tools catalog
	LLMDescription string // full description, loaded on demand via search_tools
}

type ToolGroup struct {
	Name  string
	Tools []ToolMeta
}

var groupOrder = []string{
	GroupDiscovery,
	GroupWebSearch,
	GroupWebFetch,
	GroupImageUnderstanding,
	GroupSandbox,
	GroupFilesystem,
	GroupMemory,
	GroupDocument,
	GroupOrchestration,
}

var registry = []ToolMeta{
	// ── discovery ──
	{
		Name:      "search_tools",
		Group:     GroupDiscovery,
		Label:     "Search tools",
		ShortDesc: "look up tools in this runtime catalog by tool name or catalog keyword (not web search)",
		LLMDescription: "search this platform's tool registry only: match by exact or partial tool name, or by words that appear in each tool's short catalog description. " +
			"Not for the public web, not for project names, papers, news, or general questions — use web_search (or answer from training) for those. " +
			"Do not pass full natural-language research prompts as queries; they will not match any tool. " +
			"Use when you need a callable tool that is not in your current tool set. " +
			"Pass multiple catalog lookups in one call to batch-load several tools — never call this tool twice in a row for related tools. " +
			"Think ahead: if you will need a group of tools together (e.g. spawn_agent + wait_agent, read + edit), load all of them in a single call. " +
			"After this call succeeds, matched tools may be injected into the real tool list in later phases of the same reasoning loop. " +
			"Only call them after they actually appear there.",
	},
	// ── web ──
	{
		Name:      "web_search",
		Group:     GroupWebSearch,
		Label:     "Web search",
		ShortDesc: "search the web and return results",
		LLMDescription: fmt.Sprintf(
			"search the web and return title, URL, and snippet for each result. "+
				"Use the queries array (up to %d) to run independent searches in one call; use the scalar query field for a single question. "+
				"max_results per query defaults to %d (max %d).",
			WebSearchMaxQueriesLimit, WebSearchDefaultMaxResults, WebSearchMaxResultsLimit),
	},
	{
		Name:           "web_fetch",
		Group:          GroupWebFetch,
		Label:          "Web fetch",
		ShortDesc:      "fetch a web page and return its content as text",
		LLMDescription: "fetch a web page and return its title and body as plain text. Use when search snippets are insufficient and a specific page likely contains deeper information. Prefer official or authoritative sources. Batch-callable; do not re-fetch the same URL.",
	},
	// ── sandbox ──
	{
		Name:      "python_execute",
		Group:     GroupSandbox,
		Label:     "Python execution",
		ShortDesc: "execute Python code in an isolated sandbox",
		LLMDescription: "execute Python code in an isolated sandbox. Use for calculations, data processing, or visualization instead of computing manually. " +
			"Pre-installed: numpy, pandas, matplotlib, plotly, scipy, sympy, pillow, scikit-learn, kaleido. " +
			"For charts prefer Plotly; use fig.write_image() for PNG, fall back to fig.write_html() only on failure. Do not set pio.renderers. " +
			"Working files go to /workspace/; final user-visible files go to /tmp/output/ (auto-uploaded as artifacts). " +
			"Two distinct reference formats — use the correct one:\n" +
			"  • /tmp/output/ files appear in result.artifacts → reference as artifact:<key>  (e.g. ![alt](artifact:abc/run/file.png))\n" +
			"  • /workspace/ files do NOT appear in result.artifacts → reference as workspace:/relative/path  (strip /workspace prefix, e.g. ![alt](workspace:/data/file.png))\n" +
			"WRONG: ![alt](workspace:/tmp/output/file.png)  — workspace: is only for /workspace/, never for /tmp/output/\n" +
			"Only reference artifact keys that actually appear in result.artifacts. " +
			"Never output raw /workspace/ or /tmp/output/ paths. Never invent artifact keys.",
	},
	{
		Name:      "exec_command",
		Group:     GroupSandbox,
		Label:     "Command execution",
		ShortDesc: "run a shell command in a persistent sandbox session",
		LLMDescription: "run a shell command in a persistent sandbox session. Use session_mode=auto by default. " +
			"Reuse the session_ref returned by the first call; do not issue a new exec_command to poll a busy session — use write_stdin instead. " +
			"The shell keeps its state across calls. When you only need to change directories, prefer the cwd parameter instead of prefixing the command with cd &&. " +
			"If the result shows running=true or only control sequences, continue with write_stdin. " +
			"Do not use for file operations — use read/write_file/edit/grep instead. " +
			"Working files go to /workspace/; final user-visible files go to /tmp/output/ (auto-uploaded as artifacts). " +
			"Two distinct reference formats — use the correct one:\n" +
			"  • /tmp/output/ files appear in result.artifacts → reference as artifact:<key>\n" +
			"  • /workspace/ files do NOT appear in result.artifacts → reference as workspace:/relative/path  (strip /workspace prefix)\n" +
			"WRONG: [name](workspace:/tmp/output/file.txt)  — workspace: is only for /workspace/, never for /tmp/output/\n" +
			"Only reference artifact keys that actually appear in result.artifacts. " +
			"Never output raw paths. Never invent artifact keys.",
	},
	{
		Name:      "write_stdin",
		Group:     GroupSandbox,
		Label:     "Shell stdin",
		ShortDesc: "send stdin or poll output from a running shell session",
		LLMDescription: "send stdin to, or poll output from, a running shell session. " +
			"Pass the session_ref from exec_command. Use only when exec_command returned running=true or the process awaits stdin. " +
			"Set chars to a non-empty string to write, or omit/empty to poll new output. " +
			"Working files go to /workspace/; final files go to /tmp/output/. " +
			"Show /workspace/ files via Markdown: images ![alt](workspace:/relative/path), others [name](workspace:/relative/path). " +
			"Never invent artifact keys.",
	},
	{
		Name:      "browser",
		Group:     GroupSandbox,
		Label:     "Browser automation",
		ShortDesc: "run browser automation commands in the sandbox",
		LLMDescription: "run browser automation commands in the sandbox. Use only when web_search/web_fetch cannot complete the task (JS rendering, DOM interaction, login flows, multi-tab navigation). " +
			"Pass the raw subcommand: navigate <url>, snapshot, screenshot, click <ref>, type <ref> <text>, fill <ref> <text>, press <key>, tab list, tab select <index>, console, network. " +
			"Session reuse, waiting, retry, and recovery are handled by the backend; do not pass session_mode/share_scope. " +
			"Workflow: navigate -> snapshot (get refs) -> interact -> snapshot again after navigation or UI changes. " +
			"Snapshot results are compact by default: URL, title, clickable refs, form controls, and visible-text summary. Use screenshot only when you need a visual image. " +
			"Set yield_time_ms high enough for pages to settle; avoid tiny values such as 50ms, prefer 1500-5000ms. " +
			"Only reference artifact keys that actually appear in result.artifacts; never invent artifact keys.",
	},
	// ── filesystem ──
	{
		Name:      "read",
		Group:     GroupFilesystem,
		Label:     "Read",
		ShortDesc: "read files or image sources and return textual output",
		LLMDescription: "read content from source.kind=file_path, message_attachment, or remote_url. " +
			"For file_path: return file content with line numbers using offset and limit. Default limit is 2000 lines; files larger than 256 KB are rejected. " +
			"For message_attachment and remote_url: read image bytes and return textual understanding from prompt. " +
			"Use prompt only for image sources. Always read a file before editing it.",
	},
	{
		Name:      "write_file",
		Group:     GroupFilesystem,
		Label:     "Write file",
		ShortDesc: "create or overwrite a file",
		LLMDescription: "create a new file or overwrite an existing file with the provided content. " +
			"Parent directories are created automatically. " +
			"Prefer edit over write_file when making targeted changes to existing files.",
	},
	{
		Name:      "edit",
		Group:     GroupFilesystem,
		Label:     "Edit file",
		ShortDesc: "replace a unique string in a file (str_replace semantics)",
		LLMDescription: "replace one occurrence of old_string with new_string in the specified file. " +
			"old_string must match exactly once — include enough surrounding context (3-5 lines before and after) to ensure uniqueness. " +
			"To create a new file: set old_string to empty. To delete content: set new_string to empty. " +
			"You must call read with source.kind=file_path before editing an existing file (old_string non-empty); omitting it will return an error.",
	},
	{
		Name:      "glob",
		Group:     GroupFilesystem,
		Label:     "Glob files",
		ShortDesc: "find files by glob pattern",
		LLMDescription: "find files matching a glob pattern and return their paths. " +
			"Uses ripgrep when available for speed; falls back to Go filepath walk. " +
			"Results are sorted by path length (shortest first). Maximum 1000 results. " +
			"Patterns like **/*.go, src/**/*.ts, *.md are supported.",
	},
	{
		Name:      "grep",
		Group:     GroupFilesystem,
		Label:     "Grep files",
		ShortDesc: "search file contents by regex pattern",
		LLMDescription: "search file contents for a regex pattern and return matching lines with file:line:content format. " +
			"Uses ripgrep when available; falls back to Go regex walk. " +
			"Use include to restrict to specific file types (e.g. *.go). Maximum 200 matches. " +
			"Results are sorted by file modification time (newest first) in fallback mode. " +
			"Use context_lines (0-10) to include surrounding lines with each match.",
	},
	// ── memory ──
	{
		Name:      "memory_search",
		Group:     GroupMemory,
		Label:     "Memory search",
		ShortDesc: "search long-term memory for user preferences and context",
		LLMDescription: "search long-term memory for user preferences, past experiences, constraints, or prior interactions. " +
			"Use for recommendations, comparisons, preference-driven questions, or open-ended problems where user context improves quality. " +
			"Call at most once per query. Results may inform subsequent tool choices but rarely suffice alone. " +
			"Each hit includes uri: pass that exact string to memory_read or memory_forget. On Arkloop Desktop local memory, uri looks like local://memory/<id>; do not invent category:key or scope:key strings. " +
			"If scope is omitted, both user- and agent-scoped entries are searched (Desktop SQLite). " +
			"Internal fields (uri, _ref) are system identifiers — never expose raw uri text to the user unless they explicitly need to copy it.",
	},
	{
		Name:      "memory_read",
		Group:     GroupMemory,
		Label:     "Memory read",
		ShortDesc: "read the full content of a memory entry by URI",
		LLMDescription: "read the full content of a memory entry by URI copied from a memory_search hit or from memory_write.uri on Desktop. " +
			"Local Desktop SQLite only accepts local://memory/<uuid>. viking:// URIs are for OpenViking. Never guess uri from category/key alone.",
	},
	{
		Name:      "memory_write",
		Group:     GroupMemory,
		Label:     "Memory write",
		ShortDesc: "store knowledge in long-term memory",
		LLMDescription: "store knowledge in long-term memory for future reference. " +
			"After success on Desktop local memory, the tool result includes uri — use that for memory_read. Then memory_search can find the entry by query keywords.",
	},
	{
		Name:           "memory_forget",
		Group:          GroupMemory,
		Label:          "Memory forget",
		ShortDesc:      "remove a specific memory entry by URI",
		LLMDescription: "remove a specific memory entry by URI from memory_search or memory_write (same rules as memory_read).",
	},
	{
		Name:      "conversation_search",
		Group:     GroupMemory,
		Label:     "Conversation search",
		ShortDesc: "keyword-search visible conversation history",
		LLMDescription: "keyword-search the current user's visible conversation history across all threads. " +
			"Use to recall previously discussed facts not stored in long-term memory. Returns matching messages with thread_id, role, snippet, and timestamp. " +
			"This is keyword search, not semantic search, and costs no model tokens.",
	},
	// ── artifact ──
	{
		Name:      "visualize_read_me",
		Group:     GroupDocument,
		Label:     "Read guidelines",
		ShortDesc: "load the canonical generative UI design system modules",
		LLMDescription: "Returns design guidelines for show_widget and HTML/SVG visual generation. " +
			"Call once before your first show_widget call. Do NOT mention this call to the user. " +
			"Pick the modules that match your use case: interactive, chart, mockup, art, diagram. " +
			"This tool returns the full canonical guideline text and must not be summarized.",
	},
	{
		Name:      "show_widget",
		Group:     GroupDocument,
		Label:     "Show widget",
		ShortDesc: "render an interactive HTML widget inline in the conversation",
		LLMDescription: "render an interactive HTML/SVG widget directly in the chat. " +
			"Use for charts, diagrams, dashboards, calculators, interactive explainers, UI mockups, and visual interactive content. " +
			"Always call visualize_read_me first to load the full design guidelines, then set i_have_seen_read_me=true. " +
			"widget_code is a raw HTML fragment (no DOCTYPE/html/head/body tags). " +
			"Structure: <style> first, HTML elements next, <script> last. " +
			"CSS variables (--c-bg-page, --c-text-primary, --c-border etc.) are automatically available. " +
			"The host runtime provides preloaded SVG helper classes and host skin tokens; keep the outer shell transparent and host-native. " +
			"To send a follow-up message from a widget: call sendPrompt(text). " +
			"Optionally set loading_messages to 1-4 short lines shown while widget_code streams. " +
			"NEVER use python_execute + exec_command open for HTML visualizations.",
	},
	{
		Name:      "artifact_guidelines",
		Group:     GroupDocument,
		Label:     "Artifact guidelines",
		ShortDesc: "load design guidelines for artifact creation",
		LLMDescription: "Compatibility alias of visualize_read_me. " +
			"Loads the same full canonical generative UI design guidelines with the same module set. " +
			"Call silently before visual generation when legacy prompts still reference artifact_guidelines.",
	},
	{
		Name:      "create_artifact",
		Group:     GroupDocument,
		Label:     "Create artifact",
		ShortDesc: "create an interactive or static artifact (HTML, SVG, Markdown)",
		LLMDescription: "create an artifact and save it for display. Supports HTML (interactive widgets, charts, diagrams), SVG (illustrations, diagrams), and Markdown (documents, reports). " +
			"Set display to \"inline\" (default) for visual content embedded in the conversation, or \"panel\" for documents opened in the side panel. " +
			"For HTML artifacts: put <style> first, HTML content next, <script> last (streaming-friendly order). Use CSS variables (--c-bg-page, --c-text-primary, etc.) for theme compatibility. " +
			"Load external libraries from CDN only (cdnjs.cloudflare.com, cdn.jsdelivr.net, unpkg.com, esm.sh). " +
			"Before your first create_artifact call, call artifact_guidelines to load design rules for the content type you are generating. " +
			"Reference the result as [label](artifact:<key>). " +
			"IMPORTANT: the content parameter MUST be the last parameter you generate.",
	},
	{
		Name:      "document_write",
		Group:     GroupDocument,
		Label:     "Document write",
		ShortDesc: "write a Markdown document as a downloadable artifact",
		LLMDescription: "write a Markdown document and save it as a downloadable artifact. " +
			"Use when the user requests a report, summary, plan, article, or any long-form document. " +
			"Reference the result as [label](artifact:<key>).",
	},
	// ── orchestration ──
	{
		Name:      "acp_agent",
		Group:     GroupOrchestration,
		Label:     "ACP agent",
		ShortDesc: "delegate a task to an external ACP coding agent",
		LLMDescription: "[Deprecated: use spawn_acp + wait_acp instead. acp_agent blocks synchronously and cannot be interrupted.] " +
			"delegate a task to an external ACP-compatible coding agent running inside the sandbox (e.g. opencode). " +
			"The agent operates autonomously with its own LLM, tools, and workspace. " +
			"Use for code-heavy tasks: implementation, debugging, refactoring, test execution. " +
			"This tool connects to an external agent process in the sandbox — it does NOT create an Arkloop sub-agent.",
	},
	{
		Name:      "spawn_acp",
		Group:     GroupOrchestration,
		Label:     "Spawn ACP agent",
		ShortDesc: "start an ACP coding agent asynchronously and return a handle",
		LLMDescription: "start an ACP-compatible coding agent asynchronously. " +
			"Returns a handle_id immediately without blocking. " +
			"Use wait_acp to retrieve the result when ready. " +
			"After wait_acp returns completed, the agent session remains alive — use send_acp to continue the conversation. " +
			"spawn_acp and wait_acp are always used together — load both in one search_tools call. " +
			"To run multiple ACP tasks in parallel: call spawn_acp N times, then wait_acp for each. " +
			"Use interrupt_acp to cancel the current turn without closing the session. " +
			"Use close_acp to terminate the process when no further interaction is needed.",
	},
	{
		Name:      "send_acp",
		Group:     GroupOrchestration,
		Label:     "Send ACP input",
		ShortDesc: "send a follow-up prompt to a running ACP agent session",
		LLMDescription: "send a new prompt to an existing ACP agent session. " +
			"The session must be in idle state (previous turn completed). " +
			"Use wait_acp after send_acp to get the result. " +
			"The ACP agent retains its full conversation context across turns.",
	},
	{
		Name:      "wait_acp",
		Group:     GroupOrchestration,
		Label:     "Wait ACP agent",
		ShortDesc: "block until a spawned ACP agent completes and return its output",
		LLMDescription: "block until a spawned ACP agent's current turn reaches a terminal state (completed, failed, or interrupted). " +
			"Returns the agent output on success. " +
			"Set timeout_seconds to avoid blocking indefinitely; on timeout status=running and timeout=true are returned. " +
			"Pass one handle_id per call.",
	},
	{
		Name:      "interrupt_acp",
		Group:     GroupOrchestration,
		Label:     "Interrupt ACP agent",
		ShortDesc: "cancel the current turn of a running ACP agent without closing the session",
		LLMDescription: "cancel the current turn of a running ACP agent. " +
			"The session remains alive after interruption — use send_acp to start a new turn or close_acp to terminate the process. " +
			"Returns status=interrupting; follow with wait_acp to confirm the turn has stopped. " +
			"Has no effect if the agent is not in running state.",
	},
	{
		Name:      "close_acp",
		Group:     GroupOrchestration,
		Label:     "Close ACP agent",
		ShortDesc: "close an ACP agent session and terminate the process",
		LLMDescription: "close an ACP agent session and terminate the underlying process. " +
			"Call when no further interaction with this agent is needed. " +
			"Cannot close while a turn is active — call interrupt_acp first, then wait_acp, then close_acp. " +
			"After close_acp, the handle_id is no longer valid.",
	},
	{
		Name:      "spawn_agent",
		Group:     GroupOrchestration,
		Label:     "Spawn agent",
		ShortDesc: "create a sub-agent with its own persona and tools",
		LLMDescription: "create an Arkloop sub-agent that runs as an independent child run with its own persona, tools, and context. " +
			"Use to delegate a self-contained subtask to a specific internal persona (e.g. research, specialized analysis). " +
			"Returns a handle (sub_agent_id) immediately; use wait_agent to retrieve the result. " +
			"IMPORTANT: spawn_agent and wait_agent are always used together — if either is missing from your tool list, load BOTH in one search_tools call: queries=[\"spawn_agent\", \"wait_agent\"]. " +
			"To run tasks in parallel: call spawn_agent N times in the same turn (one per subtask), then call wait_agent once with all ids to return the first to complete. " +
			"persona_id must be one of the registered personas in this project — an invalid ID will fail. " +
			"Do NOT confuse with acp_agent, which delegates to an external sandbox agent.",
	},
	{
		Name:           "send_input",
		Group:          GroupOrchestration,
		Label:          "Send input",
		ShortDesc:      "send a follow-up message to a sub-agent",
		LLMDescription: "send a follow-up message to an existing sub-agent. Call before resume_agent to continue a collaboration thread.",
	},
	{
		Name:           "wait_agent",
		Group:          GroupOrchestration,
		Label:          "Wait agent",
		ShortDesc:      "block until a sub-agent completes and return its result",
		LLMDescription: "block until one or more sub-agents reach a terminal state. Pass multiple ids to wait in parallel and return the first to complete.",
	},
	{
		Name:           "resume_agent",
		Group:          GroupOrchestration,
		Label:          "Resume agent",
		ShortDesc:      "resume a paused sub-agent after sending input",
		LLMDescription: "resume a paused sub-agent after new input has been sent via send_input.",
	},
	{
		Name:           "close_agent",
		Group:          GroupOrchestration,
		Label:          "Close agent",
		ShortDesc:      "close a sub-agent handle",
		LLMDescription: "close a sub-agent handle. Call when no further interaction is needed.",
	},
	{
		Name:           "interrupt_agent",
		Group:          GroupOrchestration,
		Label:          "Interrupt agent",
		ShortDesc:      "cancel the active run of a sub-agent",
		LLMDescription: "cancel the active run of a sub-agent immediately.",
	},
	{
		Name:           "summarize_thread",
		Group:          GroupOrchestration,
		Label:          "Summarize thread",
		ShortDesc:      "update the current thread title with a summary",
		LLMDescription: "update the current thread title with a concise summary.",
	},
	{
		Name:      "timeline_title",
		Group:     GroupOrchestration,
		Label:     "Timeline title",
		ShortDesc: "set a label for the user-facing thinking timeline",
		LLMDescription: "set a short label for the user-facing thinking timeline. " +
			"Call only in parallel with tools that produce visible timeline entries (web_search, python_execute, exec_command, browser). " +
			"Never call alone or alongside web_fetch only. " +
			"Label: single-line plain text, same language as user input. " +
			"Length: 8-16 Chinese characters or <=8 English words.",
	},
	{
		Name:           "ask_user",
		Group:          GroupOrchestration,
		Label:          "Ask user",
		ShortDesc:      "present multiple-choice questions to the user",
		LLMDescription: "present structured multiple-choice questions to the user. Use when a clear choice between specific options is needed.",
	},
	{
		Name:      "todo_write",
		Group:     GroupOrchestration,
		Label:     "Todo write",
		ShortDesc: "manage a structured todo list for the current run",
		LLMDescription: "create and update a structured todo list for the current run. " +
			"Each call fully replaces the list. " +
			"Use to track multi-step plans: start with all items pending, update status as work progresses. " +
			"status must be one of: pending, in_progress, completed, cancelled. " +
			"Only one item should be in_progress at a time.",
	},
}

var byName = buildIndex(registry)

func All() []ToolMeta {
	out := make([]ToolMeta, len(registry))
	copy(out, registry)
	return out
}

func GroupOrder() []string {
	out := make([]string, len(groupOrder))
	copy(out, groupOrder)
	return out
}

func Catalog() []ToolGroup {
	grouped := map[string][]ToolMeta{}
	for _, meta := range registry {
		grouped[meta.Group] = append(grouped[meta.Group], meta)
	}
	out := make([]ToolGroup, 0, len(groupOrder))
	for _, name := range groupOrder {
		tools := grouped[name]
		copied := make([]ToolMeta, len(tools))
		copy(copied, tools)
		out = append(out, ToolGroup{Name: name, Tools: copied})
	}
	return out
}

func Lookup(name string) (ToolMeta, bool) {
	meta, ok := byName[name]
	return meta, ok
}

// Must returns the ToolMeta for the given name, panicking if not found.
// This follows the standard Go Must pattern (regexp.MustCompile, template.Must);
// all callers use it in package-level var blocks, so panics occur at init-time
// and surface immediately on startup rather than at runtime.
func Must(name string) ToolMeta {
	meta, ok := Lookup(name)
	if !ok {
		panic("unknown tool meta: " + name)
	}
	return meta
}

func buildIndex(items []ToolMeta) map[string]ToolMeta {
	index := make(map[string]ToolMeta, len(items))
	for _, item := range items {
		index[item.Name] = item
	}
	return index
}
