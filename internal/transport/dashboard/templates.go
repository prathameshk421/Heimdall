package dashboard

// listHTML is the list page. It holds no script and no style: the stylesheet comes from /assets.
const listHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Heimdall changes</title>
<link rel="stylesheet" href="/assets/dashboard.css">
</head>
<body>
<header>
<h1>Heimdall changes</h1>
<p class="note">Read only. Each row comes from the event log of one change.</p>
</header>
<main>
{{- if .Rows}}
<div class="scroll">
<table>
<thead>
<tr><th scope="col">Change</th><th scope="col">State</th><th scope="col">Gate</th><th scope="col">Open findings</th><th scope="col">Version</th></tr>
</thead>
<tbody>
{{- range .Rows}}
<tr><td><a href="/changes/{{.ID}}">{{.Ticket}}</a></td><td>{{.State}}</td><td>{{.Gate}}</td><td>{{.OpenFindings}}</td><td>{{.Version}}</td></tr>
{{- end}}
</tbody>
</table>
</div>
{{- else}}
<p>No changes in the log yet.</p>
{{- end}}
</main>
</body>
</html>
`

// detailHTML is the change page. The live part reads /sse/changes/{id} and adds rows; the script is in /assets.
const detailHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.D.Ticket}} · Heimdall</title>
<link rel="stylesheet" href="/assets/dashboard.css">
</head>
<body>
<header>
<p><a href="/">All changes</a></p>
<h1>{{.D.Ticket}}</h1>
<dl class="facts">
<dt>Repository</dt><dd>{{.D.Repo}}</dd>
<dt>State</dt><dd id="state">{{.D.State}}</dd>
<dt>Gate</dt><dd id="gate">{{.D.Gate.Status}}</dd>
<dt>Version</dt><dd id="version">{{.D.Version}}</dd>
<dt>Last event</dt><dd>{{.Updated}}</dd>
</dl>
{{- if .D.Problem}}
<p class="problem">The log does not replay in the state machine: {{.D.Problem}}</p>
{{- end}}
</header>
<main>
<p id="live" class="live" data-stream="{{.Stream}}" data-after="{{.D.Version}}"><span id="live-status">Live updates: connecting</span></p>

<section>
<h2>Gate and blockers</h2>
{{- if .D.Blockers}}
<ul>
{{- range .D.Blockers}}
<li>{{.}}</li>
{{- end}}
</ul>
{{- else}}
<p>No blockers in the latest gate.</p>
{{- end}}
{{- if .D.Gate.SHA}}
<p class="note">Head commit: {{.D.Gate.SHA}}</p>
{{- end}}
</section>

<section>
<h2>Findings</h2>
{{- if .D.Findings}}
<div class="scroll">
<table>
<thead><tr><th scope="col">Finding</th><th scope="col">Tier</th><th scope="col">State</th><th scope="col">By</th></tr></thead>
<tbody>
{{- range .D.Findings}}
<tr><td>{{.Ref}}</td><td>{{.Tier}}</td><td>{{.State}}</td><td>{{.By}}</td></tr>
{{- end}}
</tbody>
</table>
</div>
{{- else}}
<p>No findings.</p>
{{- end}}
</section>

<section>
<h2>Reviewers</h2>
{{- if .D.Reviewers}}
<div class="scroll">
<table>
<thead><tr><th scope="col">Reviewer</th><th scope="col">Status</th></tr></thead>
<tbody>
{{- range .D.Reviewers}}
<tr><td>{{.Name}}</td><td>{{.Status}}</td></tr>
{{- end}}
</tbody>
</table>
</div>
{{- else}}
<p>No reviewers picked yet.</p>
{{- end}}
</section>

<section>
<h2>Open questions</h2>
{{- if .Asks}}
<ul>
{{- range .Asks}}
<li>{{.ID}} asked {{.Since}}</li>
{{- end}}
</ul>
{{- else}}
<p>No open questions.</p>
{{- end}}
</section>

<section>
<h2>Timeline</h2>
<div class="scroll">
<table>
<thead><tr><th scope="col">Version</th><th scope="col">At</th><th scope="col">Event</th><th scope="col">Actor</th><th scope="col">Note</th></tr></thead>
<tbody id="timeline">
{{- range .Timeline}}
<tr data-version="{{.Version}}"><td>{{.Version}}</td><td>{{.At}}</td><td>{{.Type}}</td><td>{{.Actor}}</td><td>{{.Note}}</td></tr>
{{- end}}
</tbody>
</table>
</div>
</section>
</main>
<script src="/assets/dashboard.js" defer></script>
</body>
</html>
`
