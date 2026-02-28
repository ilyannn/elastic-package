// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"path/filepath"
	"strings"
	"sync"
)

// workspaceManager tracks workspace roots, open document URIs, and their
// associations with package roots. All methods are safe for concurrent use.
type workspaceManager struct {
	mu sync.Mutex

	// roots is the set of workspace-folder roots (absolute paths).
	roots map[string]struct{}

	// openDocs maps document URI -> absolute file path.
	openDocs map[string]string

	// docPackageRoot maps document URI -> discovered package root (absolute path).
	docPackageRoot map[string]string

	// previousDiagURIs maps package root -> set of URIs that had diagnostics
	// in the previous publish cycle. Used to clear stale diagnostics.
	previousDiagURIs map[string]map[string]struct{}
}

func newWorkspaceManager() *workspaceManager {
	return &workspaceManager{
		roots:            make(map[string]struct{}),
		openDocs:         make(map[string]string),
		docPackageRoot:   make(map[string]string),
		previousDiagURIs: make(map[string]map[string]struct{}),
	}
}

// addRoots registers workspace folders.
func (wm *workspaceManager) addRoots(folders []WorkspaceFolder) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for _, f := range folders {
		path := uriToPath(f.URI)
		wm.roots[filepath.Clean(path)] = struct{}{}
	}
}

// removeRoots unregisters workspace folders and returns URIs whose diagnostics
// should be cleared and package roots that should be garbage-collected.
func (wm *workspaceManager) removeRoots(folders []WorkspaceFolder) (clearURIs []string, removedPkgRoots []string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	removedPaths := make(map[string]struct{})
	for _, f := range folders {
		path := filepath.Clean(uriToPath(f.URI))
		delete(wm.roots, path)
		removedPaths[path] = struct{}{}
	}

	// Find documents and package roots under the removed workspace folders.
	pkgRoots := make(map[string]struct{})
	for uri, docPath := range wm.openDocs {
		for rp := range removedPaths {
			if pathUnder(docPath, rp) {
				clearURIs = append(clearURIs, uri)
				if pr, ok := wm.docPackageRoot[uri]; ok {
					pkgRoots[pr] = struct{}{}
				}
				delete(wm.openDocs, uri)
				delete(wm.docPackageRoot, uri)
				break
			}
		}
	}

	for pr := range pkgRoots {
		removedPkgRoots = append(removedPkgRoots, pr)
		// Also clear stale diag URIs for removed package roots.
		if prev, ok := wm.previousDiagURIs[pr]; ok {
			for u := range prev {
				clearURIs = append(clearURIs, u)
			}
			delete(wm.previousDiagURIs, pr)
		}
	}
	return clearURIs, removedPkgRoots
}

// openDoc registers an opened document and returns its absolute file path.
func (wm *workspaceManager) openDoc(uri string) string {
	path := uriToPath(uri)
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.openDocs[uri] = path
	return path
}

// closeDoc removes a document from tracking. It returns the package root for
// the document (if known) and whether any other open documents still reference
// that package root.
func (wm *workspaceManager) closeDoc(uri string) (packageRoot string, hasOtherDocs bool) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	delete(wm.openDocs, uri)
	pr, ok := wm.docPackageRoot[uri]
	if !ok {
		return "", false
	}
	delete(wm.docPackageRoot, uri)

	for _, otherPR := range wm.docPackageRoot {
		if otherPR == pr {
			return pr, true
		}
	}
	return pr, false
}

// setDocPackageRoot associates a document URI with a package root.
func (wm *workspaceManager) setDocPackageRoot(uri, packageRoot string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.docPackageRoot[uri] = packageRoot
}

// setPreviousDiagURIs records which URIs had diagnostics in the last publish for
// a given package root.
func (wm *workspaceManager) setPreviousDiagURIs(packageRoot string, uris map[string]struct{}) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.previousDiagURIs[packageRoot] = uris
}

// getPreviousDiagURIs returns the URIs that had diagnostics in the last publish
// for a given package root.
func (wm *workspaceManager) getPreviousDiagURIs(packageRoot string) map[string]struct{} {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	prev := wm.previousDiagURIs[packageRoot]
	if prev == nil {
		return nil
	}
	cp := make(map[string]struct{}, len(prev))
	for k, v := range prev {
		cp[k] = v
	}
	return cp
}

// isDocOpen returns whether the given URI is currently open.
func (wm *workspaceManager) isDocOpen(uri string) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	_, ok := wm.openDocs[uri]
	return ok
}

// resolveRoot returns the longest-prefix workspace root that contains the given
// path. If no root matches, returns empty string.
func (wm *workspaceManager) resolveRoot(path string) string {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	cleanPath := filepath.Clean(path)
	best := ""
	for r := range wm.roots {
		if pathUnder(cleanPath, r) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

// pathUnder returns true if child is under (or equal to) parent.
func pathUnder(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}
