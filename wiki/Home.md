# bongoci

bongoci is a CI tool that runs builds described by a small DSL (`build.bongo`) on top of [BuildKit](https://github.com/moby/buildkit). Tasks become an LLB graph that buildkitd solves; declared outputs are copied back to the host.

## Pages

- [[Getting Started]] – install, prerequisites, first build
- [[Bongo DSL]] – language reference (`BONGOVER`, `MODULE`, tasks, `INPUT`/`OUTPUT`, `ENV`, `CACHE`)
- [[CLI Reference]] – `ci` binary flags
- [[Architecture]] – how a `build.bongo` becomes an LLB graph and runs
- [[Caching]] – `--cache-from` and registry cache import/export
- [[VS Code Extension]] – `bongo-ls` and the `vscode-bongo` extension
- [[Releases]] – the GitHub Actions release pipeline

## Repository layout

```
cmd/ci/          # the `ci` build runner
cmd/bongo-ls/    # LSP server for .bongo files
pkg/manifest/    # .bongo parser (lexer + recursive descent)
pkg/compiler/    # manifest → BuildKit LLB graph
pkg/runner/      # solves the LLB graph against buildkitd, exports artifacts
pkg/buildenv/    # starts/stops a containerised buildkitd
vscode-bongo/    # the VS Code extension
.github/workflows/release.yml  # tag → build → vsix → release
```
