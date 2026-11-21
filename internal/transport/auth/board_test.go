package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/auth"
)

func TestNoBoardRouteIsReadableWithoutASession(t *testing.T) {
	r := newRig(t, rigOptions{})
	junk := &http.Cookie{Name: sessionCookie, Value: "not-a-session"}
	for _, path := range boardRoutes() {
		requireRefused(t, r.get(path), path)
		requireRefused(t, r.get(path, junk), path+" with a junk cookie")
		requireRefused(t, r.do(http.MethodHead, path, nil, nil), path+" by HEAD")
		requireRefused(t, r.do(http.MethodGet, path, nil, http.Header{"Accept": {"text/event-stream"}}), path+" as a stream")
	}
}

// A browser that asks for a page is sent to the login page, which remembers where it was going.
func TestPageRequestWithoutASessionGoesToTheLoginPage(t *testing.T) {
	r := newRig(t, rigOptions{})
	resp := r.do(http.MethodGet, "/changes/PAY-7/report?tab=gate", nil, http.Header{"Accept": {"text/html,application/xhtml+xml"}})
	require.Equal(t, http.StatusSeeOther, resp.Status)
	require.Equal(t, "/auth/login?next="+url.QueryEscape("/changes/PAY-7/report?tab=gate"), resp.Header.Get("Location"))
	require.NotContains(t, resp.Body, "PAY-7 ")
}

func TestOnlyTheLoginPageAndItsStyleAreOpen(t *testing.T) {
	r := newRig(t, rigOptions{})
	page := r.get("/auth/login?next=/changes/PAY-7")
	require.Equal(t, http.StatusOK, page.Status)
	require.Contains(t, page.Body, `href="/auth/start?next=%2Fchanges%2FPAY-7"`)
	require.Contains(t, page.Header.Get("Content-Security-Policy"), "default-src 'none'")
	require.Equal(t, "no-store", page.Header.Get("Cache-Control"))
	css := r.get("/auth/assets/login.css")
	require.Equal(t, http.StatusOK, css.Status)
	require.True(t, strings.HasPrefix(css.Header.Get("Content-Type"), "text/css"))
	for _, path := range []string{"/auth/assets/dashboard.css", "/auth/nothing"} {
		require.Equal(t, http.StatusNotFound, r.get(path).Status, path)
	}
	// A dot path is cleaned by a redirect, and the cleaned path is a board route again.
	dotted := r.get("/auth/assets/../../assets/dashboard.js")
	require.NotEqual(t, http.StatusOK, dotted.Status)
	require.NotContains(t, dotted.Body, "EventSource")
}

func TestASignedInViewerReadsEveryBoardRoute(t *testing.T) {
	r := newRig(t, rigOptions{})
	session := r.loginAs("vera@example.com")
	for path, want := range map[string]int{
		"/": http.StatusOK, "/changes/PAY-7": http.StatusOK, "/changes/PAY-7/report": http.StatusOK,
		"/changes/PAY-7/report.json": http.StatusOK, "/sse/changes/PAY-7": http.StatusOK,
		"/assets/dashboard.css": http.StatusOK, "/assets/dashboard.js": http.StatusOK, "/assets/report.css": http.StatusOK,
		"/assets/report.js": http.StatusOK, "/changes/NOPE-1": http.StatusNotFound,
	} {
		resp := r.get(path, session)
		require.Equal(t, want, resp.Status, path)
		require.Contains(t, resp.Header.Get("Cache-Control"), "no", path)
	}
	require.Contains(t, r.get("/", session).Body, `href="/auth/logout"`, "the board offers sign out")
}

