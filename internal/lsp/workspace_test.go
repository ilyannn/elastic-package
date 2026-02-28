// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"runtime"
	"testing"
)

func TestWorkspaceManager_AddAndResolveRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	wm := newWorkspaceManager()
	wm.addRoots([]WorkspaceFolder{
		{URI: "file:///repo", Name: "repo"},
		{URI: "file:///repo/sub", Name: "sub"},
	})

	// /repo/sub/file.yml should resolve to /repo/sub (longest prefix).
	got := wm.resolveRoot("/repo/sub/file.yml")
	if got != "/repo/sub" {
		t.Errorf("resolveRoot = %q, want /repo/sub", got)
	}

	// /repo/other/file.yml should resolve to /repo.
	got = wm.resolveRoot("/repo/other/file.yml")
	if got != "/repo" {
		t.Errorf("resolveRoot = %q, want /repo", got)
	}

	// /elsewhere/file.yml should resolve to empty.
	got = wm.resolveRoot("/elsewhere/file.yml")
	if got != "" {
		t.Errorf("resolveRoot = %q, want empty", got)
	}
}

func TestWorkspaceManager_OpenCloseDoc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	wm := newWorkspaceManager()

	uri := "file:///repo/pkg/manifest.yml"
	path := wm.openDoc(uri)
	if path != "/repo/pkg/manifest.yml" {
		t.Errorf("openDoc returned %q", path)
	}

	if !wm.isDocOpen(uri) {
		t.Error("expected doc to be open")
	}

	wm.setDocPackageRoot(uri, "/repo/pkg")

	pkgRoot, hasOther := wm.closeDoc(uri)
	if pkgRoot != "/repo/pkg" {
		t.Errorf("closeDoc pkgRoot = %q, want /repo/pkg", pkgRoot)
	}
	if hasOther {
		t.Error("expected no other docs")
	}

	if wm.isDocOpen(uri) {
		t.Error("expected doc to be closed")
	}
}

func TestWorkspaceManager_CloseDoc_OtherDocsRemain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	wm := newWorkspaceManager()

	uri1 := "file:///repo/pkg/a.yml"
	uri2 := "file:///repo/pkg/b.yml"

	wm.openDoc(uri1)
	wm.openDoc(uri2)
	wm.setDocPackageRoot(uri1, "/repo/pkg")
	wm.setDocPackageRoot(uri2, "/repo/pkg")

	pkgRoot, hasOther := wm.closeDoc(uri1)
	if pkgRoot != "/repo/pkg" {
		t.Errorf("closeDoc pkgRoot = %q", pkgRoot)
	}
	if !hasOther {
		t.Error("expected other docs to remain")
	}
}

func TestWorkspaceManager_RemoveRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}

	wm := newWorkspaceManager()
	wm.addRoots([]WorkspaceFolder{
		{URI: "file:///repo", Name: "repo"},
		{URI: "file:///other", Name: "other"},
	})

	uri1 := "file:///repo/pkg/a.yml"
	wm.openDoc(uri1)
	wm.setDocPackageRoot(uri1, "/repo/pkg")

	// Set previous diagnostics.
	wm.setPreviousDiagURIs("/repo/pkg", map[string]struct{}{
		uri1: {},
		"file:///repo/pkg/b.yml": {},
	})

	clearURIs, removedPkgRoots := wm.removeRoots([]WorkspaceFolder{
		{URI: "file:///repo", Name: "repo"},
	})

	if len(clearURIs) == 0 {
		t.Error("expected some URIs to clear")
	}

	if len(removedPkgRoots) == 0 {
		t.Error("expected removed package roots")
	}

	// The /repo root should be gone.
	got := wm.resolveRoot("/repo/pkg/a.yml")
	if got != "" {
		t.Errorf("expected empty root after removal, got %q", got)
	}

	// The /other root should still be there.
	got = wm.resolveRoot("/other/file.yml")
	if got != "/other" {
		t.Errorf("expected /other root, got %q", got)
	}
}

func TestWorkspaceManager_PreviousDiagURIs(t *testing.T) {
	wm := newWorkspaceManager()

	uris := map[string]struct{}{
		"file:///a.yml": {},
		"file:///b.yml": {},
	}
	wm.setPreviousDiagURIs("/pkg", uris)

	got := wm.getPreviousDiagURIs("/pkg")
	if len(got) != 2 {
		t.Errorf("expected 2 URIs, got %d", len(got))
	}

	// Mutating the returned map should not affect internal state.
	delete(got, "file:///a.yml")
	got2 := wm.getPreviousDiagURIs("/pkg")
	if len(got2) != 2 {
		t.Error("internal state was mutated")
	}
}

func TestPathUnder(t *testing.T) {
	tests := []struct {
		child  string
		parent string
		want   bool
	}{
		{"/repo/sub/file.yml", "/repo", true},
		{"/repo", "/repo", true},
		{"/other/file.yml", "/repo", false},
		{"/repoextra/file.yml", "/repo", false},
	}

	for _, tt := range tests {
		got := pathUnder(tt.child, tt.parent)
		if got != tt.want {
			t.Errorf("pathUnder(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}
