// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// sendRequest sends a JSON-RPC request and returns the framed bytes.
func sendRequest(id int, method string, params interface{}) string {
	p, _ := json.Marshal(params)
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      rawID(id),
		Method:  method,
		Params:  json.RawMessage(p),
	}
	body, _ := json.Marshal(msg)
	return string(body)
}

// sendNotificationBody sends a JSON-RPC notification.
func sendNotificationBody(method string, params interface{}) string {
	p, _ := json.Marshal(params)
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  json.RawMessage(p),
	}
	body, _ := json.Marshal(msg)
	return string(body)
}

func rawID(id int) *json.RawMessage {
	raw := json.RawMessage(fmt.Sprintf("%d", id))
	return &raw
}

// runTranscript sends a sequence of framed messages to the server and
// collects the output. The server runs until it encounters an exit
// notification or the input is exhausted.
func runTranscript(t *testing.T, messages []string) string {
	t.Helper()

	var input bytes.Buffer
	for _, body := range messages {
		fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}

	var output bytes.Buffer
	srv := NewServer(&input, &output)
	err := srv.Run()
	if err != nil {
		t.Fatalf("server.Run() error: %v", err)
	}
	return output.String()
}

func TestServer_InitializeShutdownExit(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
			Capabilities: ClientCapabilities{
				Workspace: WorkspaceClientCapabilities{
					WorkspaceFolders: true,
				},
			},
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	// Should contain initialize response.
	if !strings.Contains(output, `"id":1`) {
		t.Error("expected initialize response with id 1")
	}
	if !strings.Contains(output, `"openClose":true`) {
		t.Error("expected textDocumentSync capability")
	}
	if !strings.Contains(output, `"workspaceFolders"`) {
		t.Error("expected workspace folders capability")
	}

	// Should contain shutdown response.
	if !strings.Contains(output, `"id":2`) {
		t.Error("expected shutdown response with id 2")
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "textDocument/hover", map[string]interface{}{}),
		sendRequest(3, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	if !strings.Contains(output, `"code":-32601`) {
		t.Error("expected method not found error code")
	}
	if !strings.Contains(output, "method not found") {
		t.Error("expected method not found message")
	}
}

func TestServer_RejectBeforeInitialize(t *testing.T) {
	messages := []string{
		sendRequest(1, "textDocument/hover", map[string]interface{}{}),
		sendRequest(2, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(3, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	// First request should get a "not initialized" error.
	if !strings.Contains(output, `"code":-32803`) {
		t.Error("expected server not initialized error code")
	}
	// Initialize should still succeed.
	if !strings.Contains(output, `"id":2`) {
		t.Error("expected initialize response")
	}
}

func TestServer_RejectAfterShutdown(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "shutdown", nil),
		sendRequest(3, "textDocument/hover", map[string]interface{}{}),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	// After shutdown, the hover request should get an error.
	if !strings.Contains(output, `"id":3`) {
		t.Error("expected response for post-shutdown request")
	}
	if !strings.Contains(output, "shutting down") {
		t.Error("expected shutting down error message")
	}
}

func TestServer_CancelRequestAcknowledged(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendNotificationBody("$/cancelRequest", map[string]interface{}{"id": 99}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	// Should not panic or error.
	output := runTranscript(t, messages)
	_ = output
}

func TestServer_InitializeWithRootPath(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootPath: "/workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	if !strings.Contains(output, `"id":1`) {
		t.Error("expected initialize response")
	}
	// Without workspaceFolders support, workspace capability should not be advertised.
	if strings.Contains(output, `"workspaceFolders"`) {
		t.Error("workspace folders capability should not be advertised when client does not support it")
	}
}

func TestServer_InitializeWithWorkspaceFolders(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			Capabilities: ClientCapabilities{
				Workspace: WorkspaceClientCapabilities{
					WorkspaceFolders: true,
				},
			},
			WorkspaceFolders: []WorkspaceFolder{
				{URI: "file:///workspace/a", Name: "a"},
				{URI: "file:///workspace/b", Name: "b"},
			},
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	if !strings.Contains(output, `"workspaceFolders"`) {
		t.Error("expected workspace folders capability")
	}
}

func TestServer_DidOpenAndClose(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendNotificationBody("textDocument/didOpen", DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        "file:///workspace/manifest.yml",
				LanguageID: "yaml",
				Version:    1,
				Text:       "name: test",
			},
		}),
		sendNotificationBody("textDocument/didClose", DidCloseTextDocumentParams{
			TextDocument: TextDocumentIdentifier{
				URI: "file:///workspace/manifest.yml",
			},
		}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	// didClose should publish empty diagnostics for the closed file.
	if !strings.Contains(output, `"diagnostics":[]`) {
		t.Error("expected empty diagnostics on didClose")
	}
	if !strings.Contains(output, `file:///workspace/manifest.yml`) {
		t.Error("expected URI in diagnostics notification")
	}
}

func TestServer_DidSave(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendNotificationBody("textDocument/didSave", DidSaveTextDocumentParams{
			TextDocument: TextDocumentIdentifier{
				URI: "file:///workspace/manifest.yml",
			},
		}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	// Should not panic or error. scheduleValidation will fail to find
	// the package root for the fake path and log a debug message.
	output := runTranscript(t, messages)
	if !strings.Contains(output, `"id":2`) {
		t.Error("expected shutdown response")
	}
}

func TestServer_DidChangeWorkspaceFolders(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
			Capabilities: ClientCapabilities{
				Workspace: WorkspaceClientCapabilities{
					WorkspaceFolders: true,
				},
			},
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendNotificationBody("workspace/didChangeWorkspaceFolders", DidChangeWorkspaceFoldersParams{
			Event: WorkspaceFoldersChangeEvent{
				Added: []WorkspaceFolder{
					{URI: "file:///workspace/new-root", Name: "new-root"},
				},
			},
		}),
		sendNotificationBody("workspace/didChangeWorkspaceFolders", DidChangeWorkspaceFoldersParams{
			Event: WorkspaceFoldersChangeEvent{
				Removed: []WorkspaceFolder{
					{URI: "file:///workspace", Name: "root"},
				},
			},
		}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	// Should not panic or error.
	output := runTranscript(t, messages)
	if !strings.Contains(output, `"id":2`) {
		t.Error("expected shutdown response")
	}
}

func TestServer_DidChange(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendNotificationBody("textDocument/didChange", DidChangeTextDocumentParams{
			TextDocument: VersionedTextDocumentIdentifier{
				URI:     "file:///workspace/manifest.yml",
				Version: 2,
			},
			ContentChanges: []TextDocumentContentChangeEvent{
				{Text: "name: test\nversion: 0.0.1"},
			},
		}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	// Should not panic or error. scheduleValidation will fail to find
	// the package root for the fake path and log a debug message.
	output := runTranscript(t, messages)
	if !strings.Contains(output, `"id":2`) {
		t.Error("expected shutdown response")
	}
}

func TestServer_InitializeAdvertisesChangeSync(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	// The initialize response should advertise change sync (full).
	if !strings.Contains(output, `"change":1`) {
		t.Error("expected change:1 (full sync) in capabilities")
	}
}

func TestServer_DoubleInitialize(t *testing.T) {
	messages := []string{
		sendRequest(1, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendNotificationBody("initialized", struct{}{}),
		sendRequest(2, "initialize", InitializeParams{
			RootURI: "file:///workspace",
		}),
		sendRequest(3, "shutdown", nil),
		sendNotificationBody("exit", nil),
	}

	output := runTranscript(t, messages)

	// Second initialize should return an error.
	if !strings.Contains(output, "already initialized") {
		t.Error("expected already initialized error")
	}
}