func TestLoginStartSendsPKCEStateAndNonce(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.start("/"), r.start("/")
	q := a.authURL.Query()
	require.Equal(t, r.idp.issuer()+"/authorize", a.authURL.Scheme+"://"+a.authURL.Host+a.authURL.Path)
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, clientID, q.Get("client_id"))
	require.Equal(t, r.idp.redirectURL, q.Get("redirect_uri"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Contains(t, strings.Fields(q.Get("scope")), "openid")
	for _, k := range []string{"state", "nonce", "code_challenge"} {
		require.GreaterOrEqual(t, len(q.Get(k)), 43, "%s has at least 256 bits", k)
		require.NotEqual(t, q.Get(k), b.authURL.Query().Get(k), "%s is fresh on every start", k)
	}
	require.NotContains(t, a.cookie.Value, q.Get("state"), "the login cookie is sealed, not plain")
	requireHardened(t, a.cookie)
	require.LessOrEqual(t, a.cookie.MaxAge, 600)
}

func TestSessionCookieIsHardened(t *testing.T) {
	r := newRig(t, rigOptions{})
	s := r.start("/")
	resp := r.finish(s, grant{claims: r.claimsFor(s, "vera@example.com")})
	session := cookieNamed(resp.Cookies, sessionCookie)
	require.NotNil(t, session)
	requireHardened(t, session)
	require.Equal(t, int(time.Hour/time.Second), session.MaxAge)
	cleared := cookieNamed(resp.Cookies, loginCookie)
	require.NotNil(t, cleared, "the login cookie is used once")
	require.Negative(t, cleared.MaxAge)
	require.NotContains(t, session.Value, "vera", "the session cookie is sealed")
}

func requireHardened(t *testing.T, c *http.Cookie) {
	t.Helper()
	require.True(t, c.HttpOnly, c.Name)
	require.True(t, c.Secure, c.Name)
	require.Equal(t, http.SameSiteLaxMode, c.SameSite, c.Name)
	require.Equal(t, "/", c.Path, c.Name)
	require.Empty(t, c.Domain, c.Name)
}

func TestTamperedOrForeignSessionCookieIsRefused(t *testing.T) {
	r := newRig(t, rigOptions{})
	good := r.loginAs("vera@example.com")
	require.Equal(t, http.StatusOK, r.get("/", good).Status)

	foreign := newRig(t, rigOptions{key: otherKey}).loginAs("vera@example.com")
	s := r.start("/")
	cases := map[string]string{
		"one byte flipped":  flip(good.Value, len(good.Value)/2),
		"last byte flipped": flip(good.Value, len(good.Value)-1),
		"truncated":         good.Value[:len(good.Value)-4],
		"empty":             "",
		"not base64":        "!!!!",
		"login cookie":      s.cookie.Value,
		"other server key":  foreign.Value,
		"oversized":         strings.Repeat("A", 8192),
		"spare bit":         spareBit(good.Value),
	}
	for name, value := range cases {
		resp := r.get("/", &http.Cookie{Name: sessionCookie, Value: value})
		require.Equal(t, http.StatusUnauthorized, resp.Status, name)
	}
	// Every single character changed, the last one too: unpadded base64 has spare bits there, and the decoder must
	// refuse them, so a sealed value has one spelling only.
	for i := range len(good.Value) {
		resp := r.get("/test/whoami", &http.Cookie{Name: sessionCookie, Value: flip(good.Value, i)})
		require.Equal(t, http.StatusUnauthorized, resp.Status, "character %d changed", i)
	}
}

// spareBit changes only the lowest bit of the last character. In unpadded base64 of a length that is not a multiple
// of 3 bytes, that bit carries no data, so a lax decoder would read the same bytes.
func spareBit(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, s[len(s)-1])
	return s[:len(s)-1] + string(alphabet[last^1])
}

func flip(s string, i int) string {
	b := []byte(s)
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}

func TestExpiredSessionIsRefused(t *testing.T) {
	r := newRig(t, rigOptions{})
	session := r.loginAs("vera@example.com")
	r.clock.advance(time.Hour - time.Second)
	require.Equal(t, http.StatusOK, r.get("/", session).Status)
	r.clock.advance(2 * time.Second)
	requireRefused(t, r.get("/", session), "after the TTL")
}

func TestUnknownLoginGets403AndNoSession(t *testing.T) {
	r := newRig(t, rigOptions{})
	s := r.start("/")
	resp := r.finish(s, grant{claims: r.claimsFor(s, "mallory@example.com")})
	require.Equal(t, http.StatusForbidden, resp.Status)
	require.Nil(t, cookieNamed(resp.Cookies, sessionCookie))
	require.NotContains(t, resp.Body, "mallory", "the page does not echo the login")
}

