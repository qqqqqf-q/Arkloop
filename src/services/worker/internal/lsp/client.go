package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

const fileSizeLimit = 10 * 1024 * 1024 // 10MB

type Client struct {
	transport  *Transport
	serverCaps ServerCapabilities
	rootURI    string
	initOpts   any
	openDocs   map[string]*openDocument // URI -> doc
	docMu      sync.Mutex
	logger     *slog.Logger
}

type openDocument struct {
	URI        string
	LanguageID string
	Version    atomic.Int32
}

func NewClient(transport *Transport, logger *slog.Logger) *Client {
	return &Client{
		transport: transport,
		openDocs:  make(map[string]*openDocument),
		logger:    logger,
	}
}

func (c *Client) Initialize(ctx context.Context, rootURI string, initOpts any) (*InitializeResult, error) {
	c.rootURI = rootURI
	c.initOpts = initOpts

	rootPath, err := URIToPath(rootURI)
	if err != nil {
		rootPath = "workspace"
	}

	params := InitializeParams{
		ProcessID:             os.Getpid(),
		RootURI:               rootURI,
		Capabilities:          DefaultClientCapabilities(),
		InitializationOptions: initOpts,
		WorkspaceFolders: []WorkspaceFolder{
			{URI: rootURI, Name: filepath.Base(rootPath)},
		},
	}

	raw, err := c.transport.Call(ctx, MethodInitialize, params)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	var result InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal initialize result: %w", err)
	}
	c.serverCaps = result.Capabilities

	if err := c.transport.Notify(ctx, MethodInitialized, struct{}{}); err != nil {
		return nil, fmt.Errorf("initialized notification: %w", err)
	}

	return &result, nil
}

func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.transport.Call(ctx, MethodShutdown, nil)
	if err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return c.transport.Notify(ctx, MethodExit, nil)
}

func (c *Client) DidOpen(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if info.Size() > fileSizeLimit {
		return fmt.Errorf("file too large: %d bytes (limit %d)", info.Size(), fileSizeLimit)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	uri := PathToURI(path)
	langID := LanguageIDForPath(path)
	if langID == "" {
		langID = "plaintext"
	}

	c.docMu.Lock()
	existing, alreadyOpen := c.openDocs[uri]
	if alreadyOpen {
		c.docMu.Unlock()
		version := existing.Version.Add(1)
		params := DidChangeTextDocumentParams{
			TextDocument: VersionedTextDocumentIdentifier{
				URI:     uri,
				Version: version,
			},
			ContentChanges: []TextDocumentContentChangeEvent{
				{Text: string(content)},
			},
		}
		return c.transport.Notify(ctx, MethodTextDocumentDidChange, params)
	}

	// reserve slot while holding lock to prevent concurrent didOpen
	doc := &openDocument{
		URI:        uri,
		LanguageID: langID,
	}
	doc.Version.Store(1)
	c.openDocs[uri] = doc
	c.docMu.Unlock()

	params := DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: langID,
			Version:    1,
			Text:       string(content),
		},
	}

	if err := c.transport.Notify(ctx, MethodTextDocumentDidOpen, params); err != nil {
		// rollback on failure
		c.docMu.Lock()
		delete(c.openDocs, uri)
		c.docMu.Unlock()
		return fmt.Errorf("didOpen: %w", err)
	}

	return nil
}

func (c *Client) DidChange(ctx context.Context, path string) error {
	uri := PathToURI(path)

	c.docMu.Lock()
	doc, ok := c.openDocs[uri]
	c.docMu.Unlock()

	if !ok {
		return c.DidOpen(ctx, path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	version := doc.Version.Add(1)

	params := DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: string(content)},
		},
	}

	return c.transport.Notify(ctx, MethodTextDocumentDidChange, params)
}

func (c *Client) DidSave(ctx context.Context, path string) error {
	uri := PathToURI(path)

	c.docMu.Lock()
	_, ok := c.openDocs[uri]
	c.docMu.Unlock()

	if !ok {
		return nil
	}

	params := DidSaveTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	return c.transport.Notify(ctx, MethodTextDocumentDidSave, params)
}

func (c *Client) DidClose(ctx context.Context, path string) error {
	uri := PathToURI(path)

	c.docMu.Lock()
	_, ok := c.openDocs[uri]
	if ok {
		delete(c.openDocs, uri)
	}
	c.docMu.Unlock()

	if !ok {
		return nil
	}

	params := DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	return c.transport.Notify(ctx, MethodTextDocumentDidClose, params)
}

func (c *Client) EnsureOpen(ctx context.Context, path string) error {
	uri := PathToURI(path)

	c.docMu.Lock()
	_, ok := c.openDocs[uri]
	c.docMu.Unlock()

	if ok {
		return nil
	}
	return c.DidOpen(ctx, path)
}

func (c *Client) Definition(ctx context.Context, params TextDocumentPositionParams) ([]Location, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodDefinition, params)
	if err != nil {
		return nil, err
	}
	return unmarshalLocationResult(raw)
}

