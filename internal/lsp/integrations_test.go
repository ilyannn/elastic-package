// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidatePackage_Integrations runs validatePackage against every package
// in a local clone of elastic/integrations. Set INTEGRATIONS_REPO to the path
// of the cloned repo to enable this test.
//
//	INTEGRATIONS_REPO=/tmp/integrations go test -v -run TestValidatePackage_Integrations ./internal/lsp/...
func TestValidatePackage_Integrations(t *testing.T) {
	repoDir := os.Getenv("INTEGRATIONS_REPO")
	if repoDir == "" {
		t.Skip("set INTEGRATIONS_REPO to enable")
	}

	packagesDir := filepath.Join(repoDir, "packages")
	entries, err := os.ReadDir(packagesDir)
	if err != nil {
		t.Fatalf("reading packages dir: %v", err)
	}

	var (
		totalPkgs    int
		cleanPkgs    int
		errorPkgs    int
		totalDiags   int
		badURIPkgs   []string
		panickedPkgs []string
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkgDir := filepath.Join(packagesDir, entry.Name())
		manifest := filepath.Join(pkgDir, "manifest.yml")
		if _, err := os.Stat(manifest); err != nil {
			continue
		}

		totalPkgs++
		t.Run(entry.Name(), func(t *testing.T) {
			// Catch panics — a panic here means our code has a bug.
			defer func() {
				if r := recover(); r != nil {
					panickedPkgs = append(panickedPkgs, entry.Name())
					t.Errorf("PANIC: %v", r)
				}
			}()

			diags := validatePackage(pkgDir)
			if len(diags) == 0 {
				cleanPkgs++
				return
			}

			errorPkgs++
			for uri, dd := range diags {
				totalDiags += len(dd)

				// Every URI should be a valid file:// URI.
				u, err := url.Parse(uri)
				if err != nil || u.Scheme != "file" {
					badURIPkgs = append(badURIPkgs, entry.Name())
					t.Errorf("bad URI: %q", uri)
					continue
				}

				// The path should not contain doubled segments (the bug we fixed).
				path := u.Path
				if strings.Contains(path, pkgDir+"/"+pkgDir) ||
					strings.Count(path, entry.Name()) > 2 {
					badURIPkgs = append(badURIPkgs, entry.Name())
					t.Errorf("URI has duplicated path segments: %q", uri)
				}

				_ = dd // diagnostics exist, that's expected for some packages
			}
		})
	}

	t.Logf("Results: %d packages tested, %d clean, %d with errors, %d total diagnostics",
		totalPkgs, cleanPkgs, errorPkgs, totalDiags)
	if len(panickedPkgs) > 0 {
		t.Errorf("Panics in: %v", panickedPkgs)
	}
	if len(badURIPkgs) > 0 {
		t.Errorf("Bad URIs in: %v", badURIPkgs)
	}
}
