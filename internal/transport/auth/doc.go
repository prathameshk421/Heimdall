// Package auth signs people in to the web board and guards its routes (WP A9). The board is sold to security
// teams, so nothing on it is readable without a session, and a session is only for a person in the role map.
//
// # Purpose
//
// The authorization code flow with PKCE against one provider: a generic OpenID Connect issuer (Okta, Google,
// Keycloak, Entra ID) or GitHub OAuth. GitHub is not an OIDC provider, so its login comes from GET /user. A proven
// login maps to a person of [board_auth.people] in heimdall.toml, with the role viewer, approver or admin. The
// package holds no business rule; it decides only who is reading.
//
// # API
//
//   - New(Config) (*Gate, error): checks the config and builds the gate. It does not call the provider: an OIDC
//     issuer is discovered on the first sign-in and kept once that succeeds.
//   - Gate.Mount(next): serves /auth/login (the page), /auth/start, /auth/callback, GET and POST /auth/logout and
//     /auth/assets/login.css. Every other path goes to next.
//   - Gate.Require(least Role): the guard of a route. No session: 303 to the login page for a page request (Accept
//     text/html), 401 otherwise. A session whose person is not in the role map, or has a lower role: 403. An unsafe
//     method also needs a same-origin request (http.CrossOriginProtection) and the session's CSRF token.
//   - FromContext(ctx) (Principal, bool): the signed-in person, their role and their CSRF token.
//   - Off(): dev mode. Every request is the admin "developer". CheckLoopback(addr) refuses a listener other
//     machines can reach; the server calls it on its real listener.
//   - ParseRole, ParseSessionKey: read the role names and the 32-byte base64 key of heimdall.toml.
//
// # Invariants
//
//   - Default deny. Mount and Require open only the login page, its style and the sign-in steps. Every board route,
//     the assets and the live stream included, sits behind Require (dashboard.Options.Access is required).
//   - Cookies are __Host- prefixed, HttpOnly, Secure, SameSite=Lax, Path=/ and host-only. Their values are JSON
//     sealed with AES-256-GCM; the cookie name is the additional data, so a login cookie never opens as a session.
//   - A session has an absolute life of SessionTTL (5 minutes to 24 hours) and is never extended. Each sign-in makes
//     a new session id and CSRF token, so a session cannot be fixed in advance.
//   - The role map is read on every request. A person removed, demoted or given another login after a restart loses
//     the access at once, even with a cookie sealed before.
//   - The login cookie lives 10 minutes and is cleared at the callback. The state is compared in constant time. The
//     OIDC ID token is checked for signature (the issuer's JWKS), issuer, audience, expiry and nonce. An email
//     counts only when email_verified is true. A subject is matched with its case; an email and a GitHub login
//     without it.
//   - The return path after sign-in is a path on this server only (safeNext): no scheme, host, //, backslash,
//     control character or fragment, and never an /auth path. Anything else returns to /.
//   - Sign-out is a POST with the CSRF token. It revokes the session id in this process until the session would
//     have expired, and clears the cookie.
//   - No token, code, secret or login is put in a log line or a page. A refused login is logged without the login.
//
// # Failure modes
//
//   - The provider is unreachable: 502 at /auth/start or /auth/callback, and the board stays closed.
//   - A broken, foreign or expired login state, a state mismatch, a provider error or no code: 400.
//   - A refused code, a bad ID token, no verified email or a failed GitHub user call: 401, no session.
//   - A proven login that is not in the role map: 403, no session.
//   - A tampered, foreign-key, expired or revoked session cookie: as no session.
//
// # Tests
//
// board_test.go runs the whole board (the dashboard behind Require) against fake_idp_test.go, an OIDC provider and
// GitHub OAuth server in one httptest server that checks the client secret, redirect URL, PKCE verifier and single
// use of a code. It covers every board route without a session, tampered and expired cookies, open redirects, the
// unknown login, the role order, the role map read per request, CSRF and origin checks, sign-out, the login state,
// the ID token checks and a forged signature, the GitHub flow and an unreachable provider. new_test.go covers the
// config refusals, the role and key parsers and dev mode.
package auth
