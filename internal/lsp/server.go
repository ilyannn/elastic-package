// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

// Server is the LSP server that communicates over JSON-RPC on stdin/stdout.
type Server struct {
	reader    *bufio.Reader
	writer    *messageWriter
	workspace *workspaceManager
	scheduler *scheduler

	initialized bool
	shutdown    bool

	// supportsWorkspaceFolders is set from client capabilities during initialize.
	supportsWorkspaceFolders bool
}

// NewServer creates a new LSP server reading from r and writing to w.
func NewServer(r io.Reader, w io.Writer) *Server {
	return &Server{
		reader:    bufio.NewReader(r),
		writer:    newMessageWriter(w),
		workspace: newWorkspaceManager(),
	}
}

// Run starts the server event loop. It reads JSON-RPC messages from stdin and
// dispatches them until exit or an unrecoverable error.
func (s *Server) Run() error {
	logInfo("server", map[string]interface{}{"event": "started"})

	for {
		msg, err := readMessage(s.reader)
		if err != nil {
			if s.shutdown {
				return nil
			}
			return fmt.Errorf("reading message: %w", err)
		}

		if err := s.dispatch(msg); err != nil {
			if err == errExit {
				return nil
			}
			return err
		}
	}
}

var errExit = fmt.Errorf("exit")

func (s *Server) dispatch(msg *jsonrpcMessage) error {
	// Panic recovery at dispatch boundary.
	defer func() {
		if r := recover(); r != nil {
			logError("dispatch", map[string]interface{}{
				"event":  "panic-recovered",
				"method": msg.Method,
				"panic":  fmt.Sprintf("%v", r),
			})
			if msg.ID != nil {
				_ = s.writer.sendErrorResponse(msg.ID, codeInternalError, "internal error")
			}
		}
	}()

	isRequest := msg.ID != nil
	method := msg.Method

	logDebug("dispatch", map[string]interface{}{
		"method":     method,
		"is_request": isRequest,
	})

	// If shutdown has been received, reject all requests except exit.
	if s.shutdown && method != "exit" {
		if isRequest {
			return s.writer.sendErrorResponse(msg.ID, codeInvalidRequest, "server is shutting down")
		}
		return nil
	}

	// Before initialization, only allow initialize and exit.
	if !s.initialized && method != "initialize" && method != "exit" {
		if isRequest {
			return s.writer.sendErrorResponse(msg.ID, codeRequestFailed, "server not initialized")
		}
		return nil
	}

	switch method {
	case "initialize":
		return s.handleInitialize(msg)
	case "initialized":
		return s.handleInitialized(msg)
	case "shutdown":
		return s.handleShutdown(msg)
	case "exit":
		return s.handleExit()
	case "$/cancelRequest":
		// Acknowledge; no-op in Phase 1.
		logDebug("$/cancelRequest", nil)
		return nil
	case "textDocument/didOpen":
		return s.handleDidOpen(msg)
	case "textDocument/didSave":
		return s.handleDidSave(msg)
	case "textDocument/didClose":
		return s.handleDidClose(msg)
	case "workspace/didChangeWorkspaceFolders":
		return s.handleDidChangeWorkspaceFolders(msg)
	default:
		logDebug("dispatch", map[string]interface{}{
			"event":  "unknown-method",
			"method": method,
		})
		if isRequest {
			return s.writer.sendErrorResponse(msg.ID, codeMethodNotFound, "method not found: "+method)
		}
		return nil
	}
}

func (s *Server) handleInitialize(msg *jsonrpcMessage) error {
	if s.initialized {
		return s.writer.sendErrorResponse(msg.ID, codeInvalidRequest, "already initialized")
	}

	var params InitializeParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.writer.sendErrorResponse(msg.ID, codeParseError, "invalid initialize params")
	}

	// Record client capabilities.
	s.supportsWorkspaceFolders = params.Capabilities.Workspace.WorkspaceFolders

	// Register workspace folders from initialize params.
	if len(params.WorkspaceFolders) > 0 {
		s.workspace.addRoots(params.WorkspaceFolders)
	} else if params.RootURI != "" {
		s.workspace.addRoots([]WorkspaceFolder{{URI: params.RootURI, Name: "root"}})
	} else if params.RootPath != "" {
		s.workspace.addRoots([]WorkspaceFolder{{URI: pathToURI(params.RootPath), Name: "root"}})
	}

	// Start the scheduler.
	s.scheduler = newScheduler()

	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				OpenClose: true,
				Save:      &SaveOptions{IncludeText: false},
			},
		},
	}

	// Advertise workspace folder support only if the client supports it.
	if s.supportsWorkspaceFolders {
		result.Capabilities.Workspace = &ServerWorkspaceCapabilities{
			WorkspaceFolders: &WorkspaceFoldersServerCapabilities{
				Supported:           true,
				ChangeNotifications: true,
			},
		}
	}

	s.initialized = true

	logInfo("initialize", map[string]interface{}{
		"workspace_folders_support": s.supportsWorkspaceFolders,
	})

	return s.writer.sendResponse(msg.ID, result)
}

func (s *Server) handleInitialized(msg *jsonrpcMessage) error {
	logInfo("initialized", nil)
	return nil
}

