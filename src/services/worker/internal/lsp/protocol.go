//go:build desktop

package lsp

import (
	"encoding/json"
	"path/filepath"
)

// LSP method constants.
const (
	MethodInitialize             = "initialize"
	MethodInitialized            = "initialized"
	MethodShutdown               = "shutdown"
	MethodExit                   = "exit"
	MethodTextDocumentDidOpen    = "textDocument/didOpen"
	MethodTextDocumentDidChange  = "textDocument/didChange"
	MethodTextDocumentDidSave    = "textDocument/didSave"
	MethodTextDocumentDidClose   = "textDocument/didClose"
	MethodDefinition             = "textDocument/definition"
	MethodReferences             = "textDocument/references"
	MethodHover                  = "textDocument/hover"
	MethodDocumentSymbol         = "textDocument/documentSymbol"
	MethodWorkspaceSymbol        = "workspace/symbol"
	MethodTypeDefinition         = "textDocument/typeDefinition"
	MethodImplementation         = "textDocument/implementation"
	MethodCompletion             = "textDocument/completion"
	MethodSignatureHelp          = "textDocument/signatureHelp"
	MethodPrepareRename          = "textDocument/prepareRename"
	MethodRename                 = "textDocument/rename"
	MethodPublishDiagnostics     = "textDocument/publishDiagnostics"
	MethodPrepareCallHierarchy   = "textDocument/prepareCallHierarchy"
	MethodIncomingCalls          = "callHierarchy/incomingCalls"
	MethodOutgoingCalls          = "callHierarchy/outgoingCalls"

	ErrContentModified      = -32801
	ErrRequestCancelled     = -32800
	ErrServerNotInitialized = -32002
)

// Position in a text document (0-based).
type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

// Range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// LocationLink represents a link between a source and target location.
type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// Location links a URI to a range.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier identifies a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentPositionParams combines document + position.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ReferenceContext controls whether declarations are included.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams for textDocument/references.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

// DiagnosticSeverity indicates severity.
type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

// Diagnostic represents a compiler/linter diagnostic.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     any                `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams sent from server.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// CallHierarchyItem represents an item in a call hierarchy.
type CallHierarchyItem struct {
	Name           string          `json:"name"`
	Kind           SymbolKind      `json:"kind"`
	Tags           []int           `json:"tags,omitempty"`
	Detail         string          `json:"detail,omitempty"`
	URI            string          `json:"uri"`
	Range          Range           `json:"range"`
	SelectionRange Range           `json:"selectionRange"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// CallHierarchyPrepareParams for textDocument/prepareCallHierarchy.
type CallHierarchyPrepareParams = TextDocumentPositionParams

// CallHierarchyIncomingCallsParams for callHierarchy/incomingCalls.
type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyIncomingCall represents an incoming call.
type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

// CallHierarchyOutgoingCallsParams for callHierarchy/outgoingCalls.
type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyOutgoingCall represents an outgoing call.
type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

// RenameParams for textDocument/rename.
type RenameParams struct {
	TextDocumentPositionParams
	NewName string `json:"newName"`
}

// TextEdit is a change to a document.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// OptionalVersionedTextDocumentIdentifier identifies a specific document version.
type OptionalVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int32 `json:"version"`
}

// TextDocumentEdit groups edits for one document.
type TextDocumentEdit struct {
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                              `json:"edits"`
}

// WorkspaceEdit represents changes across documents.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"`
}

// TextDocumentItem is a full document snapshot.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int32  `json:"version"`
	Text       string `json:"text"`
}

// DidOpenTextDocumentParams for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// VersionedTextDocumentIdentifier includes version.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
}

// TextDocumentContentChangeEvent for full sync.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidChangeTextDocumentParams for textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidSaveTextDocumentParams for textDocument/didSave.
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

// DidCloseTextDocumentParams for textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// MarkupContent for hover/completion docs.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Hover result.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// SymbolKind as defined by LSP.
type SymbolKind int

const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

var symbolKindName = map[SymbolKind]string{
	1: "File", 2: "Module", 3: "Namespace", 4: "Package",
	5: "Class", 6: "Method", 7: "Property", 8: "Field",
	9: "Constructor", 10: "Enum", 11: "Interface", 12: "Function",
	13: "Variable", 14: "Constant", 15: "String", 16: "Number",
	17: "Boolean", 18: "Array", 19: "Object", 20: "Key",
	21: "Null", 22: "EnumMember", 23: "Struct", 24: "Event",
	25: "Operator", 26: "TypeParameter",
}

func (k SymbolKind) String() string {
	if name, ok := symbolKindName[k]; ok {
		return name
	}
	return "Unknown"
}

// DocumentSymbol represents a symbol in a document (hierarchical).
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// WorkspaceSymbol represents a symbol across the workspace.
type WorkspaceSymbol struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	Location Location   `json:"location"`
}

// WorkspaceSymbolParams for workspace/symbol.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// DocumentSymbolParams for textDocument/documentSymbol.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// CompletionParams for textDocument/completion.
type CompletionParams struct {
	TextDocumentPositionParams
}

