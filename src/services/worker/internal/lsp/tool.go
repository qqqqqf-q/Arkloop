//go:build desktop

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	lspRequestTimeout = 15 * time.Second
	errArgsInvalid    = "tool.args_invalid"
	errLSPFailed      = "tool.lsp_failed"
)

// LSPTool implements tools.Executor for LSP operations.
type LSPTool struct {
	manager *Manager
	logger  *slog.Logger
}

// NewLSPTool creates a new LSP tool executor.
func NewLSPTool(manager *Manager, logger *slog.Logger) *LSPTool {
	return &LSPTool{manager: manager, logger: logger}
}

type lspArgs struct {
	Operation          string
	FilePath           string
	Line               int
	Column             int
	Query              string
	NewName            string
	IncludeDeclaration *bool
}

func parseLSPArgs(raw map[string]any) (lspArgs, error) {
	var a lspArgs

	op, _ := raw["operation"].(string)
	if op == "" {
		return a, fmt.Errorf("operation is required")
	}
	a.Operation = op
	a.FilePath, _ = raw["file_path"].(string)
	a.Query, _ = raw["query"].(string)
	a.NewName, _ = raw["new_name"].(string)

	a.Line = toInt(raw["line"])
	a.Column = toInt(raw["column"])
	if v, ok := raw["include_declaration"]; ok {
		if b, ok := v.(bool); ok {
			a.IncludeDeclaration = &b
		}
	}

	return a, nil
}

func validateArgs(a lspArgs) error {
	needsFile := map[string]bool{
		"definition": true, "references": true, "hover": true,
		"document_symbols": true, "type_definition": true,
		"implementation": true, "rename": true,
		"prepare_call_hierarchy": true, "incoming_calls": true,
		"outgoing_calls": true,
	}
	needsPosition := map[string]bool{
		"definition": true, "references": true, "hover": true,
		"type_definition": true, "implementation": true, "rename": true,
		"prepare_call_hierarchy": true, "incoming_calls": true,
		"outgoing_calls": true,
	}

	if needsFile[a.Operation] && a.FilePath == "" {
		return fmt.Errorf("file_path is required for %s", a.Operation)
	}
	if needsPosition[a.Operation] && (a.Line < 1 || a.Column < 1) {
		return fmt.Errorf("line and column (1-based) are required for %s", a.Operation)
	}
	if a.Operation == "workspace_symbols" && a.Query == "" {
		return fmt.Errorf("query is required for workspace_symbols")
	}
	if a.Operation == "rename" && a.NewName == "" {
		return fmt.Errorf("new_name is required for rename")
	}
	return nil
}

func (t *LSPTool) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	a, err := parseLSPArgs(args)
	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errArgsInvalid, Message: err.Error()},
			DurationMs: ms(started),
		}
	}

	// resolve relative path
	if a.FilePath != "" && !filepath.IsAbs(a.FilePath) {
		a.FilePath = filepath.Join(execCtx.WorkDir, a.FilePath)
	}

	if err := validateArgs(a); err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errArgsInvalid, Message: err.Error()},
			DurationMs: ms(started),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, lspRequestTimeout)
	defer cancel()

	// check capability before dispatching
	if method := operationToMethod(a.Operation); method != "" {
		if err := t.checkCapability(a.FilePath, a.Operation, method); err != nil {
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: errLSPFailed, Message: err.Error()},
				DurationMs: ms(started),
			}
		}
	}

	var result string
	switch a.Operation {
	case "definition":
		result, err = t.definition(ctx, a, execCtx.WorkDir)
	case "references":
		result, err = t.references(ctx, a, execCtx.WorkDir)
	case "hover":
		result, err = t.hover(ctx, a)
	case "document_symbols":
		result, err = t.documentSymbols(ctx, a)
	case "workspace_symbols":
		result, err = t.workspaceSymbols(ctx, a)
	case "type_definition":
		result, err = t.typeDefinition(ctx, a, execCtx.WorkDir)
	case "implementation":
		result, err = t.implementation(ctx, a, execCtx.WorkDir)
	case "prepare_call_hierarchy":
		result, err = t.prepareCallHierarchy(ctx, a)
	case "incoming_calls":
		result, err = t.incomingCalls(ctx, a)
	case "outgoing_calls":
		result, err = t.outgoingCalls(ctx, a)
	case "diagnostics":
		result, err = t.diagnostics(ctx, a)
	case "rename":
		result, err = t.rename(ctx, a)
	default:
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errArgsInvalid, Message: "unknown operation: " + a.Operation},
			DurationMs: ms(started),
		}
	}

	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errLSPFailed, Message: err.Error()},
			DurationMs: ms(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"output": result},
		DurationMs: ms(started),
	}
}

