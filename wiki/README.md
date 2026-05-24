# Wiki source

These markdown files are the source-of-truth for the GitHub wiki at <https://github.com/crikke/bongoci/wiki>. They are kept in the main repo so wiki edits go through normal PR review.

## Publishing to GitHub

The GitHub wiki is a separate git repo (`crikke/bongoci.wiki.git`) that only exists once you:

1. Enable wikis on the repo: **Settings → Features → Wikis**
2. Create at least one page through the web UI (this initialises the wiki repo)

After that, sync these files into the wiki repo:

```sh
git clone https://github.com/crikke/bongoci.wiki.git /tmp/bongoci.wiki
cp wiki/*.md /tmp/bongoci.wiki/
cd /tmp/bongoci.wiki && git add -A && git commit -m "Sync from main repo" && git push
```

GitHub wiki page naming rules (relevant for files here):

- `Home.md` is the landing page
- Spaces in titles map to `-` in filenames (e.g. `Getting Started` → `Getting-Started.md`)
- `_Sidebar.md` overrides the right-hand navigation

## File map

| Wiki page | File |
| --- | --- |
| Home | `Home.md` |
| Getting Started | `Getting-Started.md` |
| Bongo DSL | `Bongo-DSL.md` |
| CLI Reference | `CLI-Reference.md` |
| Architecture | `Architecture.md` |
| Caching | `Caching.md` |
| VS Code Extension | `VS-Code-Extension.md` |
| Releases | `Releases.md` |
| Sidebar | `_Sidebar.md` |
