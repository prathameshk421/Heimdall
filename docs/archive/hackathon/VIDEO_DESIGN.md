# Heimdall film: design system

The rules for every frame. If a shot breaks a rule, the shot changes.

## What the research says

| Source | Finding | What we do |
|---|---|---|
| Hyperframes `launch.md` | Effects-heavy films drew about a tenth of the views of films built on a real result | Real Discord, GitHub and Jira are the film. Graphics only explain what cannot be filmed |
| Hyperframes `story.md` section 8 | Named tells: dark void with small floating things, cream-paper-serif-mono-label look, everything fading up, three cards in a row, docked logo | All banned below. Our first two drafts were these tells |
| Hyperframes `story.md` section 5 | Several worlds, each with its own ground and scale, entered through an object on screen | Five worlds below |
| Awesome-opus: `annacher` (sprites.ai) | "One shape, never cut": the same element morphs, a cursor drives every change | The Pack is our one shape |
| Awesome-opus: `heyiamnick` | One object promoted through stages, and it never leaves the screen | The Pack travels from laptop to gate |
| Awesome-opus: `howdevelop` (TanStack) | Every transition demonstrates a feature. No fake dashboards, no tiny code | Each transition is a product step |
| Awesome-opus: top posters | Big tight sans headline, real UI at large scale, bright grounds | Type and scale rules below |
| PDoomVideo | Sets, not cards. Something happens in every shot. Motivated transitions. Palette arc. Camera moves in every shot | Palette arc follows the story. No static shot |
| `/brag` | Hook in 2 s, real UI, one claim per scene, 0.3 s per word, every frame postable | Applied to every beat |
| Hyperframes `film.md` | Speech is about 2.2 words a second. About 19 words per 9 s scene. Whole film 400 words at most | VO limited to 360 words |

## The one idea

**The Pack.** A single rounded card carries one developer's knowledge. It is born on their laptop, gets sealed, crosses the machine, opens inside their reviewer, and its proof lands at the gate. Every world change is the Pack doing its job. If you pause any frame, you can find it.

## Colour

Values are from anthropic.com's own CSS tokens. Inspiration only: no Anthropic logo and no implied endorsement.

| Token | Hex | Role |
|---|---|---|
| `ivory` | `#faf9f5` | Light ground |
| `oat` | `#e3dacc` | Light surface edge |
| `slate` | `#141413` | Dark ground, main text on light |
| `slate-2` | `#262624` | Dark surface |
| `mid` | `#5e5d59` | Quiet text on light |
| `periwinkle` | `#cde2fb` | Light blue ground |
| `deep` | `#184f95` | Deep blue ground, text on periwinkle |
| `sky` | `#6a9bcc` | Blue accent |
| `clay` | `#d97757` | The one accent: rationed to one word, the payoff, the Pack |
| `olive` | `#788c5d` | Pass state, third developer |
| `error` | `#bf4d43` | Fail state only |

Developers: bob `clay`, ana `sky`, eli `olive`.

**Rationing:** one accent item per frame. No gradients on UI, no glow, no particles.

## Worlds (palette arc)

| # | World | Ground | Ink | Story |
|---|---|---|---|---|
| 1 | Laptop | `periwinkle` | `deep` | What the dev already has |
| 2 | Scan | `slate` | `ivory`, `clay` | Secrets struck out |
| 3 | Pack | `ivory` | `slate` | Distil, approve, seal |
| 4 | Machine | `deep` | `ivory`, `periwinkle` | VM, OpenCode, Switch, Graphiti, Go |
| 5 | Real | `ivory` | Real UI | Jira, Discord, Telegram, GitHub footage |
| 6 | Proof | `slate` | `ivory`, `error` | Failing test, gate |
| 7 | End | `clay` | `ivory` | Mark and claim |

## Type

| Role | Face | Size at 1080p | Notes |
|---|---|---|---|
| Hero line | Geist 600 to 800, tracking -0.035em | 120 to 200 px | Fills 60 to 90% of width. 6 words at most per line |
| Secondary | Geist 500 | 44 to 60 px | One per frame |
| Real text | Geist Mono | 26 px minimum | Only for real code, paths, commands, logs. Never decoration |

Banned: serif headlines with mono labels, handwriting fonts, trailing periods on titles, small corner captions as decoration, emoji.

A landed title stays dead still. Motion moves to the next thing.

## Shape

- Pack and cards: radius 28 px, 2 px edge at 10% ink, one soft shadow `0 40px 80px -30px rgba(20,20,19,.35)`.
- Icon tile: 132 px, radius 30 px, real logo at 72 px on white. Real brand colours where the brand has one.
- Minimum content size: nothing important under 40 px tall. No small thing in empty space. Elements fill the frame, and may crop off its edge on purpose.

## Logos

Real files only, in `video/heimdall/assets/logos/`: Jira, GitHub, GitHub Actions, Discord, Telegram, Mattermost, Slack, Go, Podman, Postgres, Docker, Git, Claude, OpenAI, OpenCode, Switch. Graphiti has no public logo file, so its name is set in type. Never redrawn. Slack appears under "next", and is not shown as connected.

## Motion

| Thing | Rule |
|---|---|
| Arrive | `expo.out` or spring with slight overshoot, 0.45 to 0.7 s |
| Camera, flood | `power2.inOut`, 0.5 to 0.7 s, blur up to 16 px on fast moves |
| Exit | 40% shorter than entry, `power3.in` |
| Stagger | 60 to 120 ms |
| Cursor | Oversized, 5% of frame height. Its click causes the next thing on the same frame |
| Hits | Stamps and slams use a 0 ms set plus a camera shake under 12 px |
| Rhythm | Something changes at least every 1.5 s. Payoffs at normal speed |

Transitions, one cause each: zoom through an object, colour flood from the seal, the Pack carries across, whip on a list, iris from a click. No crossfades between worlds.

## Footage

Real capture sits large, tilted slightly in 3D, at 1.6 to 2x, on its world ground. The camera cycles: whole window → one pane → the one line. Oversized cursor. Text at least 26 px after the push. A sped-up wait shows a real clock chip.

## Truth rules

- Fake product screens are banned: no invented Discord message, GitHub comment or Actions run.
- Scenes for unrun architecture carry a small `ARCHITECTURE` tag, the one small label allowed.
- Numbers come only from the table in `VIDEO_PLAN.md`.
- Sample content inside diagrams uses real files from the fixture and repo wherever one exists.

## Sound

Voice is the clock: text lands on the word that says it. Music: Sascha Ende, ende.app (CC BY 4.0), ducks under voice. Few effects, each with a cause on screen: stamp, slam, click. Loudness about -14 LUFS.

## Review loop for every scene

1. `hyperframes check --json`: fix every error, and read warnings.
2. `hyperframes snapshot --review`: read the three sheets, including the 360 px phone sheet.
3. Write P0 and P1 notes as `t · where · problem → change`. Check the tells list by name.
4. Fix, and snapshot only what changed. A second pass only if a P0 stays open.
5. Cover the words and check that the pictures still tell the story.
