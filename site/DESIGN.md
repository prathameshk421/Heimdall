# Heimdall site: design brief (v6, mood v7)

The founders said v5 looks generic and AI-made. They want a cleaner logo, a smoother feel and a stronger identity. They
like the Thinking Machines pages. This brief records what we measured there, what we take, and what we leave.

## 1. What we measured

Source: `https://thinkingmachines.ai/news/introducing-inkling/` and `https://thinkingmachines.ai/`, read on 21 November
2026 in headless Chromium 1223 at 1440 x 900, light and dark color scheme. Values are computed styles.

| Element | Measured value |
|---|---|
| Page background | `#ffffff`. The dark color scheme gives the same white page: the site has no dark mode |
| Body text | Iowan Old Style (serif), 17 px, line height 27.2 px (1.6), weight 400, color `#282828` |
| Text column | 660 px wide, centered. That is about 75 characters a line |
| Title (h1) | Same serif, 30 px, weight 600, line height 1.4, max width 600 px, centered. "Name: descriptor" form |
| Date | Sans (GT America), 15 px, gray, centered under the title |
| Section heads | h2 serif 23.2 px weight 600, 49.6 px above, 19.2 px below. h3 serif 21.6 px weight 400. Each head has a `#` anchor |
| UI text | GT America (sans), 13 to 15 px: nav, table of contents, captions, tables, buttons |
| Code | GT America Mono 13 px on a near-black panel with a 1 px border |
| Links in text | Gray `#676767` with an underline at 30% opacity. Color and underline fade in 0.3 s |
| Buttons | Pill, radius 100 px, 15 px sans. One gray-filled primary ("Try on Tinker"), then plain text links with a `›` chevron |
| Table of contents | Left column, sticky, 13 px sans, `#504945`. The active item gets a dot marker. 11.2 px between items |
| Hero figure | One black-on-cream line illustration on a square grid, in a cream panel (`#fdf7ee`-like), 660 px wide |
| Tables | 13 px sans, cells padded 8 px, no vertical rules. Group headers in gray ("Open weights", "Closed weights"). The own model's column has a rounded outline and a light fill. Missing values show "–" |
| Charts | Radar and bar charts in SVG with a legend line. Under each one: "Figure N:" in bold, then a 13 px gray caption that states the scale ("shared 0 to 100 scale; higher is better") |
| Footnotes | `*` marks in 13 px gray link to notes that give conditions (effort setting, harness, date) |
| Brand colors | Mostly neutrals: `#282828`, `#676767`, `#969696`, `#f3f3f2`. Saturated colors (cobalt, emerald, amber) appear only inside charts |
| Wordmark | Uppercase, spaced (1.7 px tracking), gray, 17 px. Quiet, not a logo shape |
| Motion | 0.2 to 0.3 s color and opacity fades. Chart points fade in. Every animation has a `prefers-reduced-motion: reduce` rule that turns it off |

What a text read also showed: the page is candid about limits, every number carries its conditions, demos show the real
artifact next to its prompt, charts sit over plain tables, and the voice is a measured "we".

## 2. What we take

- **A reading page, not a landing page.** One centered text column (680 px), a serif for reading, a sans for UI.
- **A plain title.** "Heimdall: proof before review", a date, one short lede. No slogan, no italic second line.
- **A table of contents.** Sticky on the left on wide screens, a compact list under the lede on small ones.
- **Numbers in tables, not in hero tiles.** Every number has a unit and a condition, and a footnote gives the source.
- **"Figure N:" captions** that say what the figure shows and what is illustration.
- **Quiet links.** Underline at low opacity that gets stronger on hover. A slow color fade, nothing that moves.
- **Saturated color only where it carries meaning.** Pages stay neutral.
- **Reduced motion respected everywhere.**

## 3. What we leave

- Their fonts (Iowan Old Style, GT America, Chakra Petch are not ours and are not free). We use two Google fonts.
- Their logo, wordmark style, blob illustration, grid art and any text.
- White-only pages. We keep a dark mode, because v5 had one and developers use it.
- Pill buttons with a spring animation. We use a plain rectangular button with a 2 px radius.
- Radar charts. Our data is small; a table with inline bars is clearer.
- Prev and next post links. We have one page, so anchored sections and a "back to contents" link do that job.

## 4. Identity

### Logo

Three candidates are in `site/brand/logo-options.html`: A Gate (a doorway with a check inside), B Crossing (a doorway
open at one corner, the check leaving through the gap) and C Gateposts (two posts with a check in the gap).

We chose **C, Gateposts**. Reasons:

- It says the product in one shape: two gateposts, and nothing passes unless the check holds.
- It has three strokes and no closed box. At 16 px it stays sharp, and it does not read as a checkbox. A and B both
  read as a checked box at small sizes, which is the most common "done" icon there is.
- It works in one color and in reverse with no change, because it is stroke only.
- A first try joined the check to both posts to make the letter H. It read as "M" or "И", so the check floats.

