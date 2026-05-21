# VSIX Packaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package the `vscode-bongo` extension as a self-contained `.vsix` file that bundles the `bongo-ls` binary so users can install it with one command.

**Architecture:** Four small, independent changes: update `package.json` to add `@vscode/vsce` and build scripts; create `.vsixignore` to control what lands in the package; add `bin/` to `.gitignore`; update `extension.ts` to resolve the bundled binary before falling back to PATH.

**Tech Stack:** Node.js / npm, `@vscode/vsce ^3.0.0`, TypeScript, Go (for the binary build)

---

## File Map

| File | Change |
|---|---|
| `vscode-bongo/package.json` | Add `@vscode/vsce` devDep; add `build-server`, `vscode:prepublish`, `package` scripts |
| `vscode-bongo/.vsixignore` | Create — excludes source/tooling files from the package |
| `vscode-bongo/.gitignore` | Add `bin/` line |
| `vscode-bongo/src/extension.ts` | Add `fs`/`path` imports; check bundled binary before PATH fallback |

---

### Task 1: Update `package.json` — add vsce and build scripts

**Files:**
- Modify: `vscode-bongo/package.json`

- [ ] **Step 1: Replace the `scripts` block and add `@vscode/vsce` to `devDependencies`**

Open `vscode-bongo/package.json`. The current `scripts` block is:

```json
"scripts": {
    "compile": "tsc -p ./",
    "watch": "tsc -watch -p ./"
}
```

And `devDependencies` is:

```json
"devDependencies": {
    "@types/node": "^20.19.41",
    "@types/vscode": "^1.82.0",
    "typescript": "^5.3.0"
}
```

Replace with:

```json
"scripts": {
    "compile": "tsc -p ./",
    "watch": "tsc -watch -p ./",
    "build-server": "cd .. && mkdir -p vscode-bongo/bin && go build -o vscode-bongo/bin/bongo-ls ./cmd/bongo-ls/",
    "vscode:prepublish": "npm run compile",
    "package": "npm run build-server && vsce package"
},
"devDependencies": {
    "@types/node": "^20.19.41",
    "@types/vscode": "^1.82.0",
    "@vscode/vsce": "^3.0.0",
    "typescript": "^5.3.0"
}
```

Note: `mkdir -p vscode-bongo/bin` is added to `build-server` because `go build -o` requires the output directory to exist, and `bin/` is gitignored so it won't be present on a fresh checkout.

- [ ] **Step 2: Install the new dependency**

```bash
cd vscode-bongo && npm install
```

Expected: resolves and installs `@vscode/vsce`, exits 0. `package-lock.json` is updated.

- [ ] **Step 3: Verify `vsce` is callable**

```bash
cd vscode-bongo && npx vsce --version
```

Expected: prints a version string like `3.x.x`, exits 0.

- [ ] **Step 4: Commit**

```bash
git add vscode-bongo/package.json vscode-bongo/package-lock.json
git commit -m "build(vscode-bongo): add vsce dependency and packaging scripts"
```

---

### Task 2: Create `.vsixignore` and update `.gitignore`

**Files:**
- Create: `vscode-bongo/.vsixignore`
- Modify: `vscode-bongo/.gitignore`

- [ ] **Step 1: Create `.vsixignore`**

Create `vscode-bongo/.vsixignore` with this exact content:

```
src/
node_modules/
.vscode-test/
tsconfig.json
**/*.map
```

`vsce` packages everything **not** listed here. `bin/bongo-ls` and `out/extension.js` are intentionally absent from this list so they are included in the `.vsix`.

- [ ] **Step 2: Add `bin/` to `.gitignore`**

Current `vscode-bongo/.gitignore`:

```
node_modules/
out/
.vscode-test/
*.vsix
```

Add `bin/` as the last line:

```
node_modules/
out/
.vscode-test/
*.vsix
bin/
```

- [ ] **Step 3: Verify `vsce ls` includes the right files**

First build the server binary so `bin/bongo-ls` exists:

```bash
cd vscode-bongo && npm run build-server
```

Then preview what would go into the `.vsix`:

```bash
cd vscode-bongo && npx vsce ls
```

