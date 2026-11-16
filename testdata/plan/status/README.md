# Status goldens (`Render`)

`Render(view, missing)` (H7, `AGENT_INTERACTION_ARCH.md` 4.1) is checked against the `status.txt` beside each
`view.json` in `testdata/plan/view/` (every `point-N/`, and `merge-without-approval/`). There is one copy of each.
The earlier `rendered/` folder held byte-identical duplicates and was removed. M5 step 4 reproduces them byte for
byte. How the inputs are made is in `testdata/plan/view/README.md`.

## Layout

Six lines, each ending in `\n`. Every value comes from the view or from `Missing`; nothing is added that no event
carries. In particular there is no title (the view has none; the Jira description is a blob), no display name
(people and agents appear as their ids), and no alignment beyond the label column.

```
<ticket> · <summary>
Tests     <tests>
PR        <pr>
Reviews   <reviews>
Findings  <findings>
Next      <next>
```

The label is padded with spaces to 10 characters (all labels are ASCII). `·` is U+00B7 with one space on each
side. A short SHA is the first 7 characters.

| Part | Text |
|---|---|
| summary | `merged` when `closed.merged`; `closed without merge` when closed and not merged; otherwise `1 need` or `<n> needs`, n the length of `missing` |
| tests | `none yet` when there is no submission. Otherwise `<n> test(s) for <AC ids>` (n the distinct test names in `ac_map`; `test` when n is 1; AC ids by number, comma separated) ` · submitted on <sha>`, then ` · verified` when verified, then ` · committed <sha>` when committed |
| pr | `none yet`, or `#<number> by <author> · head <head>`, then ` · slop passed`, ` · slop failed: <rules>` or ` · slop not run` (slop counts only when it is about the head), then ` · acceptance <conclusion on head>` or ` · acceptance not reported`, then ` · merged` or ` · closed` when closed |
| reviews | `not picked`, or for each picked person in pick order, joined by ` · `: `<id> waiting` with no review, `<id> clean on <sha>`, or `<id> <n> finding(s) on <sha>`, where sha is the head the review was on |
| findings | `none`, or for each finding by ref number, joined by ` · `: `<ref> <tier> <severity> <state>` |
| next | the `text` of each need in `missing`, joined by `; `, or `nothing` when there is none |

The 4.1 sketch uses titles, display names, icons and a right-aligned summary. Those need data no event carries
(titles, names) or a column rule nobody defined, so the goldens leave them out. If the owner wants them, the event or
the rule comes first, then the goldens change.
