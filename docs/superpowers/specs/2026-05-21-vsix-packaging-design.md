# VSIX Packaging Design Spec

**Date:** 2026-05-21
**Status:** Approved

---

## Overview

Package `vscode-bongo` as a self-contained `.vsix` file that bundles the `bongo-ls` binary for Linux x64. Users install it with one command — no Go toolchain or PATH setup required.

---

## Repository Changes

```
bongoci/
└── vscode-bongo/
    ├── .vsixignore          (new)
    ├── .gitignore           (modify — add bin/)
    ├── bin/                 (gitignored — holds built binary)
    │   └── bongo-ls         (built by npm run build-server, included in .vsix)
    ├── package.json         (modify — add vsce, new scripts)
    └── src/
        └── extension.ts     (modify — bundled binary resolution)
```

---

## `package.json` Changes

Add `vsce` to `devDependencies` and extend `scripts`:

```json
"scripts": {
  "compile": "tsc -p ./",
  "watch": "tsc -watch -p ./",
  "build-server": "cd .. && go build -o vscode-bongo/bin/bongo-ls ./cmd/bongo-ls/",
  "vscode:prepublish": "npm run compile",
  "package": "npm run build-server && vsce package"
},
"devDependencies": {
  "@types/node": "^20.19.1",
  "@types/vscode": "^1.82.0",
  "@vscode/vsce": "^3.0.0",
  "typescript": "^5.3.0"
}
```

Running `npm run package` from inside `vscode-bongo/` produces `bongo-0.0.1.vsix`.

---

## `.vsixignore`

Excludes source files from the package while keeping `bin/` and compiled output:

```
src/
node_modules/
.vscode-test/
tsconfig.json
**/*.map
```

`vsce` includes everything not listed here. `bin/bongo-ls` and `out/extension.js` are included automatically.

---

## `.gitignore` Update

Add `bin/` to `vscode-bongo/.gitignore` so the compiled binary is never committed:

```
node_modules/
out/
.vscode-test/
*.vsix
bin/
```

---

## Binary Resolution in `extension.ts`

`activate()` resolves the server path in this order:

1. `bongo.serverPath` user setting (explicit override)
2. `path.join(context.extensionPath, 'bin', 'bongo-ls')` — bundled binary, checked with `fs.existsSync`
3. `'bongo-ls'` — PATH fallback

```typescript
import * as fs from 'fs';
import * as path from 'path';

export async function activate(context: ExtensionContext): Promise<void> {
    const config = workspace.getConfiguration('bongo');
    let serverPath = config.get<string>('serverPath', '');

    if (!serverPath) {
        const bundled = path.join(context.extensionPath, 'bin', 'bongo-ls');
        serverPath = fs.existsSync(bundled) ? bundled : 'bongo-ls';
    }
    // ... rest of activation unchanged
}
```

---

## Installation

After packaging:

```bash
code --install-extension bongo-0.0.1.vsix
```

Or via the VS Code UI: Extensions → `···` → Install from VSIX.

---

## Out of Scope

- macOS / Windows binaries
- Marketplace publishing
- Automatic binary updates
