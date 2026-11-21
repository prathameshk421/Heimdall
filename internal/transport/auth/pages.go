package auth

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
)

// pageHTML is the one page the gate shows: a sign-in link, a sign-out form or a message. It holds no script, and its
// style comes from /auth/assets/login.css, so the strict page policy holds.
const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Heimdall sign in</title>
<link rel="stylesheet" href="/auth/assets/login.css">
</head>
<body>
<main>
<h1>Heimdall</h1>
{{- if .Message}}
<p>{{.Message}}</p>
<p><a href="/auth/login">Back to sign in</a></p>
{{- else if .CSRF}}
<form method="post" action="/auth/logout">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<button type="submit">Sign out</button>
</form>
{{- else}}
<p>The board shows the changes Heimdall checks. Sign in to read it.</p>
<p><a class="button" href="{{.Start}}">{{.Label}}</a></p>
{{- end}}
</main>
</body>
</html>
`

// loginCSS is the style of the gate's page.
const loginCSS = `body{margin:0;font:16px/1.5 system-ui,sans-serif;background:#f6f7f9;color:#1d2330}
main{max-width:28rem;margin:12vh auto;padding:2rem;background:#fff;border:1px solid #d9dde5;border-radius:8px}
h1{margin-top:0;font-size:1.5rem}
a.button,button{display:inline-block;padding:.6rem 1rem;border:0;border-radius:6px;background:#1d4ed8;color:#fff;
font:inherit;text-decoration:none;cursor:pointer}
@media (prefers-color-scheme:dark){body{background:#11141a;color:#e6e9ef}main{background:#1a1e26;border-color:#2c3340}}
`

// pages renders the gate's page.
type pages struct {
	tmpl  *template.Template
	label string
}

// pageData is what one render shows. Exactly one of Message, CSRF and Start drives the page.
type pageData struct {
	Message string
	CSRF    string
	Start   string
	Label   string
}

func newPages(label string) (*pages, error) {
	t, err := template.New("auth").Parse(pageHTML)
	if err != nil {
		return nil, fmt.Errorf("auth page: %w", err)
	}
	return &pages{tmpl: t, label: label}, nil
}

func (p *pages) login(w http.ResponseWriter, r *http.Request, log *slog.Logger, start string) {
	p.render(w, r, log, http.StatusOK, pageData{Start: start, Label: p.label})
}

func (p *pages) logout(w http.ResponseWriter, r *http.Request, log *slog.Logger, csrf string) {
	p.render(w, r, log, http.StatusOK, pageData{CSRF: csrf})
}

func (p *pages) message(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, msg string) {
	p.render(w, r, log, status, pageData{Message: msg})
}

func (p *pages) style(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	// A failed write means the client has gone away; there is no one left to tell.
	_, _ = io.WriteString(w, loginCSS)
}

// render runs the template into a buffer first, so a template error never leaves half a page.
func (p *pages) render(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, d pageData) {
	var buf bytes.Buffer
	if err := p.tmpl.Execute(&buf, d); err != nil {
		log.ErrorContext(r.Context(), "auth page render failed", "error", err)
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// A failed write means the client has gone away; there is no one left to tell.
	_, _ = buf.WriteTo(w)
}