The wordmark is drawn as strokes on a 12-unit cap height, uppercase and spaced, so the SVG files need no font. The two
Ls share one base line: a small bridge.

Files: `site/brand/logo.svg` (mark and wordmark), `site/brand/logo-mark.svg`, `site/brand/favicon.svg` (it switches to
a light stroke in a dark browser theme). The mark is drawn on a 24-unit grid with a 2.5-unit stroke. Keep clear space
of half the mark's width on every side. Do not fill it, add a gradient, or rotate it.

### Color

One accent, and neutrals. The accent marks only proven things: the finding that blocks, its proof, and the focus ring.
Everything else is ink on paper.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--paper` | `#fbfaf7` | `#121211` | Page background |
| `--fill` | `#f2f0eb` | `#1b1a18` | Figure panels, table header band |
| `--rule` | `#e4e1da` | `#2d2b28` | 1 px rules and borders |
| `--muted` | `#67645e` | `#a29e95` | Captions, dates, footnotes, UI labels |
| `--text` | `#2c2b28` | `#d9d6cf` | Body text |
| `--ink` | `#1d1c1a` | `#eeece7` | Headings, logo, primary button |
| `--accent` | `#b3341b` | `#f0805f` | Signal vermilion. Proven findings, proof labels, focus ring |

All text pairs pass WCAG AA (4.5:1) on `--paper` and `--fill`; the check is in section 5.

### Type

Two Google fonts. **Newsreader** for reading text and headings. **IBM Plex Sans** for UI: navigation, contents,
captions, tables, buttons and numbers. Code uses the system monospace font (`ui-monospace`, SF Mono, Menlo), so there
is no third web font.

| Step | Size / line height | Font | Use |
|---|---|---|---|
| Title | 34 px / 1.2 (28 px under 600 px) | Newsreader 600 | Page title only |
| Section | 24 px / 1.3 | Newsreader 600 | h2 |
| Subsection | 19 px / 1.4 | Newsreader 600 | h3 |
| Body | 18 px / 1.65 | Newsreader 400 | Paragraphs, 680 px column (about 72 characters) |
| UI | 14 px / 1.5 | Plex Sans 400 and 500 | Nav, tables, captions, buttons |
| Micro | 12.5 px / 1.45 | Plex Sans 400 | Footnotes, labels |

### Space, borders, numbers, footnotes

- Spacing scale: 4, 8, 12, 16, 24, 32, 48, 64, 96 px. Sections are 64 px apart; a head has 16 px below it.
- Borders: 1 px `--rule` only. No shadows. Radius 2 px on buttons and panels, 0 on tables.
- Tables: Plex Sans 14 px, tabular figures, numbers right-aligned, a rule under the header and between rows, no
  vertical rules. A missing value is "–" and a footnote says why.
- Numbers: every number has a unit and a condition ("10 cases, DeepSeek V4.1 Flash, 21 Nov 2026"), and it links to the
  source line on `product-build`.
- Footnotes: a superscript number in Plex Sans links to a numbered note at the end of the section. The note gives the
  condition and the source. Each note links back.
- Figures: a `--fill` panel, then "Figure N:" in Plex Sans 500 and a 14 px `--muted` caption. A caption says plainly
  which parts are illustration.
- Motion: color and opacity fades of 0.2 to 0.3 s. The hero demo is the only sequence. Under
  `prefers-reduced-motion: reduce`, nothing moves and the demo shows its final state.

## 5. Mood v7: periwinkle mist

The owner sent two mood references in November 2026: third-party paintings of a Greek temple in blue mist. Their
licence is unknown. We use them only as a mood. Nothing from them is copied, traced, embedded or kept in the
repository. The v6 page structure, type, content and truth rules stay as they are; v7 changes color, texture and art.

### What we take from the mood

- One periwinkle monochrome: sky about `#7f8ce8`, deep indigo shadows about `#2b2f8f`, pale pink-white light about
  `#f3e6f0`.
- Heavy grain in the art, a light grain on the page.
- Mist and depth: far hills that fade into the sky, a lit haze at the horizon.
- Quiet classical architecture. Columns catch a pale light.
- A tiny figure for scale, and a lot of empty sky. Calm, with nothing urgent.

### Color (replaces the v6 table in section 4)

The accent changes from vermilion to a rose that comes from the pale pink light in the mood. It still marks only
proven things: the finding that blocks, its proof, and the focus ring. In the art the same light is the "check".

| Token | Light (periwinkle paper) | Dark (indigo night) | Use |
|---|---|---|---|
| `--paper` | `#eceefb` | `#11133a` | Page background |
| `--fill` | `#e0e3f7` | `#191c4a` | Figure panels, table header band |
| `--rule` | `#c3c8ee` | `#2f3474` | 1 px rules and borders |
| `--muted` | `#4b5096` | `#a6abe8` | Captions, dates, footnotes, UI labels |
| `--text` | `#242862` | `#d8dbf7` | Body text |
| `--ink` | `#161946` | `#f3e6f0` | Headings, logo, primary button |
| `--accent` | `#a3245f` | `#f4a9cf` | Rose. Proven findings, proof labels, focus ring |