func (t *LSPTool) definition(ctx context.Context, a lspArgs, workDir string) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var locs []Location
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		locs, e = c.Definition(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("definition: %w", err)
	}
	locs = filterIgnoredLocations(ctx, workDir, locs)
	if len(locs) == 0 {
		return "No definition found. The symbol may not be recognized, or the LSP server may still be indexing.", nil
	}
	return formatLocationsWithMeta(locs, t.manager.RootDir(), "definition"), nil
}

func (t *LSPTool) typeDefinition(ctx context.Context, a lspArgs, workDir string) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var locs []Location
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		locs, e = c.TypeDefinition(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("type_definition: %w", err)
	}
	locs = filterIgnoredLocations(ctx, workDir, locs)
	if len(locs) == 0 {
		return "No type definition found. The symbol may not have a distinct type definition.", nil
	}
	return formatLocationsWithMeta(locs, t.manager.RootDir(), "type definition"), nil
}

func (t *LSPTool) implementation(ctx context.Context, a lspArgs, workDir string) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var locs []Location
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		locs, e = c.Implementation(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("implementation: %w", err)
	}
	locs = filterIgnoredLocations(ctx, workDir, locs)
	if len(locs) == 0 {
		return "No implementations found. The symbol may be an unexported type, or the LSP server may still be indexing.", nil
	}
	return formatLocationsWithMeta(locs, t.manager.RootDir(), "implementation"), nil
}

func (t *LSPTool) references(ctx context.Context, a lspArgs, workDir string) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)

	includeDecl := true
	if a.IncludeDeclaration != nil {
		includeDecl = *a.IncludeDeclaration
	}

	params := ReferenceParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
		Context: ReferenceContext{IncludeDeclaration: includeDecl},
	}

	var locs []Location
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		locs, e = c.References(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("references: %w", err)
	}
	locs = filterIgnoredLocations(ctx, workDir, locs)
	if len(locs) == 0 {
		return "No references found. Ensure the file is saved and the cursor is on a valid symbol.", nil
	}
	return formatLocationsWithMeta(locs, t.manager.RootDir(), "reference"), nil
}

func (t *LSPTool) hover(ctx context.Context, a lspArgs) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var h *Hover
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		h, e = c.Hover(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("hover: %w", err)
	}
	if h == nil {
		return "No hover information available at this position.", nil
	}
	return formatHover(h), nil
}

func (t *LSPTool) documentSymbols(ctx context.Context, a lspArgs) (string, error) {
	uri := PathToURI(a.FilePath)
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	var syms []DocumentSymbol
	err := t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		syms, e = c.DocumentSymbol(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("document_symbols: %w", err)
	}
	return formatDocumentSymbols(syms, 0), nil
}

