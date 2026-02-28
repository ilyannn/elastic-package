// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elastic/elastic-package/internal/packages"
	"github.com/elastic/elastic-package/internal/validation"

	"github.com/elastic/package-spec/v3/code/go/pkg/specerrors"
)

const diagnosticSource = "elastic-package"

var (
	// filePattern matches: file "some/path.yml"
	filePattern = regexp.MustCompile(`file "([^"]+)"`)
	// folderPattern matches: folder [some/folder]
	folderPattern = regexp.MustCompile(`folder \[([^\]]+)\]`)
)

// validatePackage runs validation on the given package root and returns
// per-URI diagnostics. The key in the returned map is a file:// URI.
func validatePackage(packageRoot string) map[string][]Diagnostic {
	processedErr, _ := validation.ValidateAndFilterFromPath(packageRoot)
	if processedErr == nil {
		return nil
	}
	return mapErrorsToDiagnostics(packageRoot, processedErr)
}

// mapErrorsToDiagnostics converts a validation error (which may be a
// specerrors.ValidationErrors) into per-URI diagnostics.
func mapErrorsToDiagnostics(packageRoot string, err error) map[string][]Diagnostic {
	result := make(map[string][]Diagnostic)
	manifestURI := pathToURI(filepath.Join(packageRoot, packages.PackageManifestFile))

	ve, ok := err.(specerrors.ValidationErrors)
	if !ok {
		// Plain error: report on the package manifest.
		result[manifestURI] = append(result[manifestURI], Diagnostic{
			Range:    zeroRange(),
			Severity: SeverityError,
			Source:   diagnosticSource,
			Message:  err.Error(),
		})
		return result
	}

	for _, e := range ve {
		msg := e.Error()
		uri := extractFileURI(packageRoot, msg)
		if uri == "" {
			uri = manifestURI
		}
		result[uri] = append(result[uri], Diagnostic{
			Range:    zeroRange(),
			Severity: SeverityError,
			Source:   diagnosticSource,
			Message:  msg,
		})
	}
	return result
}

// extractFileURI tries to extract a file URI from an error message using the
// two regex patterns documented in the plan.
func extractFileURI(packageRoot, msg string) string {
	if m := filePattern.FindStringSubmatch(msg); len(m) > 1 {
		relPath := m[1]
		absPath := filepath.Join(packageRoot, filepath.FromSlash(relPath))
		return pathToURI(absPath)
	}
	if m := folderPattern.FindStringSubmatch(msg); len(m) > 1 {
		folder := m[1]
		// Map to the folder's manifest if it exists, otherwise fall back to
		// the package manifest.
		candidate := filepath.Join(packageRoot, filepath.FromSlash(folder), packages.PackageManifestFile)
		return pathToURI(candidate)
	}
	return ""
}

// zeroRange returns a Range pointing at the start of the file (line 0, char 0).
func zeroRange() Range {
	return Range{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 0, Character: 0},
	}
}

// publishDiagnostics publishes per-URI diagnostics and clears stale ones.
func publishDiagnostics(writer *messageWriter, wm *workspaceManager, packageRoot string, diags map[string][]Diagnostic) {
	previous := wm.getPreviousDiagURIs(packageRoot)

	currentURIs := make(map[string]struct{})
	for uri, d := range diags {
		currentURIs[uri] = struct{}{}
		if err := writer.sendNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: d,
		}); err != nil {
			logError("publishDiagnostics", map[string]interface{}{
				"error": err.Error(),
				"uri":   uri,
			})
		}
	}

	// Clear diagnostics for URIs that no longer have errors.
	for uri := range previous {
		if _, ok := currentURIs[uri]; !ok {
			if err := writer.sendNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
				URI:         uri,
				Diagnostics: []Diagnostic{},
			}); err != nil {
				logError("publishDiagnostics", map[string]interface{}{
					"error": err.Error(),
					"uri":   uri,
				})
			}
		}
	}

	wm.setPreviousDiagURIs(packageRoot, currentURIs)

	count := 0
	for _, d := range diags {
		count += len(d)
	}
	logInfo("publishDiagnostics", map[string]interface{}{
		"package_root":     packageRoot,
		"diagnostics_count": count,
		"uris_with_errors": len(diags),
	})
}

// clearDiagnosticsForURI sends an empty diagnostics array for the given URI.
func clearDiagnosticsForURI(writer *messageWriter, uri string) {
	if err := writer.sendNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []Diagnostic{},
	}); err != nil {
		logError("clearDiagnostics", map[string]interface{}{
			"error": err.Error(),
			"uri":   uri,
		})
	}
}

// findPackageRoot discovers the package root for a file path by walking up
// the directory tree looking for manifest.yml.
func findPackageRoot(filePath string) (string, error) {
	dir := filepath.Dir(filePath)
	return packages.FindPackageRootFrom(dir)
}

// splitValidationErrors splits an error string by newlines for individual
// error messages. This is used as a fallback when the error is not a
// ValidationErrors type.
func splitValidationErrors(errMsg string) []string {
	var msgs []string
	for _, line := range strings.Split(errMsg, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			msgs = append(msgs, line)
		}
	}
	return msgs
}