Contrast (WCAG 2.1 ratio, computed from the hex values):

| Pair | Light on paper | Light on fill | Dark on paper | Dark on fill |
|---|---|---|---|---|
| `--muted` | 6.30 | 5.72 | 8.16 | 7.36 |
| `--text` | 11.70 | 10.61 | 13.07 | 11.79 |
| `--ink` | 14.42 | 13.08 | 14.76 | 13.31 |
| `--accent` | 6.11 | 5.55 | 9.70 | 8.74 |

The art has its own tokens (`--sky0` to `--sky2`, `--glow`, `--hill-far`, `--hill-near`, `--ground`, `--col-dark`,
`--col-mid`, `--col-lit`, `--figure`). They switch with the theme, so the same SVG is a day scene on light paper and a
night scene on dark. No text sits on the art.

### Grain

- The page grain is an inline SVG `feTurbulence` noise (a data URI, about 0.5 KB), tiled at 180 px on a fixed
  `body::before` layer with `pointer-events: none`.
- It sits under the content (`z-index: -1`), so it changes only the page color. Text is never drawn through it.
- Opacity is 7% in light and 6% in dark. The worst case, every grain pixel pure black (light) or pure white (dark) at
  that opacity, keeps the lowest pair (accent on fill, light theme) at 4.86:1 with 6% and above 4.5:1 with 7%.
- Under `prefers-reduced-transparency: reduce` the grain is off.
- Panels (`--fill`) cover the grain, so tables and the demo stay flat and crisp.

### Hero art: "The gate at first light"

An original inline SVG above the title. It shows the Gateposts mark as two tall fluted columns on a misty plain. A pale
pink-white light comes through the gap between them, and the check from the logo floats in that gap as light. Soft
hills and a faint distant colonnade sit behind. One tiny figure walks toward the gate.

- Depth comes from layered gradients, Gaussian blur on the far layers and the mist, and a grain overlay.
- The columns are lit from the gap (v7.1). Each shaft has a shadow side with a little reflected light, a lit side that
  faces the gap, fluting that is faint in shadow and bright on the lit side, and a soft shadow under the capital. Each
  column casts a soft shadow outward on the plinth, and a pool of light lies between them. Mist rises over the bases
  and the plinth. The far colonnade is fainter and more blurred, with haze over its base.
- The SVG uses the art tokens, so it follows the theme. One mask fades the whole scene to transparent at the top and
  the bottom (v7.1), so the page color and its grain show through. The art has no top or bottom edge in either theme.
- It scales with `preserveAspectRatio="xMidYMax slice"`: wide screens see the whole plain, a 360 px phone sees the
  gate and the figure.
- The mist drifts 24 px over 40 s and the glow breathes slowly. Under `prefers-reduced-motion: reduce` both stop.
- It has a `<title>` and `<desc>` as its text alternative.
- The logo files in `site/brand/` do not change. The art is an illustration of the mark, not a new mark.

### Page changes

- Section heads get a short rule in `--rule` above them. Table headers sit on a `--fill` band.
- Figure 1 keeps its content. Its panel takes a faint sky gradient, and the proven row glows in the rose accent.
- The og:image is `site/shots/og-v7.png`, made from the hero art and the title.

### Checks (22 November 2026, headless Chromium 1223)

- 1440, 834 and 360 px, light and dark: 0 px horizontal overflow. Every visible text element passes AA with the
  worst-case grain added: lowest ratio 5.24:1 in light, 6.94:1 in dark. No text sits over the art.
- Keyboard: a visible 2 px focus ring on 40 of 40 tab stops in both themes (`site/shots/v7-focus-demo.png`).
- Reduced motion: the mist and glow animations stop. Reduced transparency: the grain is off.
- Weight: the HTML with all inline assets is 62,862 bytes (16,356 gzip), up from 53,606 (13,675 gzip) in v6. The art
  is 6.4 KB of it. Total transfer with fonts is 232 KB, up from 223 KB.
- v7.1 (22 November 2026, same browser): 0 px overflow at 1440, 834 and 360 px in both themes. Lowest text contrast
  with worst-case grain 5.24:1 in light and 7.03:1 in dark. Every tab stop shows the 2 px ring in both themes. With
  reduced motion the mist and glow have no animation. The art is 9.4 KB; the HTML is 65,962 bytes (16,860 gzip).
- LCP on a local server with 150 ms latency, 1.6 Mbit/s and 4x CPU slowdown, 3 runs: v7 516 to 584 ms at 1440 px and
  504 to 556 ms at 360 px; v6 500 to 548 ms and 492 to 536 ms. The LCP element is the lede paragraph in both.
