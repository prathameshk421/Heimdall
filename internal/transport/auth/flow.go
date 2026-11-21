package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// maxFormBody bounds the form an unsafe request may post with its CSRF token.
const maxFormBody = 64 << 10

// Mount returns next with the gate's own routes in front of it. Every path under /auth/ is served here, and every
// other request goes to next unchanged. Only the login page and its style are open to a request with no session.
func (g *Gate) Mount(next http.Handler) http.Handler {
	routes := http.NewServeMux()
	routes.HandleFunc("GET "+loginPath, g.loginPage)
	routes.HandleFunc("GET "+startPath, g.start)
	routes.HandleFunc("GET "+callbackPath, g.callback)
	routes.HandleFunc("GET "+SignOutPath, g.logoutPage)
	routes.HandleFunc("POST "+SignOutPath, g.logout)
	routes.HandleFunc("GET "+stylePath, g.pages.style)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", pageCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		routes.ServeHTTP(w, r)
	})
}

// Require guards next: the request needs a live session of a person in the role map with at least role least. An
// unsafe method also needs a same-origin request and the session's CSRF token. A browser that asks for a page with
// no session goes to the login page; any other request with no session gets 401. A known session without the role
// gets 403.
func (g *Gate) Require(least Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			s, ok := g.session(r)
			if !ok {
				g.unauthenticated(w, r)
				return
			}
			p, ok := g.people.current(s.Person, s.Identity)
			if !ok || p.Role < least {
				http.Error(w, "your sign-in does not give access to this page", http.StatusForbidden)
				return
			}
			if !safeMethod(r.Method) && !g.unsafeAllowed(w, r, s) {
				http.Error(w, "request refused: it must come from this site with its token", http.StatusForbidden)
				return
			}
			pr := Principal{Person: p.Name, Login: s.Display, Role: p.Role, CSRF: s.CSRF}
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), pr)))
		})
	}
}

// unauthenticated sends a page request to the login page and answers anything else with 401.
func (g *Gate) unauthenticated(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		redirect(w, loginPath+"?next="+url.QueryEscape(r.URL.RequestURI()))
		return
	}
	http.Error(w, "sign in required", http.StatusUnauthorized)
}

// unsafeAllowed checks a state-changing request: same origin by Fetch metadata or Origin, and the CSRF token of
// the session in the X-CSRF-Token header or the csrf form field.
func (g *Gate) unsafeAllowed(w http.ResponseWriter, r *http.Request, s session) bool {
	if err := g.origin.Check(r); err != nil {
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
		if err := r.ParseForm(); err != nil {
			return false
		}
		token = r.PostForm.Get("csrf")
	}
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.CSRF)) == 1
}

func (g *Gate) loginPage(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	g.pages.login(w, r, g.logger, startPath+"?next="+url.QueryEscape(next))
}

// start begins a sign-in: fresh state, nonce and PKCE verifier, sealed in the login cookie, then the provider.
func (g *Gate) start(w http.ResponseWriter, r *http.Request) {
	ls := loginState{Next: safeNext(r.URL.Query().Get("next")), Verifier: oauth2.GenerateVerifier()}
	var err error
	if ls.State, err = randomToken(); err == nil {
		ls.Nonce, err = randomToken()
	}
	if err != nil {
		g.fail(w, r, http.StatusInternalServerError, "sign-in could not start", err)
		return
	}
	target, err := g.provider.authURL(r.Context(), ls.State, ls.Nonce, ls.Verifier)
	if err != nil {
		g.fail(w, r, http.StatusBadGateway, "the identity provider is not reachable; try again later", err)
		return
	}
	ls.Expires = g.now().Add(loginTTL).Unix()
	value, err := g.sealer.seal(loginCookie, ls)
	if err != nil {
		g.fail(w, r, http.StatusInternalServerError, "sign-in could not start", err)
		return
	}
	setCookie(w, loginCookie, value, int(loginTTL/time.Second))
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}

// callback finishes a sign-in. The login cookie is used once. The state must match it, the provider must prove an
// identity, and the identity must be in the role map. Only then is a session issued.
func (g *Gate) callback(w http.ResponseWriter, r *http.Request) {
	ls, ok := g.loginState(r)
	clearCookie(w, loginCookie)
	q := r.URL.Query()
	switch {
	case !ok:
		g.fail(w, r, http.StatusBadRequest, "this sign-in has expired; start again", nil)
		return
	case q.Get("error") != "":
		g.fail(w, r, http.StatusBadRequest, "the identity provider did not sign you in", nil)
		return
	case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(ls.State)) != 1 || q.Get("code") == "":
		g.fail(w, r, http.StatusBadRequest, "this sign-in does not match; start again", nil)
		return
	}
	proven, err := g.provider.identify(r.Context(), q.Get("code"), ls.Verifier, ls.Nonce)
	switch {
	case errors.Is(err, errUnavailable):
		g.fail(w, r, http.StatusBadGateway, "the identity provider is not reachable; try again later", err)
		return
	case err != nil:
		g.fail(w, r, http.StatusUnauthorized, "the identity provider's answer did not prove who you are", err)
		return
	}
	p, ok := g.people.byProof(proven)
	if !ok {
		g.logger.WarnContext(r.Context(), "board sign-in refused: the identity is not in board_auth.people")
		g.pages.message(w, r, g.logger, http.StatusForbidden, "Your account is not on this board. Ask an admin to add you.")
		return
	}
	if err := g.issue(w, p, proven); err != nil {
		g.fail(w, r, http.StatusInternalServerError, "sign-in could not finish", err)
		return
	}
	g.logger.InfoContext(r.Context(), "board sign-in", "person", p.Name, "role", p.Role.String())
	redirect(w, safeNext(ls.Next))
}

// loginState opens the login cookie and checks it has not expired.
func (g *Gate) loginState(r *http.Request) (loginState, bool) {
	c, err := r.Cookie(loginCookie)
	if err != nil {
		return loginState{}, false
	}
	var ls loginState
	if err := g.sealer.open(loginCookie, c.Value, &ls); err != nil || ls.State == "" || ls.Verifier == "" {
		return loginState{}, false
	}
	if g.now().Unix() >= ls.Expires {
		return loginState{}, false
	}
	return ls, true
}

// logoutPage shows the sign-out form. A GET never signs out, so a link or an image cannot end a session.
func (g *Gate) logoutPage(w http.ResponseWriter, r *http.Request) {
	s, ok := g.session(r)
	if !ok {
		redirect(w, loginPath)
		return
	}
	g.pages.logout(w, r, g.logger, s.CSRF)
}

// logout revokes the session in this process and clears its cookie. It needs the session's CSRF token.
func (g *Gate) logout(w http.ResponseWriter, r *http.Request) {
	s, ok := g.session(r)
	if !ok {
		clearCookie(w, sessionCookie)
		redirect(w, loginPath)
		return
	}
	if !g.unsafeAllowed(w, r, s) {
		http.Error(w, "request refused: it must come from this site with its token", http.StatusForbidden)
		return
	}
	g.revoked.add(s.ID, time.Unix(s.Expires, 0), g.now())
	clearCookie(w, sessionCookie)
	g.logger.InfoContext(r.Context(), "board sign-out", "person", s.Person)
	redirect(w, loginPath)
}

// fail logs err, when there is one, and shows a fixed message. The client never sees the error.
func (g *Gate) fail(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	if err != nil {
		g.logger.WarnContext(r.Context(), "board sign-in failed", "status", status, "error", err)
	}
	g.pages.message(w, r, g.logger, status, msg)
}

// redirect answers 303 with a Location this package built from a checked path.
func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
}
