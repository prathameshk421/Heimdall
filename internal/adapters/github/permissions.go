package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Installation is what GitHub says the App's installation holds: the permission each name is granted, and the
// repositories the installation reaches. doctor compares it with RequiredPermissions.
type Installation struct {
	Permissions  map[string]string
	Repositories Repositories
}

// Repositories says which repositories the installation reaches. All is true when the installation reaches every
// repository. Otherwise Names lists each one as owner/name.
type Repositories struct {
	All   bool
	Names []string
}

// RequiredPermissions returns the permissions Heimdall needs, each with the lowest level that works (D161, TECH_SPEC
// 2.3). Every token the adapter mints is a subset of this set, so a permission outside it is never needed.
func RequiredPermissions() map[string]string {
	return map[string]string{
		"checks":        "write",
		"contents":      "write",
		"pull_requests": "write",
		"issues":        "write",
		"metadata":      "read",
	}
}

// levelRank orders GitHub's permission levels: an unknown level is 0, read 1, write 2 and admin 3.
func levelRank(level string) int {
	switch level {
	case "read":
		return 1
	case "write":
		return 2
	case "admin":
		return 3
	}
	return 0
}

// Installation reads the installation's real permissions and repository selection (V54). It sends GET
// /app/installations/{id} with the App JWT, because the settings page can differ from what the installation holds.
// A selected list is read with an installation token. Errors never carry a token, a key or a reply body.
func (t *TokenSource) Installation(ctx context.Context) (Installation, error) {
	jwt, err := t.appJWT(t.clock.Now())
	if err != nil {
		return Installation{}, err
	}
	path := fmt.Sprintf("/app/installations/%d", t.app.InstallationID)
	resp, err := t.client.call(ctx, request{method: http.MethodGet, path: path, auth: jwt})
	if err != nil {
		return Installation{}, fmt.Errorf("github installation: %w", err)
	}
	var out struct {
		Permissions         map[string]string `json:"permissions"`
		RepositorySelection string            `json:"repository_selection"`
	}
	if err := json.Unmarshal(resp.body, &out); err != nil {
		return Installation{}, errors.New("github installation: reply is not JSON")
	}
	for name, level := range out.Permissions {
		if levelRank(level) == 0 {
			return Installation{}, fmt.Errorf("github installation: permission %s has unknown level %q", name, level)
		}
	}
	inst := Installation{Permissions: out.Permissions}
	switch out.RepositorySelection {
	case "all":
		inst.Repositories = Repositories{All: true}
	case "selected":
		names, err := t.selectedRepositories(ctx)
		if err != nil {
			return Installation{}, err
		}
		inst.Repositories = Repositories{Names: names}
	default:
		return Installation{}, fmt.Errorf("github installation: unknown repository_selection %q", out.RepositorySelection)
	}
	return inst, nil
}

// selectedRepositories lists every repository of a selected installation, page by page. The next page must stay on
// the API origin, as for every list.
func (t *TokenSource) selectedRepositories(ctx context.Context) ([]string, error) {
	token, err := t.Reader(ctx)
	if err != nil {
		return nil, err
	}
	const path = "/installation/repositories"
	var names []string
	req := request{method: http.MethodGet, path: path, query: url.Values{"per_page": {perPage}}, auth: token}
	for {
		resp, err := t.client.call(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("github installation repositories: %w", err)
		}
		var page struct {
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		if err := json.Unmarshal(resp.body, &page); err != nil {
			return nil, errors.New("github installation repositories: reply is not JSON")
		}
		for _, r := range page.Repositories {
			names = append(names, r.FullName)
		}
		next, err := t.client.nextLink(resp.header.Get("Link"))
		if err != nil {
			return nil, err
		}
		if next == "" {
			return names, nil
		}
		req = request{method: http.MethodGet, path: path, auth: token, target: next}
	}
}
