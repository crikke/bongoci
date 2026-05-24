# VS Code Extension

The `vscode-bongo/` directory contains a VS Code extension that registers the `bongo` language (`.bongo` file extension) and ships an LSP client that talks to `bongo-ls`.

## What you get

- Syntax highlighting via `syntaxes/bongo.tmLanguage.json`
- Snippets via `snippets/bongo.json`
- Language configuration (comments, brackets) via `language-configuration.json`
- **Diagnostics**: live parse errors as you type, with the `build.bongo:line:col: message` format mapped to LSP `Diagnostic`s
- **Hover**: hovering an `IDENT` shows a short description for the language keywords (`BONGOVER`, `MODULE`, `NAME`, `BASE_IMAGE`, `INCLUDE`, `EXPORT`, `CMD`, `DOCKERFILE`, `INPUT`, `OUTPUT`, `CACHE`, `ENV`)

## Configuration

Single setting:

| Setting | Default | Description |
| --- | --- | --- |
| `bongo.serverPath` | `""` | Path to the `bongo-ls` binary. Leave empty to use the bundled binary; falls back to `$PATH` if no bundled binary is found |

## Build locally

```sh
cd vscode-bongo
npm ci
npm run build-server   # builds ../cmd/bongo-ls → vscode-bongo/bin/bongo-ls
npm run package        # produces a .vsix
```

`npm run package` runs `vsce package`, which bundles the compiled TypeScript and the platform-specific `bongo-ls` binary in `bin/`.

## Prebuilt vsix

Release tags (`v*`) build a vsix per architecture (`linux-x64`, `linux-arm64`) via the release pipeline; see [[Releases]]. Install with:

```sh
code --install-extension vscode-bongo-v0.0.1-linux-x64.vsix
```

## `bongo-ls` protocol notes

`bongo-ls` is a minimal LSP server (`cmd/bongo-ls/`):

- Transport: stdin/stdout with `Content-Length` framing (LSP standard)
- Sync: full document sync (`TextDocumentSyncKindFull`); on every `didOpen` / `didChange` it re-parses with `pkg/manifest/parser`
- Diagnostics: any parse error is converted into a single `Diagnostic` at the reported line/column
- Hover: serves keyword descriptions from a hardcoded map in `cmd/bongo-ls/server.go`
- Shutdown: `exit` notification calls `os.Exit(0)`
