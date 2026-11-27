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

## 6. v9 "Interlock" (27 November 2026)

The owner said the product page looked flat and asked for real interaction and Three.js. The v8 page is kept as
`site/archive/previous-v8.html`. v9 keeps every fact and source link; it changes the direction, the type, the layout and
adds two interactive moments.

### Sources we read and what we took

| Source | What we took |
|---|---|
| Anthropic, "Improving frontend design through skills" | No Inter, Roboto, Arial or system fonts. Weight extremes (200 against 900) and size jumps of 3x or more. One dominant ground with sharp accents, in CSS variables. One orchestrated page load with staggered reveals, not scattered motion. Atmosphere instead of a flat fill |
| Leonxlnx/taste-skill | A code-review tool shows its own working UI, red and green, with its action. One effect (the WebGL gate) and one or two dealt moments (the proof lab). The hero enters once. No fade-in on scroll. Anything that moves past 5 s has a pause control. Nothing invented. No violet |
| LeoStehlik/no-slop-ui | 1 px rules, 2 to 4 px radius, no glass panels, no pill buttons, no eyebrow labels, no decorative gradients |
| funboy322/avoid-ai-design | The direction comes from the subject, not a style menu. Avoid the second-order defaults (cream with terracotta, near-black with one neon, navy or slate, Space Grotesk and its cluster). Run the scanner before and after |
| miqdadbadjuber/anti-slop | No three identical feature cards, no stat strip, no logo bar, no template footer. Section order follows the product story |
| Netxeo/skill-file-design | Asymmetric splits (7/5 and 5/7 columns), a fluid 90% width, varied section rhythm, animate only transform and opacity |
| GrubbyLee/design-guide | Declare the design system before the build, then verify at 1440, 1024 and 390 px with screenshots |
| lotfb86/web-design-skills | Commit to one conceptual direction and name the one thing a visitor remembers |
| alchaincyf/huashu-design | Facts before assumptions, start from existing assets (the Gateposts mark, the golden), and look at your own screenshots before you call it done |

### Direction

The subject is a gate that lets a change through only when a test proves it safe. That is a railway interlocking: a
signal clears only when the route is proven. The page borrows the signal box, not the trains.

- **Remembered thing:** the Gateposts mark built in iron. Twelve comments rise out of the fog, pass through the gap, and
  only one is hung on the gate in red.
- **Ground:** galvanized grey `#e6e9e5` by day, `#121514` by night. Ink is iron `#0d1110`.
- **Signals:** three lamp colors with fixed meanings. Red: a proven finding blocks. Amber: a test is running. Green:
  the gate is clear. They appear nowhere else, except the rule line "Fails twice on the PR. Passes on the fix."
- **Type:** Big Shoulders Display 900 and 200 (tall, condensed signage, like gateposts) for headings, uppercase.
  Overpass 300 to 800 (from US highway signs) for text. IBM Plex Mono for test output and commits.
- **Layout:** 90% fluid width up to 1320 px, 7/5 and 5/7 splits, one dark band (the proof lab).
- **Buttons:** one pair, solid iron and a ruled outline, 2 px radius, a 1 px step on hover and a press on click.

### The two moments

1. **The gate (hero, `site/js/gate.js`).** Three.js 0.170.0 from jsDelivr through an import map. Every pose is a pure
   function of the scene time, so Replay, Pause and reduced motion need no extra state. Pointer parallax, hover a card
   to read it. The loop stops when the hero is off screen or the tab is hidden. Without WebGL the page shows the mark as
   a still. Under `prefers-reduced-motion` the scene shows its final state and never moves.
2. **The proof lab (`site/js/lab.js`).** Prove one comment or all twelve, then push the fix, approve the new head and
   merge. Each verdict follows a real rule: two red runs and a green fix prove; a pass on the head, one red run, a build
   error or a style request do not. It is labelled as an illustration (Figure 1).

### Scanner results (`avoid-ai-design` 0.5.0 `detect.mjs`)

Final run: P0 0, P1 2, P2 1. We keep the three, for these reasons:

- **SD2 (near-black with one signal), dark scheme only.** The page has three signal colors, each with one meaning, and
  the light scheme is the default.
- **SD5 (one accented word).** The flagged spans are the second line of every h2, a whole clause in weight 200. It is
  the type system (weight extremes), used the same way on every heading.
