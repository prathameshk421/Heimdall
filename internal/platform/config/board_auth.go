package config

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BoardAuthMode says how the web board checks who is reading it.
type BoardAuthMode string

// The board modes. The empty mode means [board_auth] is absent, and the server does not serve the board at all.
const (
	BoardAuthGitHub BoardAuthMode = "github"
	BoardAuthOIDC   BoardAuthMode = "oidc"
	// BoardAuthOff serves the board with no login. It is for a developer laptop only: Load refuses it unless listen
	// is a loopback address.
	BoardAuthOff BoardAuthMode = "off"
)

const (
	defaultBoardSessionTTL = 8 * time.Hour
	minBoardSessionTTL     = 5 * time.Minute
	maxBoardSessionTTL     = 24 * time.Hour
	// boardCallbackPath is where the board edge serves the identity provider's answer.
	boardCallbackPath = "/auth/callback"
	maxBoardLogin     = 256
)

// BoardAuth is [board_auth]: the login and the roles of the web board. Every secret is a file name under SecretsDir.
type BoardAuth struct {
	Mode BoardAuthMode
	// Issuer is the OpenID Connect issuer URL. It is set only in oidc mode.
	Issuer             string
	ClientID           string
	ClientSecretSecret string
	// SessionKeySecret names the file that holds the 32-byte session key, base64 encoded.
	SessionKeySecret string
	// RedirectURL is the callback the identity provider sends the browser back to. Its path is /auth/callback.
	RedirectURL string
	SessionTTL  time.Duration
	// LoginClaim is the ID token claim that names a person in oidc mode: "sub" (the default, stable per issuer) or
	// "email" (an explicit choice; verified only; an email can be reassigned to another person).
	LoginClaim string
	// People are the only identities that may read the board, sorted by person name.
	People []BoardPerson
}

// BoardPerson maps one stable identity to a person of the people map and a board role: viewer, approver or admin.
// In github mode the identity is GitHubID (Login is empty). In oidc mode it is Login, the value of the login claim
// (GitHubID is zero).
type BoardPerson struct {
	Name     string
	Login    string
	GitHubID int64
	Role     string
}

// fileBoardAuth mirrors [board_auth].
type fileBoardAuth struct {
	Mode               *string                    `toml:"mode"`
	Issuer             *string                    `toml:"issuer"`
	ClientID           *string                    `toml:"client_id"`
	ClientSecretSecret *string                    `toml:"client_secret_secret"`
	SessionKeySecret   *string                    `toml:"session_key_secret"`
	RedirectURL        *string                    `toml:"redirect_url"`
	SessionTTL         *string                    `toml:"session_ttl"`
	LoginClaim         *string                    `toml:"login_claim"`
	People             map[string]fileBoardPerson `toml:"people"`
}

type fileBoardPerson struct {
	Login    *string `toml:"login"`
	GitHubID *int64  `toml:"github_id"`
	Role     *string `toml:"role"`
}

// addBoardAuth validates [board_auth]. It runs after cfg.Listen is set, because dev mode depends on it.
func addBoardAuth(cfg *Config, raw *fileBoardAuth) []error {
	if raw == nil {
		return nil
	}
	if raw.Mode == nil {
		return []error{errors.New("board_auth.mode is required")}
	}
	switch mode := BoardAuthMode(*raw.Mode); mode {
	case BoardAuthOff:
		return checkBoardOff(cfg, raw)
	case BoardAuthGitHub, BoardAuthOIDC:
		out, errs := checkBoardLogin(mode, raw)
		if len(errs) == 0 {
			cfg.BoardAuth = out
		}
		return errs
	default:
		return []error{fmt.Errorf("board_auth.mode must be github, oidc or off, not %q", *raw.Mode)}
	}
}

// checkBoardOff allows dev mode only on a loopback listener, and with no other key, so a leftover login setting is
// never silently ignored.
func checkBoardOff(cfg *Config, raw *fileBoardAuth) []error {
	var errs []error
	for _, k := range []struct {
		name string
		set  bool
	}{
		{"issuer", raw.Issuer != nil}, {"client_id", raw.ClientID != nil},
		{"client_secret_secret", raw.ClientSecretSecret != nil}, {"session_key_secret", raw.SessionKeySecret != nil},
		{"redirect_url", raw.RedirectURL != nil}, {"session_ttl", raw.SessionTTL != nil},
		{"login_claim", raw.LoginClaim != nil}, {"people", raw.People != nil},
	} {
		if k.set {
			errs = append(errs, fmt.Errorf("board_auth.%s is not used when mode is off", k.name))
		}
	}
	if !isLoopbackListen(cfg.Listen) {
		errs = append(errs, fmt.Errorf("board_auth.mode off needs a loopback listen address, not %q", cfg.Listen))
	}
	if len(errs) == 0 {
		cfg.BoardAuth = BoardAuth{Mode: BoardAuthOff}
	}
	return errs
}

