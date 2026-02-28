// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"runtime"
	"testing"
)

func TestURIToPath_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-specific test")
	}

	tests := []struct {
		uri  string
		want string
	}{
		{"file:///home/user/test.yml", "/home/user/test.yml"},
		{"file:///tmp/foo/bar.json", "/tmp/foo/bar.json"},
		{"file:///", "/"},
	}

	for _, tt := range tests {
		got := uriToPath(tt.uri)
		if got != tt.want {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestPathToURI_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-specific test")
	}

	tests := []struct {
		path string
		want string
	}{
		{"/home/user/test.yml", "file:///home/user/test.yml"},
		{"/tmp/foo/bar.json", "file:///tmp/foo/bar.json"},
	}

	for _, tt := range tests {
		got := pathToURI(tt.path)
		if got != tt.want {
			t.Errorf("pathToURI(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestURIToPathAndBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-specific test")
	}

	paths := []string{
		"/home/user/packages/manifest.yml",
		"/tmp/elastic/package.yml",
	}

	for _, path := range paths {
		uri := pathToURI(path)
		roundTrip := uriToPath(uri)
		if roundTrip != path {
			t.Errorf("round-trip failed: %q -> %q -> %q", path, uri, roundTrip)
		}
	}
}

func TestURIToPath_NonFileScheme(t *testing.T) {
	got := uriToPath("https://example.com/foo")
	if got != "https://example.com/foo" {
		t.Errorf("expected passthrough for non-file URI, got %q", got)
	}
}
