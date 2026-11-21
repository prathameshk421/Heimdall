package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestGetCommitReadsTreeAndParents(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", repoRoot+"/git/commits/"+shaHead, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"sha": shaHead, "tree": map[string]string{"sha": shaTree},
			"parents": []map[string]string{{"sha": shaNew}}})
	})
	got, err := env.x.GetCommit(t.Context(), testRepo, shaHead)
	require.NoError(t, err)
	require.Equal(t, ports.GitCommit{Tree: shaTree, Parents: []id.SHA{shaNew}}, got)
	env.wantAuth(t)
}

func TestGetCommitRefusesABadHashInTheReply(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", repoRoot+"/git/commits/"+shaHead, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tree": map[string]string{"sha": "../x"}})
	})
	_, err := env.x.GetCommit(t.Context(), testRepo, shaHead)
	require.Error(t, err)
}

func treeRoute(env *actionEnv, entries []map[string]string, truncated bool) {
	env.f.route("GET", repoRoot+"/git/trees/"+shaTree, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"sha": shaTree, "tree": entries, "truncated": truncated})
	})
}

func TestGetTreeIsRecursiveAndChecksEntries(t *testing.T) {
	env := newActionEnv(t)
	treeRoute(env, []map[string]string{
		{"path": "changes", "mode": "040000", "type": "tree", "sha": shaNew},
		{"path": "changes/pr.jsonl", "mode": "100644", "type": "blob", "sha": shaHead},
	}, false)
	got, err := env.x.GetTree(t.Context(), testRepo, shaTree, 10)
	require.NoError(t, err)
	require.Equal(t, []ports.GitTreeItem{
		{Path: "changes", Mode: "040000", Type: "tree", SHA: shaNew},
		{Path: "changes/pr.jsonl", Mode: "100644", Type: "blob", SHA: shaHead},
	}, got)
	reqs := env.f.requests()
	require.Equal(t, "recursive=1", reqs[len(reqs)-1].Query)
}

func TestGetTreeRefusesTruncatedOversizedAndBadEntries(t *testing.T) {
	ok := map[string]string{"path": "a", "mode": "100644", "type": "blob", "sha": shaHead}

	env := newActionEnv(t)
	treeRoute(env, []map[string]string{ok}, true)
	_, err := env.x.GetTree(t.Context(), testRepo, shaTree, 10)
	require.ErrorIs(t, err, ports.ErrObjectTooLarge, "truncated")

	env = newActionEnv(t)
	treeRoute(env, []map[string]string{ok, ok, ok}, false)
	_, err = env.x.GetTree(t.Context(), testRepo, shaTree, 2)
	require.ErrorIs(t, err, ports.ErrObjectTooLarge, "over the entry limit")

	for name, e := range map[string]map[string]string{
		"dot dot path": {"path": "../x", "mode": "100644", "type": "blob", "sha": shaHead},
		"empty path":   {"path": "", "mode": "100644", "type": "blob", "sha": shaHead},
		"bad hash":     {"path": "a", "mode": "100644", "type": "blob", "sha": "zz"},
		"unknown type": {"path": "a", "mode": "100644", "type": "tag", "sha": shaHead},
	} {
		env = newActionEnv(t)
		treeRoute(env, []map[string]string{e}, false)
		_, err = env.x.GetTree(t.Context(), testRepo, shaTree, 10)
		require.Error(t, err, name)
		require.NotErrorIs(t, err, ports.ErrObjectTooLarge, name)
	}
}

func blobRoute(env *actionEnv, content []byte, size int64) {
	env.f.route("GET", repoRoot+"/git/blobs/"+shaHead, func(w http.ResponseWriter, _ *http.Request) {
		enc := base64.StdEncoding.EncodeToString(content)
		// GitHub wraps base64 content in lines.
		if len(enc) > 4 {
			enc = enc[:4] + "\n" + enc[4:]
		}
		writeJSON(w, http.StatusOK, map[string]any{"sha": shaHead, "encoding": "base64", "size": size, "content": enc})
	})
}

func TestGetBlobDecodesAndChecksTheStatedSize(t *testing.T) {
	env := newActionEnv(t)
	blobRoute(env, []byte("hello world"), 11)
	got, err := env.x.GetBlob(t.Context(), testRepo, shaHead, 100)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(got))

	env = newActionEnv(t)
	blobRoute(env, []byte("hello"), 1<<40)
	_, err = env.x.GetBlob(t.Context(), testRepo, shaHead, 100)
	require.ErrorIs(t, err, ports.ErrObjectTooLarge, "a stated size over the limit is refused before decoding")

	env = newActionEnv(t)
	blobRoute(env, []byte("hello"), 3)
	_, err = env.x.GetBlob(t.Context(), testRepo, shaHead, 100)
	require.Error(t, err, "a size that does not match the content")
	require.NotErrorIs(t, err, ports.ErrObjectTooLarge)
}

// swapEnv answers the repository read and the GraphQL call. answer writes the GraphQL reply.
func swapEnv(t *testing.T, answer func(w http.ResponseWriter)) *actionEnv {
	t.Helper()
	env := newActionEnv(t)
	env.f.route("GET", repoRoot, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"node_id": "R_kgDOTEST", "default_branch": "main"})
	})
	env.f.route("POST", "/graphql", func(w http.ResponseWriter, _ *http.Request) { answer(w) })
	return env
}