func (t *LSPTool) workspaceSymbols(ctx context.Context, a lspArgs) (string, error) {
	params := WorkspaceSymbolParams{Query: a.Query}

	// if file_path provided, use that server specifically
	if a.FilePath != "" {
		var syms []WorkspaceSymbol
		err := t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
			var e error
			syms, e = c.WorkspaceSymbol(ctx, params)
			return e
		})
		if err != nil {
			return "", fmt.Errorf("workspace_symbols: %w", err)
		}
		return formatWorkspaceSymbols(syms, t.manager.RootDir()), nil
	}

	// no file_path: query all running servers and merge results
	t.manager.mu.RLock()
	serversCopy := make(map[string]*ServerInstance, len(t.manager.servers))
	for k, v := range t.manager.servers {
		serversCopy[k] = v
	}
	t.manager.mu.RUnlock()

	if len(serversCopy) == 0 {
		return "No LSP servers active. Open a file first.", nil
	}

	var allSyms []WorkspaceSymbol
	for _, si := range serversCopy {
		var syms []WorkspaceSymbol
		err := si.Execute(ctx, func(c *Client) error {
			var e error
			syms, e = c.WorkspaceSymbol(ctx, params)
			return e
		})
		if err != nil {
			t.logger.Warn("workspace_symbols: server query failed", "err", err)
			continue
		}
		allSyms = append(allSyms, syms...)
	}

	return formatWorkspaceSymbols(allSyms, t.manager.RootDir()), nil
}

func (t *LSPTool) prepareCallHierarchy(ctx context.Context, a lspArgs) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var items []CallHierarchyItem
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		var e error
		items, e = c.PrepareCallHierarchy(ctx, params)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("prepare_call_hierarchy: %w", err)
	}
	if len(items) == 0 {
		return "No call hierarchy item found at this position.", nil
	}
	return formatCallHierarchyItems(items, t.manager.RootDir()), nil
}

func (t *LSPTool) incomingCalls(ctx context.Context, a lspArgs) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var calls []CallHierarchyIncomingCall
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		items, e := c.PrepareCallHierarchy(ctx, params)
		if e != nil {
			return e
		}
		if len(items) == 0 {
			return nil
		}
		calls, e = c.IncomingCalls(ctx, CallHierarchyIncomingCallsParams{Item: items[0]})
		return e
	})
	if err != nil {
		return "", fmt.Errorf("incoming_calls: %w", err)
	}
	if len(calls) == 0 {
		return "No incoming calls found.", nil
	}
	return formatIncomingCalls(calls, t.manager.RootDir()), nil
}

func (t *LSPTool) outgoingCalls(ctx context.Context, a lspArgs) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var calls []CallHierarchyOutgoingCall
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		items, e := c.PrepareCallHierarchy(ctx, params)
		if e != nil {
			return e
		}
		if len(items) == 0 {
			return nil
		}
		calls, e = c.OutgoingCalls(ctx, CallHierarchyOutgoingCallsParams{Item: items[0]})
		return e
	})
	if err != nil {
		return "", fmt.Errorf("outgoing_calls: %w", err)
	}
	if len(calls) == 0 {
		return "No outgoing calls found.", nil
	}
	return formatOutgoingCalls(calls, t.manager.RootDir()), nil
}

func (t *LSPTool) diagnostics(ctx context.Context, a lspArgs) (string, error) {
	if a.FilePath != "" {
		_ = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
			return c.EnsureOpen(ctx, a.FilePath)
		})
	}

	// wait for LSP to process recent edits
	if t.manager.DiagRegistry().HasRecentEdits() {
		time.Sleep(ActiveWaitDelay)
	}

	formatted := t.manager.DiagRegistry().FormatForPrompt(t.manager.RootDir())
	if formatted == "" {
		return "No diagnostics.", nil
	}
	return formatted, nil
}