// CompletionItem represents a completion suggestion.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation any    `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// CompletionList is the response for completion requests.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// ParameterInformation for signature help.
type ParameterInformation struct {
	Label         any `json:"label"`
	Documentation any `json:"documentation,omitempty"`
}

// SignatureInformation describes a callable signature.
type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation any                    `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

// SignatureHelp result.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature *uint32               `json:"activeSignature,omitempty"`
	ActiveParameter *uint32               `json:"activeParameter,omitempty"`
}

// PrepareRenameResult for textDocument/prepareRename.
type PrepareRenameResult struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
}

// WorkspaceFolder represents a workspace root.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// InitializeParams sent by the client.
type InitializeParams struct {
	ProcessID             int                `json:"processId"`
	RootURI               string             `json:"rootUri"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions any                `json:"initializationOptions,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
}

// InitializeResult returned by the server.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ClientCapabilities advertises client support.
type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
	Workspace    *WorkspaceClientCapabilities    `json:"workspace,omitempty"`
	General      *GeneralClientCapabilities      `json:"general,omitempty"`
}

type TextDocumentClientCapabilities struct {
	Synchronization *TextDocumentSyncClientCapabilities `json:"synchronization,omitempty"`
	PublishDiagnostics *PublishDiagnosticsClientCapabilities `json:"publishDiagnostics,omitempty"`
	Hover           *HoverClientCapabilities              `json:"hover,omitempty"`
	Definition      *DefinitionClientCapabilities         `json:"definition,omitempty"`
	TypeDefinition  *TypeDefinitionClientCapabilities     `json:"typeDefinition,omitempty"`
	Implementation  *ImplementationClientCapabilities     `json:"implementation,omitempty"`
	References      *ReferencesClientCapabilities         `json:"references,omitempty"`
	DocumentSymbol  *DocumentSymbolClientCapabilities     `json:"documentSymbol,omitempty"`
	Completion      *CompletionClientCapabilities         `json:"completion,omitempty"`
	SignatureHelp   *SignatureHelpClientCapabilities      `json:"signatureHelp,omitempty"`
	Rename          *RenameClientCapabilities             `json:"rename,omitempty"`
	CallHierarchy   *CallHierarchyClientCapabilities      `json:"callHierarchy,omitempty"`
}

type TextDocumentSyncClientCapabilities struct {
	WillSave          bool `json:"willSave,omitempty"`
	WillSaveWaitUntil bool `json:"willSaveWaitUntil,omitempty"`
	DidSave           bool `json:"didSave,omitempty"`
}

type PublishDiagnosticsClientCapabilities struct {
	RelatedInformation bool  `json:"relatedInformation,omitempty"`
	TagSupport         *struct {
		ValueSet []int `json:"valueSet"`
	} `json:"tagSupport,omitempty"`
}

type HoverClientCapabilities struct {
	ContentFormat []string `json:"contentFormat,omitempty"`
}

type DefinitionClientCapabilities struct{}

type TypeDefinitionClientCapabilities struct{}

type ImplementationClientCapabilities struct{}

type ReferencesClientCapabilities struct{}

type DocumentSymbolClientCapabilities struct {
	HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
}

type CompletionClientCapabilities struct {
	CompletionItem *struct {
		SnippetSupport          bool `json:"snippetSupport,omitempty"`
		CommitCharactersSupport bool `json:"commitCharactersSupport,omitempty"`
	} `json:"completionItem,omitempty"`
}

type SignatureHelpClientCapabilities struct {
	SignatureInformation *struct {
		DocumentationFormat []string `json:"documentationFormat,omitempty"`
	} `json:"signatureInformation,omitempty"`
}

type RenameClientCapabilities struct {
	PrepareSupport bool `json:"prepareSupport,omitempty"`
}

type CallHierarchyClientCapabilities struct{}

type WorkspaceClientCapabilities struct {
	WorkspaceFolders      bool                                     `json:"workspaceFolders,omitempty"`
	DidChangeWatchedFiles *DidChangeWatchedFilesClientCapabilities `json:"didChangeWatchedFiles,omitempty"`
	WorkspaceEdit         *WorkspaceEditClientCapabilities         `json:"workspaceEdit,omitempty"`
}

type DidChangeWatchedFilesClientCapabilities struct{}

type WorkspaceEditClientCapabilities struct {
	DocumentChanges bool `json:"documentChanges,omitempty"`
}

type GeneralClientCapabilities struct {
	PositionEncodings []string `json:"positionEncodings,omitempty"`
}

// ServerCapabilities advertises server support.
type ServerCapabilities struct {
	TextDocumentSync           any  `json:"textDocumentSync,omitempty"`
	HoverProvider              any  `json:"hoverProvider,omitempty"`
	CompletionProvider         any  `json:"completionProvider,omitempty"`
	SignatureHelpProvider      any  `json:"signatureHelpProvider,omitempty"`
	DefinitionProvider         any  `json:"definitionProvider,omitempty"`
	TypeDefinitionProvider     any  `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider     any  `json:"implementationProvider,omitempty"`
	ReferencesProvider         any  `json:"referencesProvider,omitempty"`
	DocumentSymbolProvider     any  `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider    any  `json:"workspaceSymbolProvider,omitempty"`
	RenameProvider             any  `json:"renameProvider,omitempty"`
	CallHierarchyProvider      any  `json:"callHierarchyProvider,omitempty"`
	DocumentFormattingProvider any  `json:"documentFormattingProvider,omitempty"`
	Workspace                  *struct {
		WorkspaceFolders *struct {
			Supported bool `json:"supported,omitempty"`
		} `json:"workspaceFolders,omitempty"`
	} `json:"workspace,omitempty"`
}

// Supports checks if the server advertises support for a given method.
// Handles both nil and explicit false (e.g. "hoverProvider": false).
func (sc ServerCapabilities) Supports(method string) bool {
	return isCapabilityEnabled(sc.capabilityFor(method))
}

func (sc ServerCapabilities) capabilityFor(method string) any {
	switch method {
	case MethodHover:
		return sc.HoverProvider
	case MethodCompletion:
		return sc.CompletionProvider
	case MethodSignatureHelp:
		return sc.SignatureHelpProvider
	case MethodDefinition:
		return sc.DefinitionProvider
	case MethodTypeDefinition:
		return sc.TypeDefinitionProvider
	case MethodImplementation:
		return sc.ImplementationProvider
	case MethodReferences:
		return sc.ReferencesProvider
	case MethodDocumentSymbol:
		return sc.DocumentSymbolProvider
	case MethodWorkspaceSymbol:
		return sc.WorkspaceSymbolProvider
	case MethodRename, MethodPrepareRename:
		return sc.RenameProvider
	case MethodPrepareCallHierarchy, MethodIncomingCalls, MethodOutgoingCalls:
		return sc.CallHierarchyProvider
	default:
		return nil
	}
}

func isCapabilityEnabled(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return true
}

// Language ID mapping from file extension.
var extToLanguageID = map[string]string{
	".go": "go", ".py": "python", ".js": "javascript", ".jsx": "javascriptreact",
	".ts": "typescript", ".tsx": "typescriptreact", ".rs": "rust", ".java": "java",
	".rb": "ruby", ".c": "c", ".cpp": "cpp", ".h": "c", ".hpp": "cpp",
	".cs": "csharp", ".php": "php", ".swift": "swift", ".kt": "kotlin",
	".lua": "lua", ".zig": "zig", ".sh": "shellscript", ".json": "json",
	".yaml": "yaml", ".yml": "yaml", ".toml": "toml", ".html": "html",
	".css": "css", ".vue": "vue", ".svelte": "svelte",
}

// LanguageIDForPath returns the LSP language ID for a file path, or "" if unknown.
func LanguageIDForPath(path string) string {
	ext := filepath.Ext(path)
	return extToLanguageID[ext]
}

// DefaultClientCapabilities returns a fully populated ClientCapabilities.
func DefaultClientCapabilities() ClientCapabilities {
	return ClientCapabilities{
		TextDocument: &TextDocumentClientCapabilities{
			Synchronization: &TextDocumentSyncClientCapabilities{
				WillSave:          true,
				WillSaveWaitUntil: true,
				DidSave:           true,
			},
			PublishDiagnostics: &PublishDiagnosticsClientCapabilities{
				RelatedInformation: true,
				TagSupport: &struct {
					ValueSet []int `json:"valueSet"`
				}{ValueSet: []int{1, 2}},
			},
			Hover: &HoverClientCapabilities{
				ContentFormat: []string{"markdown", "plaintext"},
			},
			Definition:     &DefinitionClientCapabilities{},
			TypeDefinition: &TypeDefinitionClientCapabilities{},
			Implementation: &ImplementationClientCapabilities{},
			References:     &ReferencesClientCapabilities{},
			DocumentSymbol: &DocumentSymbolClientCapabilities{
				HierarchicalDocumentSymbolSupport: true,
			},
			Completion: &CompletionClientCapabilities{
				CompletionItem: &struct {
					SnippetSupport          bool `json:"snippetSupport,omitempty"`
					CommitCharactersSupport bool `json:"commitCharactersSupport,omitempty"`
				}{},
			},
			SignatureHelp: &SignatureHelpClientCapabilities{
				SignatureInformation: &struct {
					DocumentationFormat []string `json:"documentationFormat,omitempty"`
				}{DocumentationFormat: []string{"markdown", "plaintext"}},
			},
			Rename: &RenameClientCapabilities{
				PrepareSupport: true,
			},
			CallHierarchy: &CallHierarchyClientCapabilities{},
		},
		Workspace: &WorkspaceClientCapabilities{
			WorkspaceFolders: true,
			DidChangeWatchedFiles: &DidChangeWatchedFilesClientCapabilities{},
			WorkspaceEdit: &WorkspaceEditClientCapabilities{
				DocumentChanges: true,
			},
		},
		General: &GeneralClientCapabilities{
			PositionEncodings: []string{"utf-16"},
		},
	}
}
