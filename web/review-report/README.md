# Review report page

This builds the script and stylesheet of the dashboard's review report (`/changes/<id>/report`). The page answers one
question: why can this change merge, or why can it not?

The server builds the page as a [json-render](https://github.com/vercel-labs/json-render) spec from the event log
(`internal/app/dashboard/report.go`). No model writes it. The page accepts only the components in `src/catalog.ts`,
and it refuses a spec that does not match. Reviewer text reaches the page only as string props, so React shows it as
text.

The build writes `report.js` and `report.css` into `internal/transport/dashboard/assets`, and the Go binary embeds
them, so the server needs no Node. Rebuild after a change here, and commit the two files:

    npm ci
    npm run build

When a prop struct in `report.go` changes, change the matching schema in `src/catalog.ts` in the same commit.
