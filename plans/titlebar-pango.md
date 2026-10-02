# PLAN: proper Unicode text in titlebars (pure Go, go-text/typesetting)

## Context

Window titles with non-Latin Unicode (CJK, emoji, Arabic, …) render as tofu
boxes in wimy's titlebars. Root cause: `internal/titlebar/titlebar.go` loads
**one** TTF from a hardcoded path list (`fontCandidates`) via
`golang.org/x/image/font`, falling back to `basicfont.Face7x13`. A single
font has no fallback chain and no complex-script shaping, so any glyph it
doesn't cover becomes a box.

**Decision (user)**: fix it in pure Go if feasible — it is — using
`github.com/go-text/typesetting` (the HarfBuzz port + fontconfig-style font
matching shared by Gio/Fyne/Ebitengine; Unlicense OR BSD-3-Clause, no cgo).
cgo+pango is rejected: unnecessary, and the project keeps its headline
"pure Go, no cgo" property (README, AGENTS.md).

## What the pure-Go stack provides (verified against upstream source/docs)

- `fontscan.FontMap` — scans system font dirs (`UseSystemFonts("")`, disk
  cache after first scan), `SetQuery(Query{Families})`,
  `ResolveFace(rune)` with CSS-style fallback. **= font fallback.**
- `shaping.Segmenter.Split(in, fontMap)` — splits text by bidi direction,
  script, language, face; `shaping.HarfbuzzShaper.Shape` per run.
  **= correct shaping (Arabic/Indic/ligatures) + RTL.**
- `font.Face.GlyphData` returns `GlyphOutline` **or `GlyphBitmap`** (PNG/JPG
  bitmaps — Noto Color Emoji/CBDT works). **= color emoji.**
- `shaping.LineWrapper` + `WrapConfig.WithTruncator` — cluster-aware
  truncation with a "…" run. **= ellipsization.**
- Rasterization onto `image.RGBA`: upstream `github.com/go-text/render`
  (`Renderer.DrawShapedRunAt`) — but it drags in oksvg+rasterx (SVG glyphs,
  unmaintained-ish). Its non-SVG core is ~210 lines → **vendor** it (see
  Approach).

## Approach