- **C5 (colored glow shadows).** Only on lamps that are lit, as a lit lamp looks.

### Checks run (27 November 2026, Claude Browser pane, Chromium)

- 1440 x 900 light and dark, 390 x 844 dark: no sideways scroll (`scrollWidth` 390 at 390). Hero copy and both buttons
  fit the first screen at 1440 x 900.
- The scene ran to its end state: 12 in, 11 hidden, "Merge locked: F1 proven". Hover shows the card text and verdict.
  Pause and Replay work. No console errors.
- The lab ran end to end: Prove all 12 gave 1 proven and 11 hidden, then Push the fix, Approve e4f5a6b, Merge.
- **Not verified:** reduced-motion rendering (the pane cannot emulate it; the code sets the final state), the no-WebGL
  still, Safari and Firefox, LCP and weight on a throttled network, and the deployed Vercel copy.

### v9.1 light (27 November 2026)

The owner said v9 was still too plain and asked for light effects. Light now carries the state, as on a real signal:

- **The hero is always a night scene** (its own tokens on `.hero`), in both schemes. Moonlight from behind the gate.
- **Bloom** (Three.js `UnrealBloomPass`, threshold 0.82): only lamps, the scan line, the check and the shaft glow.
- **A shaft of light** falls through the gap onto a pool on the ground, with **dust** drifting in it. Its color is the
  gate's state: warm white at rest, amber while testing, red once F1 is proven. Halos on both lamps and behind F1.
- **A hand lamp:** a warm point light follows the pointer across the front of the gate.
- **The rule line** sits dim until a lamp falls on it. It follows the pointer and sweeps slowly at rest. Touch screens
  and reduced motion show it fully lit.
- **The proof lab frame** takes the gate's light: amber while testing, red when locked, green when clear.
- **The close is a night band with a signal head:** red and amber dark, green lit.
- Cards are pale paper slips in both schemes, so they read under the lamps.

Checked in the Claude Browser pane at 1440 x 900 and 390 x 844: no sideways scroll, no console errors, the idle,
testing and locked light states, the lamp, the lab light and the close. Scanner unchanged (P0 0, P1 2, P2 1).
**Not verified:** frame rate on a low-end GPU with bloom on, Safari and Firefox.

## 7. v10 "Temple" (27 November 2026)

The owner asked to bring back the v7 blue pillars in mist, keep the condensed type direction with cleaner type, drop the
Three.js scene (it looked cheap), and sell the product from its outcomes. v9 is kept as `site/archive/previous-v9.html`
(its scripts in `site/archive/js/`).

### Direction

- **Art:** the v7 illustration of the Gateposts mark as two fluted columns in periwinkle mist, now the full hero. Three
  depth layers (far hills, the gate, near mist) move a few pixels with the pointer. The check of light draws itself
  once on load, the halo breathes, the mist drifts. Plain SVG and CSS, no library. Reduced motion shows the final art.
- **Palette:** the v7 tokens, light (periwinkle paper) and dark (indigo night). One rose accent marks only proven
  things. The proof lab and the close are indigo night bands.
- **Type:** Sofia Sans Condensed 700 and 300 for headings, sentence case; Sofia Sans for text; IBM Plex Mono for code
  and test output. It keeps the tall, column-like letters of v9 without the shouting.
- **Story:** who it is for (startups and maintainers with more agent pull requests than reviewers), what changes in the
  repo (outcomes, each with a number and a source), the proof lab, honest tests, the footprint, the sandbox, results,
  the demo, setup, questions.
- **New moment:** "Try to cheat the tests". Seven moves an agent can make; each shows an example diff, the rule id and
  its level from `internal/domain/testintegrity/doc.go`.

### Scanner (`avoid-ai-design` 0.5.0)

Final run: P0 0, P1 4, P2 1. C2 (indigo buttons) and SD9 (indigo night ground) are the owner's palette, so they stay.
L11 flags "411", which opens section 2, not the hero. SD5 flags the light second line of each heading, the type system.
C5 is the glow on lit lamps in the proof lab. We removed the gradient bar fill and the header blur.

### Checks (Claude Browser pane, Chromium)

1440 x 900 light and dark, 390 x 844 light: art renders, no sideways scroll, no failed requests, no console errors. The
cheat widget switches moves. The proof lab is unchanged from v9. **Not verified:** Safari and Firefox, reduced motion,
LCP on a slow network.