func TestOpenRedirectIsBlocked(t *testing.T) {
	cases := map[string]string{
		"/changes/PAY-7":                "/changes/PAY-7",
		"/changes/PAY-7/report?tab=x":   "/changes/PAY-7/report?tab=x",
		"https://evil.example/":         "/",
		"//evil.example/":               "/",
		"/\\evil.example/":              "/",
		"\\\\evil.example":              "/",
		"/%2F%2Fevil.example":           "/%2F%2Fevil.example",
		"javascript:alert(1)":           "/",
		"evil.example":                  "/",
		"/changes/\r\nSet-Cookie: x=1":  "/",
		"/changes/PAY-7#frag":           "/",
		"":                              "/",
		"https:/evil.example":           "/",
		"/auth/callback?code=x&state=y": "/",
	}
	for next, want := range cases {
		r := newRig(t, rigOptions{})
		s := r.start(next)
		resp := r.finish(s, grant{claims: r.claimsFor(s, "vera@example.com")})
		require.Equal(t, http.StatusSeeOther, resp.Status, next)
		require.Equal(t, want, resp.Header.Get("Location"), "next %q", next)
	}
}

func TestRolesAreOrdered(t *testing.T) {
	r := newRig(t, rigOptions{})
	viewer, approver, admin := r.loginAs("vera@example.com"), r.loginAs("ana@example.com"), r.loginAs("alice@example.com")
	for _, c := range []struct {
		path   string
		cookie *http.Cookie
		want   int
	}{
		{"/test/approver", viewer, http.StatusForbidden},
		{"/test/approver", approver, http.StatusOK},
		{"/test/approver", admin, http.StatusOK},
		{"/test/admin", viewer, http.StatusForbidden},
		{"/test/admin", approver, http.StatusForbidden},
		{"/test/admin", admin, http.StatusOK},
	} {
		require.Equal(t, c.want, r.get(c.path, c.cookie).Status, c.path)
	}
	require.True(t, strings.HasPrefix(r.get("/test/whoami", approver).Body, "ana approver "))
}

// The role map is read on every request, not frozen into the cookie: a server restarted with a smaller map takes
// effect at once for sessions it sealed before.
func TestRoleMapIsReadOnEveryRequest(t *testing.T) {
	idp := newFakeIDP(t)
	r := newRig(t, rigOptions{idp: idp})
	session := r.loginAs("ana@example.com")
	require.Equal(t, http.StatusOK, r.get("/test/approver", session).Status)

	demoted := newRig(t, rigOptions{idp: idp, clock: r.clock, people: []auth.Person{
		{Name: "ana", Login: "ana@example.com", Role: auth.RoleViewer},
	}})
	require.Equal(t, http.StatusForbidden, demoted.get("/test/approver", session).Status)
	require.Equal(t, http.StatusOK, demoted.get("/", session).Status)

	removed := newRig(t, rigOptions{idp: idp, clock: r.clock, people: []auth.Person{
		{Name: "vera", Login: "vera@example.com", Role: auth.RoleViewer},
	}})
	require.Equal(t, http.StatusForbidden, removed.get("/", session).Status)

	relinked := newRig(t, rigOptions{idp: idp, clock: r.clock, people: []auth.Person{
		{Name: "ana", Login: "someone-else@example.com", Role: auth.RoleAdmin},
	}})
	require.Equal(t, http.StatusForbidden, relinked.get("/", session).Status, "the person now has another login")
}

func TestUnsafeMethodNeedsTheCSRFTokenAndSameOrigin(t *testing.T) {
	r := newRig(t, rigOptions{})
	session := r.loginAs("vera@example.com")
	token := strings.Fields(r.get("/test/whoami", session).Body)[2]
	require.Len(t, token, 43)

	post := func(h http.Header) int { return r.do(http.MethodPost, "/test/write", nil, h, session).Status }
	require.Equal(t, http.StatusForbidden, post(nil), "no token")
	require.Equal(t, http.StatusForbidden, post(http.Header{"X-Csrf-Token": {flip(token, 3)}}), "wrong token")
	require.Equal(t, http.StatusOK, post(http.Header{"X-Csrf-Token": {token}}))
	require.Equal(t, http.StatusForbidden, post(http.Header{"X-Csrf-Token": {token}, "Sec-Fetch-Site": {"cross-site"}}))
	require.Equal(t, http.StatusForbidden, post(http.Header{"X-Csrf-Token": {token}, "Origin": {"https://evil.example"}}))
	form := strings.NewReader(url.Values{"csrf": {token}}.Encode())
	resp := r.do(http.MethodPost, "/test/write", form, http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, session)
	require.Equal(t, http.StatusOK, resp.Status, "the token may come as a form field")
	requireRefused(t, r.do(http.MethodPost, "/test/write", nil, http.Header{"X-Csrf-Token": {token}}), "no session")
}