Rework **only the text path** of `internal/titlebar`; the package's public
API (`New`, `Render`), the border-frame/interior fill, and the
RGBA→BGRA→`wl_shm` hand-off in `internal/river/deco.go` stay unchanged
(`deco.go`'s dirty-check cache already limits renders to changes).

- `Renderer` gains: `fontMap *fontscan.FontMap`, `segmenter`, `shaper`,
  `rasterizer` (all reused; **not thread-safe — fine, rendering only happens
  on the Wayland dispatch goroutine**, AGENTS.md invariant 3).
- `New(height, colors, border, families)` scans system fonts once
  (`UseSystemFonts("")`); error/empty database → log once, render bars
  without text (nil-face guard) instead of today's `basicfont` fallback.
  `x/image/font`, `basicfont`, `opentype` imports and the `fontCandidates`
  path list are deleted; `golang.org/x/image` stays (`fixed`, `vector`).
- Text pipeline in `Render`: `Input{Text: []rune(title), Size:
  fixed.I((Height-6)*scale)}` → `segmenter.Split` (face per run via fontMap)
  → shape each run → truncate to `maxW` via `LineWrapper` with shaped "…"
  truncator → sort runs by `VisualIndex` → draw each run at the pen with the
  vendored rasterizer; baseline from max `LineBounds.Ascent/Descent`,
  vertically centered as today. Empty title / `maxW<=0` guards stay.
- **Vendored raster glue** (`internal/titlebar/render.go`, ~210 lines,
  license header preserved): copy of go-text/render's `render.go` +
  `bitmap.go` minus SVG support — outline glyphs via
  `golang.org/x/image/vector`, PNG bitmap glyphs (color emoji) via
  `image.Decode` + `x/image/draw.BiLinear` scale, 1-bit bitmaps tinted with
  the fg color. Only new module dependency: `github.com/go-text/typesetting`
  (plus its `golang.org/x/*` deps). Rejected alternative: depending on
  `go-text/render` directly (pulls oksvg/rasterx/typesetting-utils for a
  feature we don't use).
- **Config**: new optional `font` property on the `titlebar` KDL section —
  comma-separated family list, default `"sans-serif"`
  (`titlebar font="Iosevka, sans-serif" …`). `config.Titlebar.Font string`;
  struct-equality diffing in `reload.go` already covers rebuild on change;
  example `config.kdl` documents it.

## Files to modify

- `internal/titlebar/titlebar.go` — replace font loading/drawing; keep API.
- `internal/titlebar/render.go` — NEW: vendored raster glue (attribution).
- `internal/titlebar/titlebar_test.go` — keep pixel tests; add Unicode tests.
- `internal/titlebar/testdata/` — NEW: small test font(s), OFL (see Steps).
- `internal/config/config.go` — `Titlebar.Font` + KDL parsing + default.
- `internal/river/reload.go` — pass families to `titlebar.New`.
- `go.mod`/`go.sum` — add `github.com/go-text/typesetting`.
- `config.kdl`, `README.md` (Unicode titlebars bullet; "no cgo" stays),
  `AGENTS.md` (titlebar line: typesetting instead of x/image/font).

## Reuse

- `fill`, `rgbaToBGRA`, border-frame logic, `ellipsize` semantics — already
  in `titlebar.go`; `deco.go` dirty-cache (`DecoWidth/Scale/Title/Focused`)
  and `shmBuffer` unchanged; `reload.go` renderer-rebuild flow unchanged.
- Test-font trick (hermetic rendering tests): `FontMap.AddFont` from
  `testdata/` instead of `UseSystemFonts` — same pattern as go-text/render's
  own `render_test.go`.

## Steps

- [ ] `go get github.com/go-text/typesetting@latest`; vendor raster glue into
      `internal/titlebar/render.go` with license header.
- [ ] Rework `titlebar.go`: `New(height, colors, border, families []string)`;
      FontMap init with nil-face guard; segment→shape→truncate→draw pipeline;
      delete old font code.
- [ ] Config: `Titlebar.Font` (default `"sans-serif"`), parse `font` prop in
      the `titlebar` section, pass through in `reload.go`, document in
      `config.kdl`.
- [ ] Tests: vendor one small OFL test font into `testdata/` (subset
      NotoSans via pyftsubset ~20KB, or Greybeard-22px.ttf 505KB from
      go-text/render) + `addFontFile` test helper; assert: ASCII draws ink;
      a string mixing covered + uncovered runes splits into runs with
      different faces (fallback machinery); no-fonts render doesn't crash;
      existing pixel tests still pass (frame/interior assertions are
      text-independent — text starts at x=6, test pixel is at x=100).
- [ ] e2e-deco.sh: hermetic fonts (`XDG_DATA_HOME=$RT/data` with a font
      copied in, `XDG_CACHE_HOME=$RT/cache`); spawn `foot --title
      '日本語テスト 🎉'`; assert titlebar commits happen and no errors in
      wimy log (pixel-level Unicode proof lives in unit tests).
- [ ] Docs: README feature bullet, AGENTS.md repo-layout line; keep the
      "no cgo" claims intact (they remain true).

## Verification

- `go build ./...`, `go vet ./...`, `gofmt -l cmd internal` clean.
- `go test ./internal/titlebar/` — new Unicode/fallback tests hermetic (no
  system-font dependency); `go test ./...` green.
- `./e2e-deco.sh` passes incl. the CJK/emoji-title window.
- Manual: `river -c wimy`, open windows with CJK / Arabic (RTL) / emoji
  titles; confirm real glyphs (mixed fonts in one title), correct RTL
  ordering, ellipsized long titles, color emoji on systems with Noto Color
  Emoji.