Expected output includes (among others):
```
out/extension.js
bin/bongo-ls
package.json
syntaxes/bongo.tmLanguage.json
snippets/bongo.json
language-configuration.json
```

Expected output does NOT include `src/`, `node_modules/`, `tsconfig.json`, or `.map` files.

- [ ] **Step 4: Commit**

```bash
git add vscode-bongo/.vsixignore vscode-bongo/.gitignore
git commit -m "build(vscode-bongo): add .vsixignore and gitignore bin/"
```

---

### Task 3: Update `extension.ts` — bundled binary resolution

**Files:**
- Modify: `vscode-bongo/src/extension.ts`

- [ ] **Step 1: Update the imports and binary resolution in `activate()`**

Current `vscode-bongo/src/extension.ts`:

```typescript
import { workspace, ExtensionContext, window } from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient;

export async function activate(context: ExtensionContext): Promise<void> {
    const config = workspace.getConfiguration('bongo');
    let serverPath = config.get<string>('serverPath', '');
    if (!serverPath) {
        serverPath = 'bongo-ls';
    }
```

Replace the top of the file so it becomes:

```typescript
import * as fs from 'fs';
import * as path from 'path';
import { workspace, ExtensionContext, window } from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient;

export async function activate(context: ExtensionContext): Promise<void> {
    const config = workspace.getConfiguration('bongo');
    let serverPath = config.get<string>('serverPath', '');

    if (!serverPath) {
        const bundled = path.join(context.extensionPath, 'bin', 'bongo-ls');
        serverPath = fs.existsSync(bundled) ? bundled : 'bongo-ls';
    }
```

The rest of `activate()` and `deactivate()` stay unchanged.

- [ ] **Step 2: Compile and verify no TypeScript errors**

```bash
cd vscode-bongo && npm run compile
```

Expected: exits 0, no errors printed to stderr. `out/extension.js` is updated.

- [ ] **Step 3: Commit**

```bash
git add vscode-bongo/src/extension.ts
git commit -m "feat(vscode-bongo): resolve bundled bongo-ls binary before PATH fallback"
```

---

### Task 4: Build the VSIX and verify

**Files:** (no source changes — verification only)

- [ ] **Step 1: Run the full package command**

```bash
cd vscode-bongo && npm run package
```

This runs `build-server` (compiles the Go binary to `bin/bongo-ls`) then `vsce package` (compiles TypeScript via `vscode:prepublish`, bundles everything into `bongo-0.0.1.vsix`).

Expected: exits 0, last line of output is something like:
```
 DONE  Packaged: .../vscode-bongo/vscode-bongo-0.0.1.vsix (N files, X.XXkB)
```

The filename is derived from `package.json` `"name"` + `"version"`, so with `"name": "vscode-bongo"` the output is `vscode-bongo-0.0.1.vsix`.

- [ ] **Step 2: Verify the binary is inside the archive**

```bash
unzip -l vscode-bongo/vscode-bongo-0.0.1.vsix | grep -E 'bin/bongo-ls|extension/out'
```

Expected output includes both of these entries:
```
extension/bin/bongo-ls
extension/out/extension.js
```

- [ ] **Step 3: Verify source files are excluded**

```bash
unzip -l vscode-bongo/vscode-bongo-0.0.1.vsix | grep -E '^.*src/|node_modules|\.map$'
```

Expected: no output (those files are excluded by `.vsixignore`).

- [ ] **Step 4: Smoke-test installation (optional but recommended)**

```bash
code --install-extension vscode-bongo/vscode-bongo-0.0.1.vsix
```

Open any `.bongo` file. Confirm syntax highlighting is active and hover on a keyword (e.g. `CMD`) shows the markdown tooltip.

- [ ] **Step 5: Commit (if any files changed during build — e.g. `package-lock.json`)**

If `git status` shows changes only from the build artefacts (`bin/`, `*.vsix`) they are gitignored and nothing to commit. If `package-lock.json` changed, commit it:

```bash
git add vscode-bongo/package-lock.json
git commit -m "build(vscode-bongo): update lockfile after vsce install"
```

Otherwise skip this step.
