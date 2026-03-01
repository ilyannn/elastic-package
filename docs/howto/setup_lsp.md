# HOWTO: Set up the LSP server

## Introduction

`elastic-package lsp` starts a Language Server Protocol (LSP) server that provides
real-time validation diagnostics for Elastic Integration packages. The server
communicates over stdin/stdout using JSON-RPC and works with any editor that
supports the LSP.

## Prerequisites

Install `elastic-package` and make sure it is on your `PATH`:

```shell
go install github.com/elastic/elastic-package@latest
```

Or build from source:

```shell
git clone https://github.com/elastic/elastic-package.git
cd elastic-package
just build && just install
```

## VS Code

Add the following to `.vscode/settings.json` in your workspace (requires the
[vscode-languageclient](https://github.com/AnyStatus/vscode-languageserver) or
a generic LSP client extension such as
[lsp-client](https://marketplace.visualstudio.com/items?itemName=nicolo-ribaudo.lsp-client)):

```jsonc
{
  "lsp-client.serverCommand": "elastic-package lsp",
  "lsp-client.documentSelector": [
    { "language": "yaml" },
    { "language": "json" }
  ]
}
```

If you are using a different LSP client extension, configure it to run
`elastic-package lsp` as a stdio-based language server for YAML and JSON files.

## Neovim (nvim-lspconfig)

Add a custom server configuration in your Neovim config:

```lua
local lspconfig = require("lspconfig")
local configs = require("lspconfig.configs")

if not configs.elastic_package then
  configs.elastic_package = {
    default_config = {
      cmd = { "elastic-package", "lsp" },
      filetypes = { "yaml", "json" },
      root_dir = lspconfig.util.root_pattern("manifest.yml"),
    },
  }
end

lspconfig.elastic_package.setup({})
```

## Sublime Text (LSP package)

Install the [LSP](https://packagecontrol.io/packages/LSP) package, then add a
custom client in `LSP.sublime-settings`:

```json
{
  "clients": {
    "elastic-package": {
      "enabled": true,
      "command": ["elastic-package", "lsp"],
      "selector": "source.yaml | source.json"
    }
  }
}
```

## Emacs (eglot)

Add the server to `eglot-server-programs`:

```elisp
(add-to-list 'eglot-server-programs
             '((yaml-mode json-mode) . ("elastic-package" "lsp")))
```

Then run `M-x eglot` in a YAML or JSON buffer inside an integration package.

## Helix

Add to `~/.config/helix/languages.toml`:

```toml
[language-server.elastic-package]
command = "elastic-package"
args = ["lsp"]

[[language]]
name = "yaml"
language-servers = ["yaml-language-server", "elastic-package"]

[[language]]
name = "json"
language-servers = ["json-language-server", "elastic-package"]
```

## Verifying the setup

Open any YAML or JSON file inside an Elastic Integration package directory
(a directory containing a `manifest.yml` with `format_version`). Introduce a
deliberate error — for example, remove the `title` field from `manifest.yml` —
and save the file. You should see a diagnostic appear in your editor's
problems/diagnostics panel with source `elastic-package`.

## Troubleshooting

The LSP server writes debug logs to stderr. To see them, check your editor's
LSP log output or start the server manually:

```shell
elastic-package lsp 2>lsp-debug.log
```

Then interact with the server by sending LSP messages on stdin (or point your
editor at the log file to inspect behavior).