func TestLogoutNeedsTheTokenAndRevokesTheSession(t *testing.T) {
	r := newRig(t, rigOptions{})
	session := r.loginAs("vera@example.com")
	page := r.get("/auth/logout", session)
	require.Equal(t, http.StatusOK, page.Status)
	require.Contains(t, page.Body, `method="post"`)
	token := strings.Fields(r.get("/test/whoami", session).Body)[2]
	require.Contains(t, page.Body, `value="`+token+`"`)

	logout := func(token string) reply {
		form := strings.NewReader(url.Values{"csrf": {token}}.Encode())
		return r.do(http.MethodPost, "/auth/logout", form, http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, session)
	}
	require.Equal(t, http.StatusForbidden, logout("").Status)
	require.Equal(t, http.StatusForbidden, logout(flip(token, 0)).Status)
	require.Equal(t, http.StatusOK, r.get("/", session).Status, "a refused logout keeps the session")

	resp := logout(token)
	require.Equal(t, http.StatusSeeOther, resp.Status)
	require.Equal(t, "/auth/login", resp.Header.Get("Location"))
	cleared := cookieNamed(resp.Cookies, sessionCookie)
	require.NotNil(t, cleared)
	require.Negative(t, cleared.MaxAge)
	requireRefused(t, r.get("/", session), "a copy of the old cookie is revoked")
	require.Equal(t, http.StatusSeeOther, r.get("/auth/logout").Status, "the logout page needs a session")
}

func TestCallbackRefusesABrokenLoginState(t *testing.T) {
	r := newRig(t, rigOptions{})
	s := r.start("/")
	claims := r.claimsFor(s, "vera@example.com")
	issue := func() {
		r.idp.issue("code-1", grant{challenge: s.authURL.Query().Get("code_challenge"), claims: claims})
	}

	issue()
	q := url.Values{"code": {"code-1"}, "state": {"wrong-state"}}
	require.Equal(t, http.StatusBadRequest, r.get("/auth/callback?"+q.Encode(), s.cookie).Status, "state mismatch")
	q.Set("state", s.authURL.Query().Get("state"))
	require.Equal(t, http.StatusBadRequest, r.get("/auth/callback?"+q.Encode()).Status, "no login cookie")
	other := r.start("/")
	require.Equal(t, http.StatusBadRequest, r.get("/auth/callback?"+q.Encode(), other.cookie).Status, "another login's cookie")
	require.Equal(t, http.StatusBadRequest, r.get("/auth/callback?error=access_denied&state="+q.Get("state"), s.cookie).Status)
	require.Equal(t, http.StatusBadRequest, r.get("/auth/callback?state="+q.Get("state"), s.cookie).Status, "no code")
	require.Zero(t, r.idp.exchanged, "nothing above reached the token endpoint")

	r.clock.advance(11 * time.Minute)
	require.Equal(t, http.StatusBadRequest, r.callback(s, "code-1").Status, "the login state expired")
}

func TestCallbackRefusesAReplayedCode(t *testing.T) {
	r := newRig(t, rigOptions{})
	s := r.start("/")
	require.Equal(t, http.StatusSeeOther, r.finish(s, grant{claims: r.claimsFor(s, "vera@example.com")}).Status)
	resp := r.callback(s, "code-1")
	require.Equal(t, http.StatusUnauthorized, resp.Status)
	require.Nil(t, cookieNamed(resp.Cookies, sessionCookie))
}

func TestIDTokenIsVerified(t *testing.T) {
	cases := map[string]func(r *rig, c map[string]any){
		"wrong nonce":        func(_ *rig, c map[string]any) { c["nonce"] = "other" },
		"no nonce":           func(_ *rig, c map[string]any) { delete(c, "nonce") },
		"wrong audience":     func(_ *rig, c map[string]any) { c["aud"] = "another-client" },
		"wrong issuer":       func(_ *rig, c map[string]any) { c["iss"] = "https://evil.example" },
		"expired":            func(r *rig, c map[string]any) { c["exp"] = r.clock.Now().Add(-time.Minute).Unix() },
		"email not verified": func(_ *rig, c map[string]any) { c["email_verified"] = false },
		"no email_verified":  func(_ *rig, c map[string]any) { delete(c, "email_verified") },
		"no email":           func(_ *rig, c map[string]any) { delete(c, "email") },
	}
	for name, tweak := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			s := r.start("/")
			c := r.claimsFor(s, "vera@example.com")
			tweak(r, c)
			resp := r.finish(s, grant{claims: c})
			require.Equal(t, http.StatusUnauthorized, resp.Status)
			require.Nil(t, cookieNamed(resp.Cookies, sessionCookie))
		})
	}
}

