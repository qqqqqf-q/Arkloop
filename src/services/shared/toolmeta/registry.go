package toolmeta

import "fmt"

const (
	GroupWebSearch     = "web_search"
	GroupWebFetch      = "web_fetch"
	GroupSandbox       = "sandbox"
	GroupMemory        = "memory"
	GroupDocument      = "document"
	GroupOrchestration = "orchestration"
	GroupDiscovery     = "discovery"
	GroupFilesystem    = "filesystem"

	WebSearchDefaultMaxResults = 5
	WebSearchMaxResultsLimit   = 20
	WebSearchMaxQueriesLimit   = 5
	WebFetchMaxLengthLimit     = 200000
)

type ToolMeta struct {
	Name           string
	Group          string
	Label          string
	ShortDesc      string // ~20 tokens, always injected into context for load_tools catalog
	LLMDescription string // full description, loaded on demand via load_tools
}

type ToolGroup struct {
	Name  string
	Tools []ToolMeta
}

var groupOrder = []string{
	GroupDiscovery,
	GroupWebSearch,
	GroupWebFetch,
	GroupSandbox,
	GroupFilesystem,
	GroupMemory,
	GroupDocument,
	GroupOrchestration,
}

var registry = []ToolMeta{
	// ── discovery ──
	{
		Name:      "load_tools",
		Group:     GroupDiscovery,
		Label:     "Load tools",
		ShortDesc: "load tools from this runtime catalog by tool name or catalog keyword",
		LLMDescription: "load tools from this platform's runtime catalog only: match by exact or partial tool name, or by words that appear in each tool's short catalog description. " +
			"Not for the public web, not for project names, papers, news, or general questions — use web_search (or answer from training) for those. " +
			"Do not pass full natural-language research prompts as queries; they will not match any tool. " +
			"Use when you need a callable tool that is not in your current tool set. " +
			"Pass multiple catalog lookups in one call to batch-load several tools — never call this tool twice in a row for related tools. " +
			"Think ahead: if you will need a group of tools together (e.g. spawn_agent + wait_agent, read + edit), load all of them in a single call. " +
			"After this call succeeds, matched tools may be injected into the real tool list in later phases of the same reasoning loop. " +
			"Only call them after they actually appear there.",
	},
	{
		Name:      "load_skill",
		Group:     GroupDiscovery,
		Label:     "Load skill",
		ShortDesc: "load one available skill into the current conversation by exact skill name",
		LLMDescription: "load one skill from <available_skills> into the current conversation. " +
			"Use the exact skill name shown in the skill catalog. " +
			"Call this before relying on a skill's instructions or specialized workflow. " +
			"This tool only loads skills visible to the current run; it does not search the web or arbitrary filesystem paths.",
	},
	{
		Name:      "arkloop_help",
		Group:     GroupDiscovery,
		Label:     "Arkloop help",
		ShortDesc: "search official Arkloop product, architecture, and Desktop help text bundled with the runtime",
		LLMDescription: "Search authoritative, version-bundled documentation about Arkloop: what the product is, service architecture, Desktop vs server, Electron (not Tauri), settings navigation, Telegram channel setup, and memory/channel identity rules. " +
			"Call before answering questions about Arkloop itself, stack facts, or how to configure Desktop—do not invent from model weights. " +
			"Pass the user's question or keywords in query; optional limit (1–12) caps how many text chunks are returned.",
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
				"max_results per query defaults to %d (max %d). "+
				"Use web_search for finding up-to-date information, documentation, current events, and facts beyond your knowledge cutoff. "+
				"Use web_fetch to follow specific URLs from search results for deeper information. "+
				"Do not use web_search for questions answerable from training data, for file operations, or for running code.",
			WebSearchMaxQueriesLimit, WebSearchDefaultMaxResults, WebSearchMaxResultsLimit),
	},
	{
		Name:      "web_fetch",
		Group:     GroupWebFetch,
		Label:     "Web fetch",
		ShortDesc: "fetch a web page and return its content as text",
		LLMDescription: "fetch a web page and return its title and body as plain text. " +
			"Use when search snippets are insufficient and a specific page likely contains deeper information. " +
			"Prefer official or authoritative sources (official documentation, Wikipedia, reputable news sites). " +
			"Do not fetch the same URL multiple times in a single conversation — the result is cached for 15 minutes. " +
			"If a URL redirects to a different host, the tool will inform you and provide the redirect URL; make a new WebFetch call with the redirect URL. " +
			"The URL must be a fully-formed valid URL. HTTP URLs are automatically upgraded to HTTPS. " +
			"This tool is read-only and does not modify any files. " +
			"Do not use web_fetch for file operations, code execution, or tasks that other dedicated tools handle.",
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
			"Work in the sandbox current working directory, normally /workspace/. Put final downloadable files in /tmp/output/ only when you want them auto-uploaded as artifacts. " +
			"Reference outputs with the real resource you have:\n" +
			"  • /tmp/output/ files appear in result.artifacts → reference as artifact:<key>  (e.g. ![alt](artifact:abc/run/file.png))\n" +
			"  • files in the current working directory → reference the exact absolute file_path  (e.g. [report](/workspace/data/report.html))\n" +
			"Only reference artifact keys that actually appear in result.artifacts. " +
			"Do not invent legacy resource links, artifact keys, or file paths. Never output raw /tmp/output/ paths.",
	},
	{
		Name:      "exec_command",
		Group:     GroupSandbox,
		Label:     "Command execution",
		ShortDesc: "run a shell command in the sandbox, either buffered or as an explicit interactive process",
		LLMDescription: "run a shell command in the sandbox. Default mode is buffered, which executes one command to completion with stdin closed. " +
			"Use follow for long-running output-only processes, stdin for non-PTY processes that need later input, and pty only for real terminal-style interaction. " +
			"The backend returns a process_ref only for follow/stdin/pty modes. Continue those processes with continue_process, terminate them with terminate_process, and resize only pty processes with resize_process. " +
			"When you only need to change directories, prefer the cwd parameter instead of prefixing the command with cd &&. " +
			"Always quote file paths that contain spaces with double quotes (e.g., cd \"path with spaces/file.txt\"). " +
			"Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of cd. You may use cd if the user explicitly requests it. " +
			"You may specify an optional timeout in milliseconds (up to 1800000ms / 30 minutes). By default, the command will timeout after 120000ms (2 minutes). " +
			"When issuing multiple commands:\n" +
			"  - If the commands are independent and can run in parallel, make multiple exec_command tool calls in a single message.\n" +
			"  - If the commands depend on each other and must run sequentially, use a single exec_command call with '&&' to chain them together.\n" +
			"  - Use ';' only when you need to run commands sequentially but don't care if earlier commands fail.\n" +
			"  - DO NOT use newlines to separate commands (newlines are ok in quoted strings).\n" +
			"IMPORTANT: Avoid using this tool for file operations or code search. Use the appropriate dedicated tool:\n" +
			"  - Read files: Use read (NOT cat, head, tail, less, more)\n" +
			"  - Edit files: Use edit (NOT sed, awk)\n" +
			"  - Write files: Use write_file (NOT echo >, cat <<EOF, tee, or shell redirects)\n" +
			"  - Search files: Use glob (NOT find, fd, ls)\n" +
			"  - Search content: Use grep (NOT grep, rg, ag)\n" +
			"  - Fetch URLs: Use web_fetch (NOT curl, wget)\n" +
			"Do NOT redirect command output to temporary files (e.g., \"git diff > /tmp/out.txt\") to work around output length limits. If a tool result is large, the system persists it automatically and provides a filepath — use grep to search it or read with offset/limit to page through it. Do NOT read the entire persisted file.\n" +
			"Do NOT use shell redirection (>, >>, | tee) to write files — use write_file or edit. " +
			"Avoid unnecessary sleep commands:\n" +
			"  - Do not sleep between commands that can run immediately — just run them.\n" +
			"  - Do not retry failing commands in a sleep loop — diagnose the root cause.\n" +
			"  - If a command is long-running and you would like to be notified when it finishes — use run_in_background. No sleep needed.\n" +
			"Avoid interactive flags in buffered mode: do not pass -i, --interactive, or similar interactive flags to commands in buffered mode — they will cause the command to hang. Use pty mode for interactive commands.\n" +
			"Shell injection: when constructing commands that include variable data or user-provided strings, always quote them properly. Never pass unvalidated user input directly to shell commands. Use single quotes for literal strings, double quotes for strings containing variables.\n" +
			"Error handling: when a command fails, read the error output first. Do not blindly retry the same command — diagnose the root cause, fix the issue, then retry. If a command fails after reasonable attempts, report the failure rather than continuing to retry.\n" +
			"For git commands:\n" +
			"  - Prefer to create a new commit rather than amending an existing commit.\n" +
			"  - Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative that achieves the same goal.\n" +
			"  - Never skip hooks (--no-verify) or bypass signing (--no-gpg-sign) unless the user has explicitly asked for it. If a hook fails, investigate and fix the underlying issue.\n" +
			"  - CRITICAL: Always create NEW commits rather than amending, unless the user explicitly requests a git amend. When a pre-commit hook fails, the commit did NOT happen — so --amend would modify the PREVIOUS commit, which may destroy work.\n" +
			"  - When staging files, prefer adding specific files by name rather than using \"git add -A\" or \"git add .\", which can accidentally include sensitive files or large binaries.\n" +
			"  - NEVER commit changes unless the user explicitly asks you to.\n" +
			"Work in the current working directory, normally /workspace/ in sandbox runs. Put final downloadable files in /tmp/output/ only when you want them auto-uploaded as artifacts. " +
			"Reference outputs with the real resource you have:\n" +
			"  • /tmp/output/ files appear in result.artifacts → reference as artifact:<key>\n" +
			"  • files in the current working directory → reference the exact absolute file_path\n" +
			"  • running web apps or pages that should open in the right panel → reference as browser:<http-or-https-url>, for example [preview](browser:http://localhost:5173)\n" +
			"Normal http/https Markdown links open externally; use browser: only when the user should preview a URL inside Arkloop. " +
			"Only reference artifact keys that actually appear in result.artifacts. " +
			"Do not invent legacy resource links, artifact keys, or file paths. Never output raw /tmp/output/ paths.",
	},
	{
		Name:      "continue_process",
		Group:     GroupSandbox,
		Label:     "Continue process",
		ShortDesc: "read new output from a running process and optionally send stdin",
		LLMDescription: "continue a running process started by exec_command in follow, stdin, or pty mode. " +
			"Pass the process_ref and the last next_cursor you received. " +
			"Omit stdin_text to only read new output. Provide stdin_text together with input_seq when the process accepts stdin. " +
			"Use close_stdin when the process is waiting for EOF rather than more text. " +
			"Work in the current working directory, normally /workspace/ in sandbox runs; final downloadable files go to /tmp/output/. " +
			"Show existing work files by linking the exact absolute file_path. " +
			"Never invent artifact keys, legacy resource links, or file paths.",
	},
	{
		Name:           "terminate_process",
		Group:          GroupSandbox,
		Label:          "Terminate process",
		ShortDesc:      "terminate a running sandbox process by process_ref",
		LLMDescription: "terminate a running process started by exec_command. Use when a follow/stdin/pty process should stop and you no longer want to wait for it. Pass the process_ref returned by exec_command.",
	},
	{
		Name:           "resize_process",
		Group:          GroupSandbox,
		Label:          "Resize PTY",
		ShortDesc:      "resize a running PTY process by process_ref",
		LLMDescription: "resize a running PTY process started by exec_command with mode=pty. Use only for real terminal sessions when rows or cols need to change. This tool is not for normal buffered commands.",
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
			"The file_path parameter must be an absolute path, not a relative path. " +
			"When you already know which part of the file you need, only read that part using offset and limit — this is important for larger files. " +
			"Otherwise it's recommended to read the whole file by not providing offset and limit. " +
			"For persisted tool output files (filepath from a tool result with \"persisted\": true): do NOT read the entire file — use grep to search for specific content, or read with offset/limit to page through sections. " +
			"The preview already shows the first and last portions; the tail often contains the most relevant output. " +
			"For message_attachment and remote_url: read image bytes and return textual understanding from prompt. " +
			"Use prompt only for image sources. " +
			"You must always read a file before editing or overwriting it — the edit and write_file tools require a prior read call and will error otherwise. " +
			"If you read a file that exists but has empty contents you will receive a system reminder warning in place of file contents.",
	},
	{
		Name:      "write_file",
		Group:     GroupFilesystem,
		Label:     "Write file",
		ShortDesc: "create or overwrite a file",
		LLMDescription: "create a new file or overwrite an existing file with the provided content. " +
			"Parent directories are created automatically. " +
			"When overwriting an existing file, you must read it first with source.kind=file_path; omitting it will return an error. " +
			"Prefer edit over write_file when making targeted changes to existing files — edit only sends the diff while write_file replaces the entire file. " +
			"NEVER create documentation files (*.md) or README files unless explicitly requested by the user. " +
			"Only use this tool to create new files or for complete rewrites of existing files.",
	},
	{
		Name:      "edit",
		Group:     GroupFilesystem,
		Label:     "Edit file",
		ShortDesc: "replace a unique string in a file (str_replace semantics)",
		LLMDescription: "replace one occurrence of old_string with new_string in the specified file. " +
			"old_string must match exactly once — include enough surrounding context (3-5 lines before and after) to ensure uniqueness. " +
			"Set replace_all=true to replace all occurrences of old_string instead of requiring uniqueness. " +
			"To create a new file: set old_string to empty. To delete content: set new_string to empty. " +
			"You must call read with source.kind=file_path before editing an existing file (old_string non-empty); omitting it will return an error. " +
			"When editing text from read output, preserve the exact indentation (tabs/spaces) as it appears after the line number prefix. Never include any part of the line number prefix in old_string or new_string. " +
			"ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required. " +
			"Use the smallest old_string that clearly identifies the target — usually 2-4 adjacent lines is sufficient. " +
			"Use replace_all for renaming variables or replacing all occurrences of a string across the file. " +
			"Prefer edit over write_file for modifying existing files — it only sends the diff and avoids overwriting unrelated content.",
	},
	{
		Name:      "glob",
		Group:     GroupFilesystem,
		Label:     "Glob files",
		ShortDesc: "find files by glob pattern",
		LLMDescription: "find files matching a glob pattern and return their paths. " +
			"Uses ripgrep when available for speed; falls back to Go filepath walk. " +
			"Results are sorted by path length (shortest first). Maximum 1000 results. " +
			"Patterns like **/*.go, src/**/*.ts, *.md are supported. " +
			"Use glob to discover files before reading them. " +
			"Do not use exec_command with find, fd, or ls for file search — use glob instead for consistent output and proper file tracking.",
	},
	{
		Name:      "grep",
		Group:     GroupFilesystem,
		Label:     "Grep files",
		ShortDesc: "search file contents by regex pattern",
		LLMDescription: "search file contents for a regex pattern. Three output modes: " +
			"files_with_matches (default, returns file paths), content (matching lines with context), count (match counts per file). " +
			"Uses ripgrep when available; falls back to Go regex walk. Results sorted by modification time (newest first). " +
			"Use include to restrict to specific file types (e.g. *.go) for faster results. " +
			"Supports pagination via limit (default 200, max 1000) and offset. " +
			"In content mode, context_lines (0-10) adds surrounding lines; when omitted, auto-context is applied based on match count. " +
			"Set case_sensitive=false for case-insensitive matching. Set multiline=true for cross-line pattern matching. " +
			"Use file_type to filter by ripgrep file types (comma-separated, e.g. 'go,ts'). " +
			"Use grep to identify relevant files before reading them — search first, then read only the files that match. " +
			"Do not use exec_command with grep, rg, or ag for code search — use grep instead for consistent output and proper file tracking.",
	},
	// ── memory ──
	{
		Name:      "memory_list",
		Group:     GroupMemory,
		Label:     "Memory list",
		ShortDesc: "list memory entries or directory contents",
		LLMDescription: "list memory entries to browse what is stored in long-term memory. " +
			"Call without uri to list top-level entries or recent memories. " +
			"Call with a directory uri to list its contents. " +
			"Use limit to control how many entries to return (default 50, max 100). " +
			"Use offset to skip entries for pagination; call repeatedly with increasing offset to browse all memories. " +
			"Each result includes a uri that can be passed to memory_read for full content.",
	},
	{
		Name:      "notebook_read",
		Group:     GroupMemory,
		Label:     "Notebook read",
		ShortDesc: "read the stable notebook snapshot or one notebook entry",
		LLMDescription: "read Notebook content that is stably injected into future conversations. " +
			"Call without uri to read the full current notebook snapshot. " +
			"Call with a local://memory/<id> uri copied from notebook_write to read one notebook entry. " +
			"Notebook is for long-lived maintained notes such as persona preferences, style guides, and standing instructions.",
	},
	{
		Name:      "notebook_write",
		Group:     GroupMemory,
		Label:     "Notebook write",
		ShortDesc: "store a long-lived notebook entry",
		LLMDescription: "store a long-lived notebook entry that should remain stably injected into future conversations. " +
			"Use this for maintained notes, long-form preferences, persona soul-like instructions, or anything that should not depend on semantic recall. " +
			"After success the result includes a local://memory/<id> uri; use that exact uri for notebook_read, notebook_edit, or notebook_forget.",
	},
	{
		Name:      "notebook_edit",
		Group:     GroupMemory,
		Label:     "Notebook edit",
		ShortDesc: "edit one long-lived notebook entry by URI",
		LLMDescription: "edit one existing notebook entry by exact local://memory/<id> uri returned from notebook_write. " +
			"Only Notebook entries are editable. Do not use this on OpenViking memory URIs.",
	},
	{
		Name:      "notebook_forget",
		Group:     GroupMemory,
		Label:     "Notebook forget",
		ShortDesc: "remove one notebook entry by URI",
		LLMDescription: "remove one notebook entry by exact local://memory/<id> uri returned from notebook_write. " +
			"Only Notebook entries are removable with this tool.",
	},
	{
		Name:      "memory_search",
		Group:     GroupMemory,
		Label:     "Memory search",
		ShortDesc: "search long-term memory for user preferences and context",
		LLMDescription: "search auto-organized long-term memory for user preferences, past experiences, constraints, or prior interactions. " +
			"Use for recommendations, comparisons, preference-driven questions, or open-ended problems where user context improves quality. " +
			"Call at most once per query. Results may inform subsequent tool choices but rarely suffice alone. " +
			"Each hit includes uri and kind. For kind=memory, pass that exact uri to memory_read, memory_edit, or memory_forget. For kind=thread, pass that exact uri to memory_read or use thread_id with memory_thread_fetch. " +
			"This memory is auto-recalled and may not appear every turn. " +
			"Internal fields (uri, _ref) are system identifiers — never expose raw uri text to the user unless they explicitly need to copy it.",
	},
	{
		Name:      "memory_thread_search",
		Group:     GroupMemory,
		Label:     "Memory thread search",
		ShortDesc: "search historical conversation threads",
		LLMDescription: "search historical conversation threads when you need prior discussion context that was preserved as thread history. " +
			"For Nowledge, you can optionally pass source to restrict results to one client such as arkloop. " +
			"Use this to find relevant past conversations by keyword before fetching the full thread. " +
			"Results identify candidate thread_id values for memory_thread_fetch.",
	},
	{
		Name:      "memory_thread_fetch",
		Group:     GroupMemory,
		Label:     "Memory thread fetch",
		ShortDesc: "fetch paginated messages from one thread",
		LLMDescription: "fetch paginated messages from one historical conversation thread by thread_id. " +
			"Start small and only fetch more pages when you need extra detail from that thread.",
	},
	{
		Name:      "memory_connections",
		Group:     GroupMemory,
		Label:     "Memory connections",
		ShortDesc: "explore graph connections around one memory or topic",
		LLMDescription: "explore graph connections around one memory or topic in the Nowledge knowledge graph. " +
			"Use when you need related memories, entities, graph neighbors, or source-linked context that normal semantic search would not surface. " +
			"Pass either memory_id from a memory result or query to search first, then expand. " +
			"Results include edge_type, relation, weight, node_type, and node identifiers for follow-up.",
	},
	{
		Name:      "memory_timeline",
		Group:     GroupMemory,
		Label:     "Memory timeline",
		ShortDesc: "browse chronological knowledge activity",
		LLMDescription: "browse chronological Nowledge activity when the user asks what happened over time, what they worked on last week, or when a memory/document/insight was recorded. " +
			"Supports last_n_days, date_from/date_to, and event_type filters. " +
			"Results are grouped activity records with event labels, dates, and related memory ids for follow-up.",
	},
	{
		Name:      "memory_context",
		Group:     GroupMemory,
		Label:     "Working Memory",
		ShortDesc: "read or patch today's Working Memory",
		LLMDescription: "read or patch today's Working Memory in Nowledge. " +
			"Call with no parameters to read today's briefing. " +
			"Use patch_section plus patch_content to replace one markdown section, or patch_section plus patch_append to append text to one section without overwriting the rest.",
	},
	{
		Name:      "memory_status",
		Group:     GroupMemory,
		Label:     "Memory status",
		ShortDesc: "check memory backend status",
		LLMDescription: "check the active memory backend status for debugging. " +
			"For Nowledge, returns backend health, version, base_url, api_key_configured, and Working Memory availability so you can verify the integration is actually working.",
	},
	{
		Name:      "memory_read",
		Group:     GroupMemory,
		Label:     "Memory read",
		ShortDesc: "read the full content of a memory entry by URI",
		LLMDescription: "read the full content of an auto-organized memory entry by URI copied from a memory_search hit or from memory_write. " +
			"For Nowledge, MEMORY.md is a valid alias for Working Memory, and nowledge://thread/... is a valid thread URI from memory_search. Results may include source_thread_id when the memory was distilled from a conversation. " +
			"For Nowledge, optional from and lines let you read an exact snippet range instead of the whole entry. " +
			"These URIs belong to semantic memory recall, not Notebook. Never guess uri from category/key alone.",
	},
	{
		Name:      "memory_write",
		Group:     GroupMemory,
		Label:     "Memory write",
		ShortDesc: "store knowledge in long-term memory",
		LLMDescription: "store knowledge in auto-organized long-term memory for future semantic recall. " +
			"Use this for events, entities, and preferences that do not need to be stably injected every turn. " +
			"If you need a stable maintained note, use notebook_write instead.",
	},
	{
		Name:      "memory_edit",
		Group:     GroupMemory,
		Label:     "Memory edit",
		ShortDesc: "overwrite one semantic memory entry by URI",
		LLMDescription: "overwrite one existing auto-organized memory entry by exact URI, usually copied from memory_search or memory_read context. " +
			"Use this when a semantic memory is still the same memory object but its full content should be replaced. " +
			"Do not use this for Notebook entries; use notebook_edit instead.",
	},
	{
		Name:           "memory_forget",
		Group:          GroupMemory,
		Label:          "Memory forget",
		ShortDesc:      "remove a specific memory entry by URI",
		LLMDescription: "remove a specific auto-organized memory entry by URI from memory_search or memory_write (same rules as memory_read). Use notebook_forget for Notebook entries.",
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
	{
		Name:      "group_history_search",
		Group:     GroupMemory,
		Label:     "Group history search",
		ShortDesc: "keyword-search current group chat history",
		LLMDescription: "keyword-search the current group chat history. " +
			"Returns matching messages with role, content snippet, attachment_keys (for images), and timestamp. " +
			"Use to recall previously discussed topics, shared images, or facts from earlier in this group conversation. " +
			"To view an image from results or from an [image attachment_key=...] placeholder in context, " +
			"call the read tool with source.kind=\"message_attachment\" and source.attachment_key set to the key. " +
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
			"IMPORTANT: after calling this tool, you MUST reference the artifact in your response using [title](artifact:<key>) where <key> is from the tool result. " +
			"The document will NOT be visible to the user unless you include this reference.",
	},
	{
		Name:      "image_generate",
		Group:     GroupDocument,
		Label:     "Generate image",
		ShortDesc: "generate an image and save it as an artifact",
		LLMDescription: "generate an image from a text prompt using the configured image model and save it as an artifact. " +
			"You may optionally provide input_images as artifact references plus simple output options such as size, quality, background, and output_format when the user asks for them. " +
			"Use when the user explicitly asks for image generation or a visual asset. " +
			"After this tool succeeds, you MUST render the result in your final response with Markdown image syntax: ![short alt text](artifact:<key>). " +
			"If the user also wants the generated file sent to Telegram or another tool that accepts artifacts, reuse the exact artifact key returned by this tool instead of inventing a URL or path. " +
			"Do not mention raw storage paths. Do not invent artifact keys. " +
			"If the tool fails, explain the failure plainly instead of pretending the image exists.",
	},
	{
		Name:      "resource_copy",
		Group:     GroupFilesystem,
		Label:     "Copy resource",
		ShortDesc: "copy artifact or uploaded attachment bytes into the agent work directory",
		LLMDescription: "copy an existing resource into the agent filesystem. " +
			"Use when you need to inspect, transform, or combine a user-uploaded attachment or generated artifact with filesystem tools. " +
			"source_uri must be a real URI already present in the conversation or tool result: artifact:<key> or attachment:<key>. " +
			"target_path must be an absolute file path inside the active work directory; in sandbox runs this is often /workspace/input.png, while Desktop may use a local project folder. " +
			"The result includes file_path; reference that exact absolute file_path in Markdown when you want the user to open it. " +
			"Do not call this just to show an existing file; link the exact absolute file_path already present. Do not invent resource keys or file paths.",
	},
	// ── orchestration ──
	{
		Name:      "spawn_agent",
		Group:     GroupOrchestration,
		Label:     "Spawn agent",
		ShortDesc: "create a sub-agent with its own persona and tools",
		LLMDescription: "create an Arkloop sub-agent that runs as an independent child run with its own persona, tools, and context. " +
			"Use to delegate a self-contained subtask to a specific internal persona (e.g. research, specialized analysis). " +
			"Returns a handle (sub_agent_id) immediately; use wait_agent to retrieve the result. " +
			"IMPORTANT: spawn_agent and wait_agent are always used together — if either is missing from your tool list, load BOTH in one load_tools call: queries=[\"spawn_agent\", \"wait_agent\"]. " +
			"To run tasks in parallel: call spawn_agent N times in the same turn (one per subtask), then call wait_agent once with all ids to return the first to complete. " +
			"persona_id must be one of the registered personas in this project — an invalid ID will fail.",
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
			"Each call fully replaces the list — include ALL items, not just new ones. " +
			"Use proactively for complex multi-step tasks (3+ distinct steps), non-trivial tasks requiring planning, or when the user provides multiple tasks. " +
			"Do NOT use for single straightforward tasks, trivial one-step operations, or purely conversational questions. " +
			"Start with all items as pending, mark one as in_progress before beginning work on it, mark it completed when done. " +
			"Only ONE item should be in_progress at a time. " +
			"Mark tasks as completed as soon as you finish them — do not batch completions. " +
			"Status workflow: pending → in_progress → completed (or cancelled). " +
			"Use clear, specific subjects in imperative form (e.g., \"Fix authentication bug\" not \"auth\"). " +
			"When useful, include active_form as the present-progress phrase for an in-progress item (e.g., \"Fixing authentication bug\").",
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
