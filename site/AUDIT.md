# Landing page: direction and audit

Status: Nov 10, 2026. Method: the unslop skill (Damir Sokolovsky, CC BY 4.0).

## The brief

The old page looked like a template. Three complaints, in order of weight:

1. **It never said what the product is.** "Nothing crosses without proof" is a slogan. A visitor
   could not tell that Heimdall is a merge gate for AI-written code, that it starts at the ticket,
   that the tests must fail first, or that a person approves the commit.
2. **The background was the same image twice.** `art/asgard-xl.webp` opened the hero and
   `art/arrival.webp` closed the page. Both are the same Asgard composition, so the page looked
   like it had one background that it could not decide where to put.
3. **Vibecoded.** Scroll-jacked camera pans, six tinted gradient stages, a rainbow gradient used as
   chrome in five places, purple glow buttons, uppercase tracked kickers, italic for emphasis, a
   display serif doing the work of an interface font.

Target browser: Firefox. The page is also checked in Chromium.

## Direction

- **Product.** A merge gate for teams whose code is written by AI agents. It starts at the ticket,
  the tests must fail first, three reviewers file findings with proof, and ten plain Go rules
  decide. Only a person approves the commit.
- **Character.** Plain, exact, a little severe. A gate, not a concierge.
- **World.** The Bifröst is the merge path. Heimdall is a guardian, so the world is a threshold at
  golden hour. The Norse art is real brand and stays, but it is used once per scene, never as
  wallpaper.
- **Moments.** (1) The hero: the bridge with the gate's own verdict panel rising from the bottom
  edge. (2) A finding with the failing test that proves it, side by side.
- **Signature.** The real clause ids from `internal/domain/gate/gate.go` in mono
  (`test_tree_pinned`, `approval`, `blocking`), and the evidence tiers E1 to E4. Only this
  product has those.

## Audit of the old page

### Surfaces and colour

| Sign | Where it lives |
| --- | --- |
| Rainbow gradient as chrome | `--bifrost` on the scroll thread, the primary button underline, the ticker rule, the step rail fill, the verdict word |
| Gradient text | `.v-title.ok em` fills "open" with the rainbow and clips it to text |
| Radial washes behind content | `.veil` over the hero, `.close::before`, `.hook-pin::after`, and six per-step `TINT` gradients |
| Purple and coloured glows | `.merged` is `#8957e5` with a purple glow, `.allpass` is green with a glow |
| Too many surfaces | `--paper`, `--wash`, `--night`, `--night-2`, `--blue`, `--sky` are five different hues for one page |
| Every icon has its own colour | Slack, Discord, Telegram, Mattermost, Jira and GitHub marks in six brand colours |

### Type

| Sign | Where it lives |
| --- | --- |
| Display serif everywhere | `--f-display` sets h1, h2, h3, the big number, the step list, the verdict, the wordmark and the question buttons |
| Uppercase tracked labels | every `.kicker`, `text-transform: uppercase`, `letter-spacing: .14em` |
| Italic for emphasis | `<em>` in most headings |
| Heading weights | Geist at 700 on the stack list, 600 on several places |
| Mixed number formats | `+77,460` against `42 MiB` and `10 MB` |

### Icons

Sizes run 12, 14, 15, 16, 18, 20, 22 and 24 px across the page. The nav, the buttons and the cards
each pick a different one.

### Spacing

`--pad` is `clamp(96px, 13vw, 180px)`, so section rhythm changes with the viewport instead of
holding a rhythm. Card radii run 10, 12, 14, 16, 18, 22, 28 and 34 px.

### Copy

"No new dashboard to learn", "Your team stays where it is" and "All Go, on one small VM" would fit
any product with the name swapped. "Watch it run" is a nav label, not a promise.

### Components

The six channel cards are six identical cards with the same shape. The ten gate rules are ten
identical cards in a five-column grid. The step list, the stack list and the questions are the same
row pattern three times.

### Landing page and hero

The headline could fit any gate product. The hero is a 270svh scroll-jacked camera pan over the
painting with the product video rising over it. The nav is a floating glass pill with a coloured
progress thread.

### Visuals

One background, twice. Six of the fourteen paintings are near-identical crops of the same Asgard
scene, so the art reads as one image repeated.

### Motion

The camera pan, the word-by-word scrub, the canvas diff, two parallax layers, clip-path bands that
open as you scroll, and an infinite ticker. The gate verdict changes only because the page tells it
to.

### Firefox

`.w { overflow: clip }` with `line-height: .92` cuts the bottom off the second line of the hero
headline in Firefox. Measured at 1440 px: the word box is 125.78 px tall and the glyph box is
111.27 px, so the descenders fall outside the clip. The fix is to give the mask room for the
descender rather than to clip to the line box.

## The palette, and why it is cool

The first attempt kept the warm cream paper (`#f7f4ee`) and the warm brown-black night
(`#161513`). It failed, and the reason is measurable from the art itself:

| Element | Pixel |
| --- | --- |
| Hero painting sky (`asgard-xl.webp`, upper left) | `242, 223, 198` warm cream |
| Hero painting citadel | `253, 239, 197` gold |
| Old page paper | `247, 244, 238` warm cream |
| Old page night | `22, 21, 19` warm brown-black |
| Close painting sky (`finale.webp`) | `18, 22, 41` deep navy |