func TestForgedIDTokenIsRefused(t *testing.T) {
	r := newRig(t, rigOptions{})
	forger, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	r.idp.forgeKey = forger
	s := r.start("/")
	resp := r.finish(s, grant{claims: r.claimsFor(s, "alice@example.com")})
	require.Equal(t, http.StatusUnauthorized, resp.Status, "a token not signed by the issuer's published key")
	require.Nil(t, cookieNamed(resp.Cookies, sessionCookie))
}

func TestEmailVerifiedAsTextIsAccepted(t *testing.T) {
	r := newRig(t, rigOptions{})
	s := r.start("/")
	c := r.claimsFor(s, "Vera@Example.com")
	c["email_verified"] = "true"
	resp := r.finish(s, grant{claims: c})
	require.Equal(t, http.StatusSeeOther, resp.Status, "some providers send the flag as text, and email case is folded")
}

func TestSubjectClaimIsMatchedExactly(t *testing.T) {
	people := []auth.Person{{Name: "vera", Login: "00uVERA", Role: auth.RoleViewer}}
	r := newRig(t, rigOptions{claim: "sub", people: people})
	s := r.start("/")
	c := r.claimsFor(s, "x@example.com")
	c["sub"], c["email_verified"] = "00uVERA", false
	require.Equal(t, http.StatusSeeOther, r.finish(s, grant{claims: c}).Status, "sub mode needs no verified email")

	s = r.start("/")
	c = r.claimsFor(s, "x@example.com")
	c["sub"] = "00uvera"
	require.Equal(t, http.StatusForbidden, r.finish(s, grant{claims: c}).Status, "a subject is matched with its case")
}

func TestGitHubSignInMapsTheNumericId(t *testing.T) {
	r := newRig(t, rigOptions{github: true})
	s := r.start("/changes/PAY-7")
	require.Equal(t, "https://github.com/login/oauth/authorize", s.authURL.Scheme+"://"+s.authURL.Host+s.authURL.Path)
	require.Equal(t, "S256", s.authURL.Query().Get("code_challenge_method"))
	resp := r.finish(s, grant{login: "ana-gh", githubID: 1002})
	require.Equal(t, http.StatusSeeOther, resp.Status, resp.Body)
	require.Equal(t, "/changes/PAY-7", resp.Header.Get("Location"))
	session := cookieNamed(resp.Cookies, sessionCookie)
	require.Equal(t, http.StatusOK, r.get("/test/approver", session).Status)
	require.True(t, strings.HasPrefix(r.get("/test/whoami", session).Body, "ana approver "))

	s = r.start("/")
	resp = r.finish(s, grant{login: "mallory", githubID: 9999})
	require.Equal(t, http.StatusForbidden, resp.Status)
	require.Nil(t, cookieNamed(resp.Cookies, sessionCookie))
}

// A renamed GitHub account keeps its id, so it keeps its person, whatever its login is now.
func TestGitHubRenamedLoginKeepsThePerson(t *testing.T) {
	r := newRig(t, rigOptions{github: true})
	before := r.loginGitHub("ana-old-name", 1002)
	after := r.loginGitHub("ana-new-name", 1002)
	for _, c := range []*http.Cookie{before, after} {
		require.True(t, strings.HasPrefix(r.get("/test/whoami", c).Body, "ana approver "))
	}
	require.True(t, strings.HasPrefix(r.get("/test/whoami", r.loginGitHub("vera-gh", 1001)).Body, "vera viewer "),
		"another id is another person")
}

// A freed login can be claimed by another account. That account has another id, so it gets no person, even when its
// login is the one a listed person used to have.
func TestGitHubReusedLoginWithAnotherIdIsRefused(t *testing.T) {
	r := newRig(t, rigOptions{github: true})
	require.NotNil(t, r.loginGitHub("ana-gh", 1002), "the real owner signs in under the old login")
	for _, login := range []string{"ana-gh", "Ana-GH", "ana"} {
		s := r.start("/")
		resp := r.finish(s, grant{login: login, githubID: 424242})
		require.Equal(t, http.StatusForbidden, resp.Status, login)
		require.Nil(t, cookieNamed(resp.Cookies, sessionCookie), login)
		require.NotContains(t, resp.Body, login, "the page does not echo the login")
	}
}

