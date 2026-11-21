// Package auth signs people in to the web board and guards its routes (WP A9). The board is sold to security
// teams, so nothing on it is readable without a session, and a session is only for a person in the role map.
//
// # Purpose
//
// The authorization code flow with PKCE against one provider: a generic OpenID Connect issuer (Okta, Google,
// Keycloak, Entra ID) or GitHub OAuth. GitHub is not an OIDC provider, so its identity comes from GET /user. A proven
// identity maps to a person of [board_auth.people] in heimdall.toml, with the role viewer, approver or admin. The
// package holds no business rule; it decides only who is reading.
//
// # Identity
//
// A person is matched by a stable identity, never by a name that can change.
//
//   - GitHub: the numeric user id (GET /user "id", read as an int64). Person.GitHubID, github_id in the toml. A
//     GitHub login can be renamed, and a freed login can be claimed by another account, which would then inherit the
//     person's role. The id never changes. The login is read for display only (Principal.Login) and is never
//     matched. An admin finds an id with `gh api user --jq .id`, or by reading "id" at
//     https://api.github.com/users/<login>. A GitHub person with no id is refused at startup, by name. There is no
//     fallback to the login.
//   - OIDC: the claim named by Claim, "sub" by default (an empty Claim means sub). The subject is stable for one
//     issuer. An email can be reassigned to another person, so it counts only when the config chooses the "email"
//     claim, and then only with email_verified true.
//
// # API
//
//   - New(Config) (*Gate, error): checks the config and builds the gate. It does not call the provider: an OIDC
//     issuer is discovered on the first sign-in and kept once that succeeds.
//   - Gate.Mount(next): serves /auth/login (the page), /auth/start, /auth/callback, GET and POST /auth/logout and
//     /auth/assets/login.css. Every other path goes to next.
//   - Gate.Require(least Role): the guard of a route. No session: 303 to the login page for a page request (Accept
//     text/html), 401 otherwise. A session whose person is not in the role map, whose identity is not the one the
//     map lists for that person, or who has a lower role: 403. An unsafe
//     method also needs a same-origin request (http.CrossOriginProtection) and the session's CSRF token.
//   - FromContext(ctx) (Principal, bool): the signed-in person, their role and their CSRF token.
//     Principal.Login is the name the provider showed. It is for display only.
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
//   - The role map is read on every request. A person removed, demoted or given another identity after a restart
//     loses the access at once, even with a cookie sealed before. The session holds the proven identity key (a GitHub
//     id as text, or the OIDC claim) and Require compares it with the key the map lists for that person.
//   - A session of an earlier shape (one that carried a "login" and no identity) never opens. The sealed JSON is
//     decoded with DisallowUnknownFields, and a session with an empty identity is refused as well. So a cookie sealed
//     before the identity change cannot open as a different person. People sign in again once.
//   - The login cookie lives 10 minutes and is cleared at the callback. The state is compared in constant time. The
//     OIDC ID token is checked for signature (the issuer's JWKS), issuer, audience, expiry and nonce. An email
//     counts only when email_verified is true. A subject is matched with its case and must not be empty; an
//     email without case. A GitHub id must be a positive whole number: a missing, zero, negative, text, fractional
//     or oversized id is refused (401), and no float ever rounds an id onto another account.
//   - The return path after sign-in is a path on this server only (safeNext): no scheme, host, //, backslash,
//     control character or fragment, and never an /auth path. Anything else returns to /.
//   - Sign-out is a POST with the CSRF token. It revokes the session id in this process until the session would
//     have expired, and clears the cookie.
//   - No token, code, secret, id or login is put in a log line or a page. A refused sign-in is logged without the
//     identity.
//
// # Failure modes
//
//   - The provider is unreachable: 502 at /auth/start or /auth/callback, and the board stays closed.
//   - A broken, foreign or expired login state, a state mismatch, a provider error or no code: 400.
//   - A refused code, a bad ID token, no verified email, a failed GitHub user call or a GitHub answer with
//     no usable numeric id: 401, no session.
//   - A proven identity that is not in the role map: 403, no session. A GitHub account that took a freed login of a
//     listed person has another id, so it lands here.
//   - A tampered, foreign-key, expired or revoked session cookie, or one of an earlier shape: as no session.
//   - Startup: a GitHub person with no github_id (or with a login, or a duplicate id), or an OIDC person with a
//     GitHub id or no login: New refuses the config and names the person.
//
// # Tests
//
// board_test.go runs the whole board (the dashboard behind Require) against fake_idp_test.go, an OIDC provider and
// GitHub OAuth server in one httptest server that checks the client secret, redirect URL, PKCE verifier and single
// use of a code. It covers every board route without a session, tampered and expired cookies, open redirects, the
// unknown identity, the role order, the role map read per request, CSRF and origin checks, sign-out, the login
// state, the ID token checks and a forged signature, the GitHub flow by numeric id (a renamed login keeps the person,
// a reused login with another id is refused, a missing, zero, negative, text, fractional or oversized id is refused),
// the OIDC default claim and an unreachable provider. session_shape_test.go seals cookies by hand to prove a session
// of the old shape does not open. new_test.go covers the config refusals (a GitHub person with no id included), the
// role and key parsers and dev mode.
package auth
