// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// redirectLogger points the package-level logger at buf for the duration
// of the test, then restores it. This avoids racing with scheduler
// goroutines that also call the package logger.
func redirectLogger(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	old := logger
	logger = log.New(buf, "", log.LstdFlags)
	t.Cleanup(func() { logger = old })
}

func TestLogEvent_StructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	redirectLogger(t, &buf)

	logEvent("info", map[string]interface{}{
		"method":   "textDocument/didOpen",
		"uri":      "file:///test.yml",
		"duration": 42,
	})

	output := buf.String()
	if !strings.Contains(output, "level=info") {
		t.Error("expected level=info")
	}
	if !strings.Contains(output, "ts=") {
		t.Error("expected timestamp")
	}
	if !strings.Contains(output, "method=textDocument/didOpen") {
		t.Error("expected method field")
	}
}

func TestLogDebug_IncludesMethod(t *testing.T) {
	var buf bytes.Buffer
	redirectLogger(t, &buf)

	logDebug("test/method", map[string]interface{}{"key": "value"})

	output := buf.String()
	if !strings.Contains(output, "level=debug") {
		t.Error("expected level=debug")
	}
	if !strings.Contains(output, "method=test/method") {
		t.Error("expected method field")
	}
}

func TestLogInfo_NilFields(t *testing.T) {
	var buf bytes.Buffer
	redirectLogger(t, &buf)

	logInfo("test/method", nil)

	output := buf.String()
	if !strings.Contains(output, "level=info") {
		t.Error("expected level=info")
	}
}

func TestLogError_IncludesFields(t *testing.T) {
	var buf bytes.Buffer
	redirectLogger(t, &buf)

	logError("test/error", map[string]interface{}{
		"error": "something broke",
		"uri":   "file:///x.yml",
	})

	output := buf.String()
	if !strings.Contains(output, "level=error") {
		t.Error("expected level=error")
	}
	if !strings.Contains(output, "something broke") {
		t.Error("expected error message")
	}
}