func (t *LSPTool) rename(ctx context.Context, a lspArgs) (string, error) {
	pos, err := ExternalToLSPPosition(a.FilePath, a.Line, a.Column)
	if err != nil {
		return "", fmt.Errorf("position conversion: %w", err)
	}
	uri := PathToURI(a.FilePath)

	var edit *WorkspaceEdit
	err = t.manager.ExecuteOnFile(ctx, a.FilePath, func(c *Client) error {
		// validate rename is possible
		_, prepErr := c.PrepareRename(ctx, TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     pos,
		})
		if prepErr != nil {
			return fmt.Errorf("rename not valid at this position: %w", prepErr)
		}

		var renameErr error
		edit, renameErr = c.Rename(ctx, RenameParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri},
				Position:     pos,
			},
			NewName: a.NewName,
		})
		return renameErr
	})
	if err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}

	summary, err := applyWorkspaceEdit(edit)
	if err != nil {
		return "", fmt.Errorf("apply rename edits: %w", err)
	}

	// notify LSP servers of changed files with fresh context
	notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer notifyCancel()
	for _, fp := range summary.ChangedFiles {
		if err := t.manager.NotifyFileChanged(notifyCtx, fp); err != nil {
			t.logger.Warn("rename: failed to notify file change", "path", fp, "err", err)
		}
	}

	return formatRenameResult(summary), nil
}

// filterIgnoredLocations removes locations in gitignored files.
func filterIgnoredLocations(ctx context.Context, workDir string, locs []Location) []Location {
	if len(locs) == 0 || workDir == "" {
		return locs
	}

	// collect unique file paths
	pathSet := make(map[string]struct{})
	for _, loc := range locs {
		p, err := URIToPath(loc.URI)
		if err == nil {
			pathSet[p] = struct{}{}
		}
	}
	if len(pathSet) == 0 {
		return locs
	}

	ignored := gitCheckIgnore(ctx, workDir, pathSet)
	if len(ignored) == 0 {
		return locs
	}

	filtered := make([]Location, 0, len(locs))
	for _, loc := range locs {
		p, err := URIToPath(loc.URI)
		if err != nil {
			filtered = append(filtered, loc)
			continue
		}
		if !ignored[p] {
			filtered = append(filtered, loc)
		}
	}
	return filtered
}

// gitCheckIgnore runs `git check-ignore --stdin` and returns ignored paths.
func gitCheckIgnore(ctx context.Context, workDir string, paths map[string]struct{}) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "check-ignore", "--stdin")
	cmd.Dir = workDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}

	var out strings.Builder
	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		return nil
	}

	w := bufio.NewWriter(stdin)
	for p := range paths {
		_, _ = w.WriteString(p + "\n")
	}
	_ = w.Flush()
	_ = stdin.Close()

	// git check-ignore exits 1 when no files are ignored
	_ = cmd.Wait()

	result := make(map[string]bool)
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			result[line] = true
		}
	}
	return result
}

// toInt converts a value from map[string]any to int.
// Handles string, float64, int, json.Number — all types the framework may deliver.
func toInt(v any) int {
	switch n := v.(type) {
	case string:
		i, _ := strconv.Atoi(n)
		return i
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func ms(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}

// operationToMethod maps tool operation names to LSP method strings.
func operationToMethod(op string) string {
	switch op {
	case "definition":
		return MethodDefinition
	case "references":
		return MethodReferences
	case "hover":
		return MethodHover
	case "document_symbols":
		return MethodDocumentSymbol
	case "workspace_symbols":
		return MethodWorkspaceSymbol
	case "type_definition":
		return MethodTypeDefinition
	case "implementation":
		return MethodImplementation
	case "rename":
		return MethodRename
	case "prepare_call_hierarchy", "incoming_calls", "outgoing_calls":
		return MethodPrepareCallHierarchy
	default:
		return ""
	}
}

// checkCapability verifies the LSP server supports the given method.
func (t *LSPTool) checkCapability(filePath, operation, method string) error {
	if filePath == "" {
		return nil
	}
	si, err := t.manager.ServerForFile(filePath)
	if err != nil {
		return nil // let the actual call handle the error
	}
	si.mu.Lock()
	client := si.client
	si.mu.Unlock()
	if client == nil {
		return nil // server not started yet, let ensureStarted handle it
	}
	if !client.ServerCapabilities().Supports(method) {
		ext := filepath.Ext(filePath)
		return fmt.Errorf("LSP server for %s files does not support %s", ext, operation)
	}
	return nil
}