The page ground and the painting were the same hue, so the art dissolved into the page instead of
reading as an object on it, and the warm-brown night fought the blue dusk painting at the close.
The gold accent could not carry itself because it was the same family as the ground.

**The palette is now a cool gallery wall**, so the warm painting is the one bright thing hung on
a neutral wall:

| Token | Value | Role |
| --- | --- | --- |
| `--paper` | `#f2f3f6` | the wall |
| `--card` | `#ffffff` | a sheet on the wall |
| `--raised` | `#e6e8ee` | the alcove sections |
| `--ink` | `#12141b` | headings |
| `--text` | `#4b505c` | body |
| `--muted` | `#5f6572` | quiet text |
| `--night` | `#11142a` | the deciding side, the dusk painting's own blue |
| `--night-2` | `#181c34` | windows and code blocks |
| `--night-3` | `#232840` | hover |
| `--night-ink` | `#edeff5` | headings on the night side |
| `--night-text` | `#a6acbe` | body on the night side |
| `--night-muted` | `#8b91a4` | quiet text on the night side |
| `--gold` | `#8a5a12` | the one accent, the gold of the horn |
| `--gold-2` | `#e5b45c` | the accent on the night side |
| `--pass` | `#116a34` | met |
| `--fail` | `#b91c1c` | not met |
| `--pass-2` | `#4cc38a` | met, on the night side |
| `--fail-2` | `#f2878f` | not met, on the night side |

`--fail-2` is a rose red rather than the old salmon, because salmon read as warm against navy.
`--pass` was darkened from `#15803d` after measuring 4.09 against its own tinted badge.

**All 26 text-on-surface pairs pass WCAG AA (4.5:1 or better).** The script that checks this reads
the tokens straight out of the built file, so it cannot drift from the CSS.

## What changed

- A new hero that says what the product is in the first two lines, over a scene used once. The
  headline is "Nothing merges without proof" and the kicker above it is "A merge gate for code
  written by AI", so the product is named in the first ten words.
- The gate verdict panel as the hero's product view, built from the real clause ids, rising off
  the bottom edge of the viewport.
- The finding and its failing test shown together, with the E1 tier.
- Art used once per scene: `asgard-xl` for the hero, `finale` for the close, `m-realms` for the
  tools, and the rest dropped from the page.
- The six real screens move into one sticky window that follows the six steps down the run
  section. On a narrow screen each screen sits under its own step instead.
- The ten real clauses from `internal/domain/gate/gate.go` are a pressable list. Pressing one
  moves the verdict between "Merge blocked" and "Merge open", and the sub-line names the clauses
  that are not met.
- One cool ramp for the page, one night ramp for the deciding parts, one gold accent.
- Geist for the interface, Instrument Serif for the headline only, Geist Mono for clause ids.
- Sentence case for the labels. One emphasised phrase per headline, in gold, never in italic.
- Static layout. Motion only where it explains something: the entrance, the gate verdict, the
  accordion, the screenshot lightbox.
- The tools section becomes the realms painting with the real marks on the glass spheres, which is
  what `site/art/ART_PROMPTS.md` always intended. The sphere positions were measured from the
  painting itself, not guessed.
- Radii are 8, 12 and 16 px plus circles. One easing curve. Two font weights for the interface.

## Bugs found by measuring, not by looking

Every one of these was found with a script in Firefox, not by reading the CSS:

- The ticker was `.tick-run` in the markup and `.ticker-run` in the stylesheet, so its SVGs fell
  back to the 300x150 default and rendered as giant logos.
- `.win` was used both for the run-section windows and for the architecture figure, so the
  architecture image was pulled into the sticky stage.
- A duplicated `const stage` in the script threw a page error that killed every rule on the page.
- The sticky stage had a fixed height, so a short screenshot left a dead navy panel under it, and
  a tall one ran up under the nav. The frames now size to their own image and cap at the stage
  height.
- The sticky window unpinned before the last step was read. The steps now carry a 46svh rhythm
  and 130 px of runway.
- `.prob` was an implicit-`auto` grid, so the code block inside it pushed 8 px past the viewport
  on a phone.
- The gate rules rendered no icons: the `.mk` span was empty and its SVG had no size.
- The close section's primary button was dark ink on a dark scene.
- The Graphiti video stopped after 5 s instead of looping.

## Verification

Firefox 159 headless via Playwright, at 1440x900, 834x1112 and 390x844.

- No horizontal overflow at any of the three widths: `scrollWidth == clientWidth`.
- No console errors and no page errors at any of the three widths.
- No element wider than its container, other than the ticker marquee and the scrollable code
  blocks, which are meant to run off.
- No oversized SVG anywhere.
- No heading collides with the fixed nav.
- Every image loads.
- All 26 contrast pairs pass AA.
- The gate verdict moves when a rule is pressed, in both directions.
- The copy button writes the real setup prompt to the clipboard.
- The accordion opens and closes.
- The screenshot lightbox opens with the right image and closes on Escape.

The gate reasons on the page are the real strings from `internal/domain/gate/gate.go`, and the
clause ids are the real constants from the same file.

---

Part of unslop by Damir Sokolovsky ([@heydamir](https://x.com/heydamir)), CC BY 4.0. Keep this
credit in copies and adaptations.
