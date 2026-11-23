package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newWriteClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{
		BaseURL:     baseURL,
		Project:     "KAN",
		EmailSecret: "email",
		TokenSecret: "token",
	}, fakeSecrets{"email": emailValue, "token": tokenValue}, &clock{at: now}, func(context.Context, time.Duration) error { return nil }, halfRandom{}, nil)
	require.NoError(t, err)
	return c
}

func TestCommentPostsADFAndDeduplicatesByKey(t *testing.T) {
	t.Parallel()
	var calls int
	var gotMethod, gotPath string
	var gotBody map[string]any
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"10001"}`))
	})

	c := newWriteClient(t, f.srv.URL)
	require.NoError(t, c.Comment(context.Background(), "KAN-5", "KAN-5/tracker_commit/abc/r0", "Tests committed."))
	require.NoError(t, c.Comment(context.Background(), "KAN-5", "KAN-5/tracker_commit/abc/r0", "Tests committed."))
	require.Equal(t, 1, calls, "same effect key is not posted twice")
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/rest/api/3/issue/KAN-5/comment", gotPath)
	require.NotNil(t, gotBody["body"])
}

func TestTransitionLooksUpMatchingStatusAndPostsID(t *testing.T) {
	t.Parallel()
	var postID, postPath string
	var posts int
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"transitions":[{"id":"31","name":"Done","to":{"name":"Done"}}]}`))
			return
		}
		posts++
		postPath = r.URL.Path
		var body struct {
			Transition struct {
				ID string `json:"id"`
			} `json:"transition"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		postID = body.Transition.ID
		w.WriteHeader(http.StatusNoContent)
	})

	c := newWriteClient(t, f.srv.URL)
	require.NoError(t, c.Transition(context.Background(), "KAN-5", "KAN-5/tracker_merge/abc/r0", "done"))
	require.NoError(t, c.Transition(context.Background(), "KAN-5", "KAN-5/tracker_merge/abc/r0", "done"))
	require.Equal(t, "/rest/api/3/issue/KAN-5/transitions", postPath)
	require.Equal(t, "31", postID)
	require.Equal(t, 1, posts, "same effect key is not transitioned twice")
}
