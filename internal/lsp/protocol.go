// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// InitializeParams represents the parameters of an initialize request.
type InitializeParams struct {
	ProcessID    *int                `json:"processId"`
	RootURI      string              `json:"rootUri,omitempty"`
	RootPath     string              `json:"rootPath,omitempty"`
	Capabilities ClientCapabilities  `json:"capabilities"`
	WorkspaceFolders []WorkspaceFolder `json:"workspaceFolders,omitempty"`
}

// ClientCapabilities describes the client's capabilities.
type ClientCapabilities struct {
	Workspace WorkspaceClientCapabilities `json:"workspace,omitempty"`
}

// WorkspaceClientCapabilities describes workspace-related client capabilities.
type WorkspaceClientCapabilities struct {
	WorkspaceFolders bool `json:"workspaceFolders,omitempty"`
}

// InitializeResult is returned in response to an initialize request.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities describes what the server can do.
type ServerCapabilities struct {
	TextDocumentSync   *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	Workspace          *ServerWorkspaceCapabilities `json:"workspace,omitempty"`
}

// TextDocumentSyncOptions describes text document sync capabilities.
type TextDocumentSyncOptions struct {
	OpenClose bool `json:"openClose"`
	Save      *SaveOptions `json:"save,omitempty"`
}

// SaveOptions describes save notification options.
type SaveOptions struct {
	IncludeText bool `json:"includeText,omitempty"`
}

// ServerWorkspaceCapabilities describes workspace-related server capabilities.
type ServerWorkspaceCapabilities struct {
	WorkspaceFolders *WorkspaceFoldersServerCapabilities `json:"workspaceFolders,omitempty"`
}

// WorkspaceFoldersServerCapabilities describes the server's workspace folder support.
type WorkspaceFoldersServerCapabilities struct {
	Supported           bool        `json:"supported"`
	ChangeNotifications interface{} `json:"changeNotifications,omitempty"`
}

// WorkspaceFolder represents a workspace folder.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// DidOpenTextDocumentParams is sent when a document is opened.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidSaveTextDocumentParams is sent when a document is saved.
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidCloseTextDocumentParams is sent when a document is closed.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidChangeWorkspaceFoldersParams is sent when workspace folders change.
type DidChangeWorkspaceFoldersParams struct {
	Event WorkspaceFoldersChangeEvent `json:"event"`
}

// WorkspaceFoldersChangeEvent describes a workspace folder change event.
type WorkspaceFoldersChangeEvent struct {
	Added   []WorkspaceFolder `json:"added"`
	Removed []WorkspaceFolder `json:"removed"`
}

// TextDocumentItem describes an opened text document.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentIdentifier identifies a text document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// PublishDiagnosticsParams is sent from server to client to publish diagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Diagnostic represents a diagnostic (error, warning, etc).
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Diagnostic severity constants.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

// Range represents a range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position represents a position in a text document (0-based line and character).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// uriToPath converts a file:// URI to a filesystem path.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	if u.Scheme != "file" {
		return uri
	}
	path := u.Path
	// On Windows, URIs like file:///C:/foo need the leading slash removed.
	if runtime.GOOS == "windows" && len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// pathToURI converts a filesystem path to a file:// URI.
func pathToURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		// Windows absolute path: C:/foo -> /C:/foo
		path = "/" + path
	}
	return "file://" + path
}