// The user API must prove an id: a missing, zero, negative, text, fractional or oversized id gives no session.
func TestGitHubWithoutAUsableIdIsRefused(t *testing.T) {
	cases := map[string]string{
		"no id":               `{"login":"ana-gh"}`,
		"null id":             `{"login":"ana-gh","id":null}`,
		"zero id":             `{"login":"ana-gh","id":0}`,
		"negative id":         `{"login":"ana-gh","id":-1002}`,
		"id as text":          `{"login":"ana-gh","id":"1002"}`,
		"fractional id":       `{"login":"ana-gh","id":1002.5}`,
		"exponent id":         `{"login":"ana-gh","id":1.002e3}`,
		"id beyond int64":     `{"login":"ana-gh","id":9223372036854775808}`,
		"huge id":             `{"login":"ana-gh","id":1` + strings.Repeat("0", 400) + `}`,
		"id as array":         `{"login":"ana-gh","id":[1002]}`,
		"login but no id key": `{"login":"ana-gh","node_id":"MDQ6VXNlcjEwMDI="}`,
	}
	r := newRig(t, rigOptions{github: true})
	for name, body := range cases {
		s := r.start("/")
		resp := r.finish(s, grant{login: "ana-gh", userBody: body})
		require.Equal(t, http.StatusUnauthorized, resp.Status, name)
		require.Nil(t, cookieNamed(resp.Cookies, sessionCookie), name)
	}
}

// The largest id the board accepts is a whole int64, read without a float rounding it onto another account.
func TestGitHubIdIsReadAsAnExactInteger(t *testing.T) {
	const big = int64(9007199254740993) // 2^53 + 1: a float64 would turn it into 2^53
	people := []auth.Person{{Name: "ana", GitHubID: big - 1, Role: auth.RoleViewer}}
	r := newRig(t, rigOptions{github: true, people: people})
	s := r.start("/")
	resp := r.finish(s, grant{login: "x", githubID: big})
	require.Equal(t, http.StatusForbidden, resp.Status, "a neighbour id is another account")
	s = r.start("/")
	resp = r.finish(s, grant{login: "x", githubID: big - 1})
	require.Equal(t, http.StatusSeeOther, resp.Status)
}

// The session names the proven id. A person whose id changes in the map loses a session sealed for the old id.
func TestGitHubSessionIsCheckedAgainstTheIdInTheMap(t *testing.T) {
	idp := newFakeIDP(t)
	r := newRig(t, rigOptions{github: true, idp: idp})
	session := r.loginGitHub("ana-gh", 1002)
	require.Equal(t, http.StatusOK, r.get("/", session).Status)

	moved := newRig(t, rigOptions{github: true, idp: idp, clock: r.clock, people: []auth.Person{
		{Name: "ana", GitHubID: 2002, Role: auth.RoleAdmin},
	}})
	require.Equal(t, http.StatusForbidden, moved.get("/", session).Status, "the person now has another id")
}

func TestOIDCDefaultClaimIsTheSubject(t *testing.T) {
	people := []auth.Person{{Name: "vera", Login: "sub-vera", Role: auth.RoleViewer}}
	r := newRig(t, rigOptions{claim: "default", people: people})
	s := r.start("/")
	c := r.claimsFor(s, "vera@example.com")
	c["sub"] = "sub-vera"
	require.Equal(t, http.StatusSeeOther, r.finish(s, grant{claims: c}).Status, "an empty claim means sub")

	// The email of a listed person is not a way in: it is not the identity this gate matches.
	people = []auth.Person{{Name: "vera", Login: "vera@example.com", Role: auth.RoleViewer}}
	r = newRig(t, rigOptions{claim: "default", people: people})
	s = r.start("/")
	require.Equal(t, http.StatusForbidden, r.finish(s, grant{claims: r.claimsFor(s, "vera@example.com")}).Status)
}

func TestUnreachableProviderFailsClosed(t *testing.T) {
	idp := newFakeIDP(t)
	r := newRig(t, rigOptions{idp: idp})
	idp.srv.Close()
	resp := r.get("/auth/start")
	require.Equal(t, http.StatusBadGateway, resp.Status)
	require.Nil(t, cookieNamed(resp.Cookies, sessionCookie))
	requireRefused(t, r.get("/"), "the board stays closed")
}