func (c *Client) TypeDefinition(ctx context.Context, params TextDocumentPositionParams) ([]Location, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodTypeDefinition, params)
	if err != nil {
		return nil, err
	}
	return unmarshalLocationResult(raw)
}

func (c *Client) Implementation(ctx context.Context, params TextDocumentPositionParams) ([]Location, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodImplementation, params)
	if err != nil {
		return nil, err
	}
	return unmarshalLocationResult(raw)
}

func (c *Client) References(ctx context.Context, params ReferenceParams) ([]Location, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodReferences, params)
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(raw, &locs); err != nil {
		return nil, fmt.Errorf("unmarshal references: %w", err)
	}
	return locs, nil
}

func (c *Client) Hover(ctx context.Context, params TextDocumentPositionParams) (*Hover, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodHover, params)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var h Hover
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("unmarshal hover: %w", err)
	}
	return &h, nil
}

func (c *Client) DocumentSymbol(ctx context.Context, params DocumentSymbolParams) ([]DocumentSymbol, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodDocumentSymbol, params)
	if err != nil {
		return nil, err
	}

	var syms []DocumentSymbol
	if err := json.Unmarshal(raw, &syms); err != nil {
		return nil, fmt.Errorf("unmarshal document symbols: %w", err)
	}
	return syms, nil
}

func (c *Client) WorkspaceSymbol(ctx context.Context, params WorkspaceSymbolParams) ([]WorkspaceSymbol, error) {
	raw, err := c.transport.Call(ctx, MethodWorkspaceSymbol, params)
	if err != nil {
		return nil, err
	}

	var syms []WorkspaceSymbol
	if err := json.Unmarshal(raw, &syms); err != nil {
		return nil, fmt.Errorf("unmarshal workspace symbols: %w", err)
	}
	return syms, nil
}

func (c *Client) Completion(ctx context.Context, params CompletionParams) (*CompletionList, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodCompletion, params)
	if err != nil {
		return nil, err
	}

	var cl CompletionList
	if err := json.Unmarshal(raw, &cl); err != nil {
		return nil, fmt.Errorf("unmarshal completion: %w", err)
	}
	return &cl, nil
}

func (c *Client) SignatureHelp(ctx context.Context, params TextDocumentPositionParams) (*SignatureHelp, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodSignatureHelp, params)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var sh SignatureHelp
	if err := json.Unmarshal(raw, &sh); err != nil {
		return nil, fmt.Errorf("unmarshal signature help: %w", err)
	}
	return &sh, nil
}

func (c *Client) PrepareRename(ctx context.Context, params TextDocumentPositionParams) (*PrepareRenameResult, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodPrepareRename, params)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var result PrepareRenameResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal prepare rename: %w", err)
	}
	return &result, nil
}

func (c *Client) Rename(ctx context.Context, params RenameParams) (*WorkspaceEdit, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodRename, params)
	if err != nil {
		return nil, err
	}

	var edit WorkspaceEdit
	if err := json.Unmarshal(raw, &edit); err != nil {
		return nil, fmt.Errorf("unmarshal rename: %w", err)
	}
	return &edit, nil
}

func (c *Client) PrepareCallHierarchy(ctx context.Context, params CallHierarchyPrepareParams) ([]CallHierarchyItem, error) {
	path, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("uri to path: %w", err)
	}
	if err := c.EnsureOpen(ctx, path); err != nil {
		return nil, fmt.Errorf("ensure open: %w", err)
	}

	raw, err := c.transport.Call(ctx, MethodPrepareCallHierarchy, params)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var items []CallHierarchyItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("unmarshal call hierarchy items: %w", err)
	}
	return items, nil
}

func (c *Client) IncomingCalls(ctx context.Context, params CallHierarchyIncomingCallsParams) ([]CallHierarchyIncomingCall, error) {
	raw, err := c.transport.Call(ctx, MethodIncomingCalls, params)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var calls []CallHierarchyIncomingCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("unmarshal incoming calls: %w", err)
	}
	return calls, nil
}

func (c *Client) OutgoingCalls(ctx context.Context, params CallHierarchyOutgoingCallsParams) ([]CallHierarchyOutgoingCall, error) {
	raw, err := c.transport.Call(ctx, MethodOutgoingCalls, params)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var calls []CallHierarchyOutgoingCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("unmarshal outgoing calls: %w", err)
	}
	return calls, nil
}

func (c *Client) ServerCapabilities() ServerCapabilities {
	return c.serverCaps
}

// unmarshalLocationResult handles LSP responses that may be Location, []Location, or []LocationLink.
func unmarshalLocationResult(raw json.RawMessage) ([]Location, error) {
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err == nil {
		return locs, nil
	}

	var loc Location
	if err := json.Unmarshal(raw, &loc); err == nil {
		return []Location{loc}, nil
	}

	var links []LocationLink
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, fmt.Errorf("unmarshal location result: %w", err)
	}
	locs = make([]Location, len(links))
	for i, link := range links {
		locs[i] = Location{URI: link.TargetURI, Range: link.TargetSelectionRange}
	}
	return locs, nil
}
