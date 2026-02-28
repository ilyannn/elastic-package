// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestExtractFileURI_FilePattern(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	msg := `validation error: file "data_stream/access/fields/base-fields.yml": some error`
	got := extractFileURI("/pkg", msg)
	want := "file:///pkg/data_stream/access/fields/base-fields.yml"
	if got != want {
		t.Errorf("extractFileURI = %q, want %q", got, want)
	}
}

func TestExtractFileURI_FolderPattern(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	msg := `folder [data_stream/access] exceeds size limit`
	got := extractFileURI("/pkg", msg)
	want := "file:///pkg/data_stream/access/manifest.yml"
	if got != want {
		t.Errorf("extractFileURI = %q, want %q", got, want)
	}
}

func TestExtractFileURI_NoMatch(t *testing.T) {
	msg := `some generic error without file reference`
	got := extractFileURI("/pkg", msg)
	if got != "" {
		t.Errorf("expected empty URI, got %q", got)
	}
}

func TestMapErrorsToDiagnostics_PlainError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	err := errors.New("something went wrong")
	diags := mapErrorsToDiagnostics("/pkg", err)

	manifestURI := "file:///pkg/manifest.yml"
	d, ok := diags[manifestURI]
	if !ok {
		t.Fatalf("expected diagnostics for %s", manifestURI)
	}
	if len(d) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(d))
	}
	if d[0].Message != "something went wrong" {
		t.Errorf("unexpected message: %q", d[0].Message)
	}
	if d[0].Source != diagnosticSource {
		t.Errorf("unexpected source: %q", d[0].Source)
	}
	if d[0].Severity != SeverityError {
		t.Errorf("unexpected severity: %d", d[0].Severity)
	}
}

func TestPublishDiagnostics_ClearsStale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	var buf bytes.Buffer
	mw := newMessageWriter(&buf)
	wm := newWorkspaceManager()

	// First publish: two URIs have diagnostics.
	wm.setPreviousDiagURIs("/pkg", map[string]struct{}{})
	diags1 := map[string][]Diagnostic{
		"file:///pkg/a.yml": {{Message: "err a", Source: diagnosticSource, Severity: SeverityError}},
		"file:///pkg/b.yml": {{Message: "err b", Source: diagnosticSource, Severity: SeverityError}},
	}
	publishDiagnostics(mw, wm, "/pkg", diags1)

	// Second publish: only a.yml has diagnostics. b.yml should be cleared.
	buf.Reset()
	diags2 := map[string][]Diagnostic{
		"file:///pkg/a.yml": {{Message: "err a2", Source: diagnosticSource, Severity: SeverityError}},
	}
	publishDiagnostics(mw, wm, "/pkg", diags2)

	output := buf.String()
	// Should contain a publish for a.yml with the error.
	if !strings.Contains(output, "err a2") {
		t.Error("expected diagnostics for a.yml")
	}
	// Should contain a publish for b.yml with empty diagnostics.
	if !strings.Contains(output, `"file:///pkg/b.yml"`) {
		t.Error("expected stale clear for b.yml")
	}
}

func TestClearDiagnosticsForURI(t *testing.T) {
	var buf bytes.Buffer
	mw := newMessageWriter(&buf)

	clearDiagnosticsForURI(mw, "file:///test.yml")

	output := buf.String()
	if !strings.Contains(output, `"diagnostics":[]`) {
		t.Error("expected empty diagnostics array")
	}
	if !strings.Contains(output, `"file:///test.yml"`) {
		t.Error("expected URI in output")
	}
}

func TestZeroRange(t *testing.T) {
	r := zeroRange()
	if r.Start.Line != 0 || r.Start.Character != 0 {
		t.Error("expected start at 0:0")
	}
	if r.End.Line != 0 || r.End.Character != 0 {
		t.Error("expected end at 0:0")
	}
}