// isLoopbackListen reports whether a host:port listens on this machine only. An empty or unspecified host listens
// on every interface.
func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	return err == nil && host != "" && isLoopbackHost(host)
}

// checkBoardLogin validates the github and oidc modes and returns the section.
func checkBoardLogin(mode BoardAuthMode, raw *fileBoardAuth) (BoardAuth, []error) {
	out := BoardAuth{Mode: mode}
	var errs []error
	if err := checkBoardClientID(raw.ClientID); err != nil {
		errs = append(errs, err)
	}
	if err := checkSecretKey("board_auth.client_secret_secret", raw.ClientSecretSecret); err != nil {
		errs = append(errs, err)
	}
	if err := checkSecretKey("board_auth.session_key_secret", raw.SessionKeySecret); err != nil {
		errs = append(errs, err)
	}
	if err := checkBoardRedirect(raw.RedirectURL); err != nil {
		errs = append(errs, err)
	}
	ttl, err := boardSessionTTL(raw.SessionTTL)
	if err != nil {
		errs = append(errs, err)
	}
	issuer, claim, ierrs := checkBoardIssuer(mode, raw)
	errs = append(errs, ierrs...)
	people, perrs := checkBoardPeople(mode, claim, raw.People)
	errs = append(errs, perrs...)
	if len(errs) > 0 {
		return BoardAuth{}, errs
	}
	out.Issuer, out.LoginClaim, out.SessionTTL, out.People = issuer, claim, ttl, people
	out.ClientID, out.ClientSecretSecret = *raw.ClientID, *raw.ClientSecretSecret
	out.SessionKeySecret, out.RedirectURL = *raw.SessionKeySecret, *raw.RedirectURL
	return out, nil
}

func checkBoardClientID(raw *string) error {
	if raw == nil || *raw == "" {
		return errors.New("board_auth.client_id is required")
	}
	if len(*raw) > maxBoardLogin || !printableNoSpace(*raw) {
		return errors.New("board_auth.client_id must be printable text of at most 256 characters with no space")
	}
	return nil
}

// checkBoardRedirect requires the absolute callback URL of this server: https (or http on loopback), path
// /auth/callback, and nothing the identity provider could match loosely.
func checkBoardRedirect(raw *string) error {
	if err := checkBaseURL("board_auth.redirect_url", raw); err != nil {
		return err
	}
	u, err := url.Parse(*raw)
	if err != nil {
		return fmt.Errorf("board_auth.redirect_url: %w", err)
	}
	switch {
	case u.User != nil:
		return errors.New("board_auth.redirect_url must not hold credentials")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return errors.New("board_auth.redirect_url must have no query or fragment")
	case u.Path != boardCallbackPath:
		return fmt.Errorf("board_auth.redirect_url must end in %s", boardCallbackPath)
	}
	return nil
}

func boardSessionTTL(raw *string) (time.Duration, error) {
	if raw == nil {
		return defaultBoardSessionTTL, nil
	}
	ttl, err := time.ParseDuration(*raw)
	if err != nil {
		return 0, fmt.Errorf("board_auth.session_ttl %q: %w", *raw, err)
	}
	if ttl < minBoardSessionTTL || ttl > maxBoardSessionTTL {
		return 0, fmt.Errorf("board_auth.session_ttl must be between 5m and 24h, not %s", ttl)
	}
	return ttl, nil
}