const opaque = "Something went wrong while executing your query on 2026-11-21T04:00:00Z. Please include `AB12:3456` when reporting this issue."

func TestSwapRefSendsTheVerifiedMutation(t *testing.T) {
	env := swapEnv(t, func(w http.ResponseWriter) {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"updateRefs": map[string]any{"clientMutationId": nil}}})
	})
	require.NoError(t, env.x.SwapRef(t.Context(), testRepo, stateRef, shaHead, shaNew))
	require.NoError(t, env.x.SwapRef(t.Context(), testRepo, stateRef, shaNew, shaTree))

	var in struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(lastRequest(t, env, "POST", "/graphql"), &in))
	require.Equal(t, updateRefsMutation, in.Query)
	require.Equal(t, map[string]any{"r": "R_kgDOTEST", "n": stateRef, "b": shaNew, "a": shaTree, "f": false}, in.Variables)
	require.Equal(t, 1, env.f.count("GET", repoRoot), "the node id is read once")
	require.Zero(t, env.f.count("PATCH", repoRoot+"/git/refs/heimdall/state"), "the swap never uses REST PATCH")
	env.wantAuth(t)
}

func lastRequest(t *testing.T, env *actionEnv, method, path string) []byte {
	t.Helper()
	var body []byte
	for _, r := range env.f.requests() {
		if r.Method == method && r.Path == path {
			body = r.Body
		}
	}
	require.NotNil(t, body, "no %s %s", method, path)
	return body
}

func TestSwapRefOutcomes(t *testing.T) {
	cases := map[string]struct {
		status int
		reply  any
		want   error
	}{
		"opaque refusal of a stale beforeOid": {200, map[string]any{"errors": []map[string]string{{"message": opaque}}}, ports.ErrRefSwapUnconfirmed},
		"non fast forward":                    {200, map[string]any{"errors": []map[string]string{{"type": "UNPROCESSABLE", "message": "rejecting non-fast forward update"}}}, ports.ErrRefSwapUnconfirmed},
		"no result":                           {200, map[string]any{"data": map[string]any{"updateRefs": nil}}, ports.ErrRefSwapUnconfirmed},
		"not json":                            {200, "<html>", ports.ErrRefSwapUnconfirmed},
		"server error":                        {502, map[string]string{"message": "bad gateway"}, ports.ErrUnavailable},
		"forbidden in graphql":                {200, map[string]any{"errors": []map[string]string{{"type": "FORBIDDEN", "message": "Resource not accessible by integration"}}}, ports.ErrUnauthorized},
		"forbidden over http":                 {403, map[string]string{"message": "Resource not accessible by integration"}, ports.ErrUnauthorized},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := swapEnv(t, func(w http.ResponseWriter) { writeJSON(w, c.status, c.reply) })
			err := env.x.SwapRef(t.Context(), testRepo, stateRef, shaHead, shaNew)
			require.ErrorIs(t, err, c.want)
			if errors.Is(c.want, ports.ErrUnauthorized) {
				require.NotErrorIs(t, err, ports.ErrRefSwapUnconfirmed, "FORBIDDEN must fail closed, not be re-read")
			}
			if c.status == 502 {
				require.ErrorIs(t, err, ports.ErrRefSwapUnconfirmed, "a lost reply is unconfirmed")
			}
			require.NotContains(t, err.Error(), "AB12", "the GraphQL message is never repeated")
			require.NotContains(t, err.Error(), "Resource not accessible")
		})
	}
}

func TestSwapRefRefusesBadInputBeforeAnyRequest(t *testing.T) {
	env := swapEnv(t, func(w http.ResponseWriter) { writeJSON(w, http.StatusOK, map[string]any{}) })
	ctx := context.Background()
	require.Error(t, env.x.SwapRef(ctx, testRepo, "heimdall/state", shaHead, shaNew))
	require.Error(t, env.x.SwapRef(ctx, testRepo, stateRef, "", shaNew), "beforeOid is required")
	require.Error(t, env.x.SwapRef(ctx, testRepo, stateRef, shaHead, "nope"))
	require.Error(t, env.x.SwapRef(ctx, "bad", stateRef, shaHead, shaNew))
	require.Empty(t, env.f.requests())
}

func TestGraphQLTargetFollowsTheRESTRoot(t *testing.T) {
	cases := map[string]string{
		"https://api.github.com":          "https://api.github.com/graphql",
		"https://ghe.example.com/api/v3":  "https://ghe.example.com/api/graphql",
		"https://ghe.example.com/api/v3/": "https://ghe.example.com/api/graphql",
	}
	for root, want := range cases {
		tok, err := NewStaticToken(staticTok)
		require.NoError(t, err)
		x, err := NewAction(ActionConfig{BaseURL: root, Tokens: tok, HTTPClient: http.DefaultClient, Clock: &fakeClock{}, Wait: (&fakeWait{clock: &fakeClock{}}).wait})
		require.NoError(t, err)
		require.Equal(t, want, x.graphqlTarget(), root)
		require.False(t, strings.HasSuffix(x.graphqlTarget(), "/v3/graphql"))
	}
}
