// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bytes"
	"errors"
	"path/filepath"
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

func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}



func TestExtractFileURI_AbsolutePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	// The spec sometimes reports absolute file paths in error messages.
	msg := `file "/pkg/data_stream/access/manifest.yml" is invalid: some error`
	got := extractFileURI("/pkg", msg)
	want := "file:///pkg/data_stream/access/manifest.yml"
	if got != want {
		t.Errorf("extractFileURI = %q, want %q", got, want)
	}
}

func TestExtractFileURI_AbsoluteFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	// The folder pattern maps to folder/manifest.yml.
	msg := `folder [/pkg/data_stream] exceeds size limit`
	got := extractFileURI("/pkg", msg)
	want := "file:///pkg/data_stream/manifest.yml"
	if got != want {
		t.Errorf("extractFileURI = %q, want %q", got, want)
	}
}

func TestValidatePackage_ValidFixture(t *testing.T) {
	pkg := filepath.Join(testdataDir(t), "valid_package")
	diags := validatePackage(pkg)
	if len(diags) != 0 {
		for uri, dd := range diags {
			for _, d := range dd {
				t.Errorf("unexpected diagnostic for %s: %s", uri, d.Message)
			}
		}
	}
}

func TestValidatePackage_InvalidFixture(t *testing.T) {
	pkg := filepath.Join(testdataDir(t), "invalid_package")
	diags := validatePackage(pkg)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for invalid package")
	}

	// All diagnostics should target the manifest URI.
	manifestURI := pathToURI(filepath.Join(pkg, "manifest.yml"))
	dd, ok := diags[manifestURI]
	if !ok {
		// Print what we got to aid debugging.
		for uri := range diags {
			t.Errorf("got diagnostics for %s, wanted %s", uri, manifestURI)
		}
		t.FailNow()
	}
	if len(dd) == 0 {
		t.Error("expected at least one diagnostic")
	}
	for _, d := range dd {
		if d.Severity != SeverityError {
			t.Errorf("expected SeverityError, got %d", d.Severity)
		}
		if d.Source != diagnosticSource {
			t.Errorf("expected source %q, got %q", diagnosticSource, d.Source)
		}
	}
}

func TestValidatePackage_NonexistentPath(t *testing.T) {
	diags := validatePackage("/nonexistent/path/to/package")
	// A non-existent path should produce at least one diagnostic (a plain error).
	if len(diags) == 0 {
		t.Error("expected diagnostics for non-existent package path")
	}
}

func TestFindPackageRoot_FromManifest(t *testing.T) {
	pkg := filepath.Join(testdataDir(t), "valid_package")
	manifestPath := filepath.Join(pkg, "manifest.yml")
	got, err := findPackageRoot(manifestPath)
	if err != nil {
		t.Fatalf("findPackageRoot error: %v", err)
	}
	if got != pkg {
		t.Errorf("findPackageRoot = %q, want %q", got, pkg)
	}
}

func TestFindPackageRoot_FromDeepFile(t *testing.T) {
	pkg := filepath.Join(testdataDir(t), "valid_package")
	deepFile := filepath.Join(pkg, "data_stream", "logs", "fields", "base-fields.yml")
	got, err := findPackageRoot(deepFile)
	if err != nil {
		t.Fatalf("findPackageRoot error: %v", err)
	}
	if got != pkg {
		t.Errorf("findPackageRoot = %q, want %q", got, pkg)
	}
}

func TestFindPackageRoot_NotFound(t *testing.T) {
	_, err := findPackageRoot("/nonexistent/file.yml")
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

func TestValidatePackage_DanglingDashboardRef(t *testing.T) {
	// This fixture has a dangling tag reference in a dashboard JSON file.
	// The error should map to the specific dashboard file, not the manifest.
	pkg := filepath.Join(testdataDir(t), "invalid_dashboard")
	diags := validatePackage(pkg)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for package with dangling dashboard ref")
	}

	dashboardURI := pathToURI(filepath.Join(pkg, "kibana", "dashboard", "test_invalid_dashboard-overview.json"))
	dd, ok := diags[dashboardURI]
	if !ok {
		for uri := range diags {
			t.Errorf("got diagnostics for %s, wanted %s", uri, dashboardURI)
		}
		t.FailNow()
	}
	if len(dd) != 1 {
		t.Errorf("expected 1 diagnostic, got %d", len(dd))
	}
	if !strings.Contains(dd[0].Message, "dangling reference") {
		t.Errorf("expected dangling reference error, got: %s", dd[0].Message)
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