// checkBoardIssuer returns the issuer and the login claim. Both belong to oidc mode only.
func checkBoardIssuer(mode BoardAuthMode, raw *fileBoardAuth) (string, string, []error) {
	if mode == BoardAuthGitHub {
		var errs []error
		if raw.Issuer != nil {
			errs = append(errs, errors.New("board_auth.issuer is used only when mode is oidc"))
		}
		if raw.LoginClaim != nil {
			errs = append(errs, errors.New("board_auth.login_claim is used only when mode is oidc"))
		}
		return "", "", errs
	}
	var errs []error
	if err := checkBaseURL("board_auth.issuer", raw.Issuer); err != nil {
		errs = append(errs, err)
	}
	claim := "sub"
	if raw.LoginClaim != nil {
		claim = *raw.LoginClaim
	}
	if claim != "email" && claim != "sub" {
		errs = append(errs, fmt.Errorf("board_auth.login_claim must be email or sub, not %q", claim))
	}
	if len(errs) > 0 {
		return "", "", errs
	}
	return *raw.Issuer, claim, nil
}

// checkBoardPeople validates the role map. A person is named by a stable identity: the numeric GitHub id in github
// mode, the value of the login claim in oidc mode. An email is folded to lower case, because the board matches it
// that way, so two people never share an identity.
func checkBoardPeople(mode BoardAuthMode, claim string, raw map[string]fileBoardPerson) ([]BoardPerson, []error) {
	if len(raw) == 0 {
		return nil, []error{errors.New("board_auth.people must name at least one person")}
	}
	var errs []error
	var out []BoardPerson
	owner := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		where := "board_auth.people." + name
		p, perrs := checkBoardPerson(where, name, mode, raw[name])
		if len(perrs) > 0 {
			errs = append(errs, perrs...)
			continue
		}
		key, label := p.Login, "login"
		switch {
		case mode == BoardAuthGitHub:
			key, label = strconv.FormatInt(p.GitHubID, 10), "github_id"
		case claim == "email":
			key = strings.ToLower(key)
		}
		if other, dup := owner[key]; dup {
			errs = append(errs, fmt.Errorf("%s.%s is also the %s of board_auth.people.%s", where, label, label, other))
			continue
		}
		owner[key] = name
		out = append(out, p)
	}
	return out, errs
}

func checkBoardPerson(where, name string, mode BoardAuthMode, raw fileBoardPerson) (BoardPerson, []error) {
	var errs []error
	if name == "" || len(name) > 64 || !printableNoSpace(name) {
		errs = append(errs, fmt.Errorf("%s: person key must be a policy person name of at most 64 characters", where))
	}
	var p BoardPerson
	var err error
	if mode == BoardAuthGitHub {
		p.GitHubID, err = checkPersonGitHubID(where, raw)
	} else {
		p.Login, err = checkPersonLogin(where, raw)
	}
	if err != nil {
		errs = append(errs, err)
	}
	if raw.Role == nil || (*raw.Role != "viewer" && *raw.Role != "approver" && *raw.Role != "admin") {
		errs = append(errs, fmt.Errorf("%s.role must be viewer, approver or admin", where))
	}
	if len(errs) > 0 {
		return BoardPerson{}, errs
	}
	p.Name, p.Role = name, *raw.Role
	return p, nil
}

// checkPersonGitHubID requires the numeric GitHub id of a github mode person. It never falls back to a login: a
// GitHub login can be renamed, and a freed login can be claimed by another account.
func checkPersonGitHubID(where string, raw fileBoardPerson) (int64, error) {
	switch {
	case raw.GitHubID == nil:
		return 0, fmt.Errorf("%s.github_id is required in github mode: it is the number "+
			"(find it with: gh api user --jq .id), because a GitHub login can be renamed and then claimed by another "+
			"account, so the login is never matched", where)
	case raw.Login != nil:
		return 0, fmt.Errorf("%s.login is not used in github mode: remove it, the board matches %s.github_id only", where, where)
	case *raw.GitHubID <= 0:
		return 0, fmt.Errorf("%s.github_id must be a positive whole number", where)
	}
	return *raw.GitHubID, nil
}

// checkPersonLogin requires the login of an oidc mode person: printable text with no space.
func checkPersonLogin(where string, raw fileBoardPerson) (string, error) {
	login := raw.Login
	switch {
	case raw.GitHubID != nil:
		return "", fmt.Errorf("%s.github_id is used only when mode is github", where)
	case login == nil || *login == "":
		return "", fmt.Errorf("%s.login is required", where)
	case len(*login) > maxBoardLogin || !printableNoSpace(*login):
		return "", fmt.Errorf("%s.login must be printable text of at most 256 characters with no space", where)
	}
	return *login, nil
}

// printableNoSpace reports whether s is printable ASCII with no space.
func printableNoSpace(s string) bool {
	for _, r := range s {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}
