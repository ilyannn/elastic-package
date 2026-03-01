// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReadMessage_Valid(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	r := bufio.NewReader(strings.NewReader(raw))

	msg, err := readMessage(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Method != "initialize" {
		t.Errorf("expected method 'initialize', got %q", msg.Method)
	}
	if msg.ID == nil {
		t.Error("expected non-nil ID")
	}
}

func TestReadMessage_MissingContentLength(t *testing.T) {
	raw := "\r\n{}"
	r := bufio.NewReader(strings.NewReader(raw))

	_, err := readMessage(r)
	if err == nil {
		t.Fatal("expected error for missing Content-Length")
	}
	if !strings.Contains(err.Error(), "missing Content-Length") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadMessage_InvalidContentLength(t *testing.T) {
	raw := "Content-Length: abc\r\n\r\n{}"
	r := bufio.NewReader(strings.NewReader(raw))

	_, err := readMessage(r)
	if err == nil {
		t.Fatal("expected error for invalid Content-Length")
	}
	if !strings.Contains(err.Error(), "invalid Content-Length") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadMessage_TruncatedBody(t *testing.T) {
	raw := "Content-Length: 100\r\n\r\n{}"
	r := bufio.NewReader(strings.NewReader(raw))

	_, err := readMessage(r)
	if err == nil {
		t.Fatal("expected error for truncated body")
	}
}

func TestReadMessage_BadJSON(t *testing.T) {
	body := `{not json}`
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	r := bufio.NewReader(strings.NewReader(raw))

	_, err := readMessage(r)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
	if !strings.Contains(err.Error(), "decoding body") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadMessage_MultipleHeaders(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"test"}`
	raw := fmt.Sprintf("Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	r := bufio.NewReader(strings.NewReader(raw))

	msg, err := readMessage(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Method != "test" {
		t.Errorf("expected method 'test', got %q", msg.Method)
	}
}

func TestReadMessage_MultipleMessages(t *testing.T) {
	body1 := `{"jsonrpc":"2.0","id":1,"method":"first"}`
	body2 := `{"jsonrpc":"2.0","id":2,"method":"second"}`
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%sContent-Length: %d\r\n\r\n%s",
		len(body1), body1, len(body2), body2)
	r := bufio.NewReader(strings.NewReader(raw))

	msg1, err := readMessage(r)
	if err != nil {
		t.Fatalf("first read error: %v", err)
	}
	if msg1.Method != "first" {
		t.Errorf("expected method 'first', got %q", msg1.Method)
	}

	msg2, err := readMessage(r)
	if err != nil {
		t.Fatalf("second read error: %v", err)
	}
	if msg2.Method != "second" {
		t.Errorf("expected method 'second', got %q", msg2.Method)
	}
}

func TestMessageWriter_SendResponse(t *testing.T) {
	var buf bytes.Buffer
	mw := newMessageWriter(&buf)

	idRaw := json.RawMessage(`1`)
	err := mw.sendResponse(&idRaw, map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Content-Length:") {
		t.Error("expected Content-Length header")
	}
	if !strings.Contains(output, `"hello":"world"`) {
		t.Error("expected result in output")
	}
	if !strings.Contains(output, `"id":1`) {
		t.Error("expected id in output")
	}
}

func TestMessageWriter_SendErrorResponse(t *testing.T) {
	var buf bytes.Buffer
	mw := newMessageWriter(&buf)

	idRaw := json.RawMessage(`1`)
	err := mw.sendErrorResponse(&idRaw, codeMethodNotFound, "method not found")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"code":-32601`) {
		t.Error("expected error code in output")
	}
	if !strings.Contains(output, `"method not found"`) {
		t.Error("expected error message in output")
	}
}

func TestMessageWriter_SendNotification(t *testing.T) {
	var buf bytes.Buffer
	mw := newMessageWriter(&buf)

	err := mw.sendNotification("textDocument/publishDiagnostics", map[string]string{
		"uri": "file:///test.yml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"method":"textDocument/publishDiagnostics"`) {
		t.Error("expected method in notification")
	}
	if !strings.Contains(output, `file:///test.yml`) {
		t.Error("expected URI in notification")
	}
	// Notifications must not have an ID.
	if strings.Contains(output, `"id"`) {
		t.Error("notification should not have id")
	}
}

func TestReadMessage_HeaderCaseInsensitive(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"test"}`
	raw := fmt.Sprintf("content-length: %d\r\n\r\n%s", len(body), body)
	r := bufio.NewReader(strings.NewReader(raw))

	msg, err := readMessage(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Method != "test" {
		t.Errorf("expected method 'test', got %q", msg.Method)
	}
}
