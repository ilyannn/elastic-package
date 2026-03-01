// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package cmd

import (
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/elastic/elastic-package/internal/cobraext"
	"github.com/elastic/elastic-package/internal/lsp"
)

const lspLongDescription = `Use this command to start an LSP server for Elastic Integration packages.

The server communicates over stdin/stdout using the Language Server Protocol and
provides diagnostics based on package-spec validation.

Editor setup (point your LSP client at "elastic-package lsp" as a stdio server):

  VS Code:      Use an LSP client extension with serverCommand "elastic-package lsp"
  Neovim:       Add a custom lspconfig entry with cmd = { "elastic-package", "lsp" }
  Sublime Text: Add an LSP client with command ["elastic-package", "lsp"]
  Emacs:        Add ("elastic-package" "lsp") to eglot-server-programs
  Helix:        Add [language-server.elastic-package] to languages.toml

For detailed setup instructions for each editor, see the HOWTO guide (./docs/howto/setup_lsp.md).`

func setupLspCommand() *cobraext.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Start the LSP server",
		Long:  lspLongDescription,
		Args:  cobra.NoArgs,
		RunE:  lspCommandAction,
	}

	return cobraext.NewCommand(cmd, cobraext.ContextGlobal)
}

func lspCommandAction(cmd *cobra.Command, args []string) error {
	// Safety guard: ensure all log output goes to stderr so stdout remains
	// clean for JSON-RPC communication.
	log.SetOutput(os.Stderr)

	srv := lsp.NewServer(os.Stdin, os.Stdout)
	return srv.Run()
}
