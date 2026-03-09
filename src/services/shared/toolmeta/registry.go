package toolmeta

import "fmt"

const (
	GroupWebSearch     = "web_search"
	GroupWebFetch      = "web_fetch"
	GroupSandbox       = "sandbox"
	GroupMemory        = "memory"
	GroupDocument      = "document"
	GroupOrchestration = "orchestration"

	WebSearchDefaultMaxResults = 5
	WebSearchMaxResultsLimit   = 20
	WebSearchMaxQueriesLimit   = 5
	WebFetchMaxLengthLimit     = 200000
)

type ToolMeta struct {
	Name           string
	Group          string
	Label          string
	LLMDescription string
}

type ToolGroup struct {
	Name  string
	Tools []ToolMeta
}

var groupOrder = []string{
	GroupWebSearch,
	GroupWebFetch,
	GroupSandbox,
	GroupMemory,
	GroupDocument,
	GroupOrchestration,
}

var registry = []ToolMeta{
	{
		Name:           "web_search",
		Group:          GroupWebSearch,
		Label:          "Web search",
		LLMDescription: fmt.Sprintf("search the internet and return title, link, and summary for each result. Use the queries array to run up to %d independent searches in a single call; fall back to the scalar query field only when you have a single question. Set max_results per query (default %d, max %d). When formulating queries: (1) decompose a complex question into independent keyword-based queries that together cover the full question with minimal overlap; (2) if a query is vague, rewrite it into a well-defined search by adding context from the conversation; (3) when the timing of an event is uncertain, use neutral phrasing such as 'latest news' or 'updates' instead of assuming a result already exists (good: 'Argentina Elections latest news'; bad: 'Argentina Elections results').", WebSearchMaxQueriesLimit, WebSearchDefaultMaxResults, WebSearchMaxResultsLimit),
	},
	{
		Name:           "web_fetch",
		Group:          GroupWebFetch,
		Label:          "Web fetch",
		LLMDescription: "fetch a web page and return its title and body as plain text. Use this when search results are insufficient but a particular page looks likely to contain deeper information. Prefer official or authoritative pages. Can be called in batch when multiple pages are worth fetching, but avoid re-fetching the same URL.",
	},
	{
		Name:           "python_execute",
		Group:          GroupSandbox,
		Label:          "Python execution",
		LLMDescription: "execute Python code in an isolated sandbox environment. Use this tool for any numerical calculations or data processing instead of computing manually. Pre-installed libraries include numpy, pandas, matplotlib, plotly, scipy, sympy, pillow, scikit-learn, etc. For charts and visualizations, prefer Plotly (plotly.express or plotly.graph_objects) over matplotlib. Use fig.write_image() for PNG output (kaleido is pre-installed). Only fall back to fig.write_html() if write_image fails. Do not set pio.renderers or attempt to open a browser. IMPORTANT: write long-lived working files to /workspace/. Use /tmp/output/ only for final files that should be uploaded as user-visible artifacts. Files written to /tmp/output/ are automatically uploaded; the tool result includes an artifacts array with each file's key, filename, size and mime_type. In your final response, only keys that actually appear in result.artifacts may be referenced as artifact:<key>. If you want to show a file that exists only in /workspace/, you must use Markdown links: images use ![简短说明](workspace:/relative/path), non-images use [文件名](workspace:/relative/path). Never write workspace:/workspace/...; strip the leading /workspace and use workspace:/... instead. Never output malformed forms like ![workspace:path] or bare workspace:/path. Never invent artifact keys from stdout, stderr, or filenames. Never output raw /workspace/... or /tmp/output/... paths as user links.",
	},
	{
		Name:           "exec_command",
		Group:          GroupSandbox,
		Label:          "Command execution",
		LLMDescription: "run a command in a persistent shell session inside the isolated sandbox. Use session_mode=auto unless you need a fresh session, an explicit resume, or a fork. You may set share_scope only when a new session should be created; do not pass share_scope on resume or fork. When the tool returns session_ref, keep using that stable reference in later calls instead of any transient sandbox identifier. Short commands usually finish in this first response. Only switch to write_stdin when the result still shows running=true or when you need to send stdin. IMPORTANT: write long-lived working files that later tools should read to /workspace/. Use /tmp/output/ only for final files that should be uploaded as user-visible artifacts. In your final response, only keys that actually appear in result.artifacts may be referenced as artifact:<key>. If you want to show a file that exists only in /workspace/, you must use Markdown links: images use ![简短说明](workspace:/relative/path), non-images use [文件名](workspace:/relative/path). Never write workspace:/workspace/...; strip the leading /workspace and use workspace:/... instead. Never output malformed forms like ![workspace:path] or bare workspace:/path. Never invent artifact keys from stdout, stderr, or filenames. Never output raw /workspace/... or /tmp/output/... paths as user links.",
	},
	{
		Name:           "write_stdin",
		Group:          GroupSandbox,
		Label:          "Shell stdin",
		LLMDescription: "send stdin to, or poll output from, a running shell session. Pass the session_ref returned by exec_command. Use this only when exec_command returned running=true or when the process is waiting for more stdin. Set chars to a non-empty string to write stdin. Set chars to an empty string, or omit it, to poll for new output without repeating already delivered output. Keep using the same session_ref while you need that shell session's /workspace state. In your final response, only keys that actually appear in result.artifacts may be referenced as artifact:<key>. If you want to show a file that exists only in /workspace/, you must use Markdown links: images use ![简短说明](workspace:/relative/path), non-images use [文件名](workspace:/relative/path). Never write workspace:/workspace/...; strip the leading /workspace and use workspace:/... instead. Never output malformed forms like ![workspace:path] or bare workspace:/path. Never invent artifact keys from stdout, stderr, or filenames. Never output raw /workspace/... or /tmp/output/... paths as user links.",
	},
	{
		Name:           "browser",
		Group:          GroupSandbox,
		Label:          "Browser automation",
		LLMDescription: "execute browser automation commands in the isolated browser sandbox. Pass command as the raw agent-browser CLI subcommand, for example: navigate <url>, snapshot, click <ref>, type <ref> <text>, tab list, console, network. Omit session_ref to reuse the default browser session for the current run or thread. Pass session_ref only when you need to continue a specific browser session or intentionally create an isolated browser session with that stable reference. session_ref is a stable identifier, not a mode flag, so do not pass placeholders like new, resume, or fork unless that exact string is the session reference you want. If a browser result returns running=true, the command is still in flight; do not issue another browser command on that same session_ref until it stops running, and use yield_time_ms when you need the browser call to wait longer before returning. Prefer browser only when web_search or web_fetch cannot complete the task because the page requires JavaScript rendering, form interaction, login flow reproduction, or DOM inspection.",
	},
	{
		Name:           "memory_search",
		Group:          GroupMemory,
		Label:          "Memory search",
		LLMDescription: "search long-term memory for information about the user (preferences, past experiences, constraints, priorities) or past interactions. Use this tool when handling recommendations, comparisons, preference-driven questions, opinions, 'best' options, 'how to' questions, or open-ended problems with multiple valid approaches — user context significantly improves answer quality in areas like shopping, travel planning, and project planning. Call at most once per user query; do not issue multiple memory searches for the same request. Use the results to guide subsequent tool selection — memory provides context, but a complete answer may still require other tools. IMPORTANT: results contain internal fields (such as uri, _ref) that are system identifiers and must never be shown to the user; only present the natural-language content (abstract) to the user, never expose storage paths, URIs, or any internal metadata.",
	},
	{
		Name:           "memory_read",
		Group:          GroupMemory,
		Label:          "Memory read",
		LLMDescription: "read the full content of a memory entry by its URI. IMPORTANT: the URI and other internal fields (_ref, storage paths) are system identifiers and must never be exposed to the user; only present the natural-language content to the user.",
	},
	{
		Name:           "memory_write",
		Group:          GroupMemory,
		Label:          "Memory write",
		LLMDescription: "store a piece of knowledge in long-term memory for future reference",
	},
	{
		Name:           "memory_forget",
		Group:          GroupMemory,
		Label:          "Memory forget",
		LLMDescription: "remove a specific memory entry",
	},
	{
		Name:           "conversation_search",
		Group:          GroupMemory,
		Label:          "Conversation search",
		LLMDescription: "search the current user's visible conversation history across all threads using keywords. Use this when you need to recall facts previously discussed but not stored in long-term memory. Returns recent matching messages with thread_id, role, content snippet, and created_at. This is keyword search over stored messages, not semantic search, and it does not spend model tokens.",
	},
	{
		Name:           "document_write",
		Group:          GroupDocument,
		Label:          "Document write",
		LLMDescription: "write a Markdown document and save it as a downloadable file artifact. Use this tool when the user requests a report, summary, plan, article, or any long-form document. Provide the full Markdown content; the file will be uploaded and returned as a downloadable artifact. Reference the result artifact using [label](artifact:<key>).",
	},
	{
		Name:           "spawn_agent",
		Group:          GroupOrchestration,
		Label:          "Spawn agent",
		LLMDescription: "spawn a sub-agent to execute a task with a specific persona, returns the sub-agent output",
	},
	{
		Name:           "summarize_thread",
		Group:          GroupOrchestration,
		Label:          "Summarize thread",
		LLMDescription: "update the current thread title with a concise summary",
	},
	{
		Name:           "timeline_title",
		Group:          GroupOrchestration,
		Label:          "Timeline title",
		LLMDescription: "UI metadata tool that sets a short label shown in the user-facing thinking timeline. Call this tool in parallel with your first tool call of each round (include it in the same tool_use batch). Also call it when you are only thinking without other tools, to describe what you are considering. The label parameter must be a single-line plain-text phrase (no quotes, no Markdown, no numbering) in the same language as the user's input. Keep it concise: 8-16 characters for Chinese, <=8 words for English. You may prefix with stage words such as 'Searching for ...', 'Analyzing ...', 'Reviewing ...', etc. Call this tool as often as possible to keep the timeline informative.",
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