func (s *Server) handleShutdown(msg *jsonrpcMessage) error {
	logInfo("shutdown", nil)
	s.shutdown = true
	if s.scheduler != nil {
		s.scheduler.stop()
	}
	return s.writer.sendResponse(msg.ID, nil)
}

func (s *Server) handleExit() error {
	logInfo("exit", nil)
	if s.scheduler != nil {
		s.scheduler.stop()
	}
	return errExit
}

func (s *Server) handleDidOpen(msg *jsonrpcMessage) error {
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		logError("textDocument/didOpen", map[string]interface{}{"error": err.Error()})
		return nil
	}

	uri := params.TextDocument.URI
	filePath := s.workspace.openDoc(uri)

	logDebug("textDocument/didOpen", map[string]interface{}{
		"uri":  uri,
		"path": filePath,
	})

	s.scheduleValidation(uri, filePath)
	return nil
}

func (s *Server) handleDidSave(msg *jsonrpcMessage) error {
	var params DidSaveTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		logError("textDocument/didSave", map[string]interface{}{"error": err.Error()})
		return nil
	}

	uri := params.TextDocument.URI
	filePath := uriToPath(uri)

	logDebug("textDocument/didSave", map[string]interface{}{
		"uri":  uri,
		"path": filePath,
	})

	s.scheduleValidation(uri, filePath)
	return nil
}

func (s *Server) handleDidClose(msg *jsonrpcMessage) error {
	var params DidCloseTextDocumentParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		logError("textDocument/didClose", map[string]interface{}{"error": err.Error()})
		return nil
	}

	uri := params.TextDocument.URI

	logDebug("textDocument/didClose", map[string]interface{}{
		"uri": uri,
	})

	// Clear diagnostics for the closed document.
	clearDiagnosticsForURI(s.writer, uri)

	// Remove from tracking. If no more files reference the package root,
	// the root association is dropped.
	_, _ = s.workspace.closeDoc(uri)
	return nil
}

func (s *Server) handleDidChangeWorkspaceFolders(msg *jsonrpcMessage) error {
	var params DidChangeWorkspaceFoldersParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		logError("workspace/didChangeWorkspaceFolders", map[string]interface{}{"error": err.Error()})
		return nil
	}

	if len(params.Event.Added) > 0 {
		s.workspace.addRoots(params.Event.Added)
		logInfo("workspace/didChangeWorkspaceFolders", map[string]interface{}{
			"event": "added",
			"count": len(params.Event.Added),
		})
	}

	if len(params.Event.Removed) > 0 {
		if s.scheduler != nil {
			for _, f := range params.Event.Removed {
				s.scheduler.cancelWorkspaceRoot(filepath.Clean(uriToPath(f.URI)))
			}
		}
		clearURIs, _ := s.workspace.removeRoots(params.Event.Removed)
		for _, uri := range clearURIs {
			clearDiagnosticsForURI(s.writer, uri)
		}
		logInfo("workspace/didChangeWorkspaceFolders", map[string]interface{}{
			"event":        "removed",
			"count":        len(params.Event.Removed),
			"cleared_uris": len(clearURIs),
		})
	}

	return nil
}

// scheduleValidation finds the package root for a file and schedules a
// validation job through the scheduler.
func (s *Server) scheduleValidation(uri, filePath string) {
	wsRoot := s.workspace.resolveRoot(filePath)

	pkgRoot, err := findPackageRoot(filePath)
	if err != nil {
		logDebug("scheduleValidation", map[string]interface{}{
			"event": "package-root-not-found",
			"uri":   uri,
			"error": err.Error(),
		})
		return
	}

	// If the file belongs to a known workspace root, never allow validation
	// to escape that root boundary.
	if wsRoot != "" && !pathUnder(pkgRoot, wsRoot) {
		logWarn("scheduleValidation", map[string]interface{}{
			"event":          "package-root-outside-workspace-root",
			"uri":            uri,
			"workspace_root": wsRoot,
			"package_root":   pkgRoot,
		})
		return
	}

	// If no workspace root matches, lazily register the discovered package root.
	if wsRoot == "" {
		if s.workspace.ensureRoot(pkgRoot) {
			wsRoot = pkgRoot
			logInfo("scheduleValidation", map[string]interface{}{
				"event":          "lazy-root-added",
				"workspace_root": wsRoot,
			})
		}
	}

	s.workspace.setDocPackageRoot(uri, pkgRoot)

	writer := s.writer
	wm := s.workspace

	s.scheduler.schedule(wsRoot, pkgRoot, uri, func(ctx context.Context) {
		start := time.Now()

		diags := validatePackage(pkgRoot)
		if diags == nil {
			diags = make(map[string][]Diagnostic)
		}

		// Check if this job was cancelled before publishing.
		select {
		case <-ctx.Done():
			logDebug("scheduleValidation", map[string]interface{}{
				"event":        "publish-skipped-stale",
				"package_root": pkgRoot,
				"duration_ms":  time.Since(start).Milliseconds(),
			})
			return
		default:
		}

		publishDiagnostics(writer, wm, pkgRoot, diags)
		logDebug("scheduleValidation", map[string]interface{}{
			"event":        "validation-complete",
			"package_root": pkgRoot,
			"duration_ms":  time.Since(start).Milliseconds(),
		})
	})
}
