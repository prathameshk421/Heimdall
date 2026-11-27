package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// choiceServer records the tool_choice of each call and answers it with the next reply in order.
type choiceServer struct {
	mu      sync.Mutex
	choices []string
	replies []choiceReply
}

type choiceReply struct {
	status      int
	contentType string
	body        []byte
}

func (s *choiceServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			ToolChoice *string `json:"tool_choice"`
		}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		choice := "<none>"
		if body.ToolChoice != nil {
			choice = *body.ToolChoice
		}
		s.choices = append(s.choices, choice)
		reply := s.replies[min(len(s.choices), len(s.replies))-1]
		s.mu.Unlock()
		if reply.contentType != "" {
			w.Header().Set("Content-Type", reply.contentType)
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write(reply.body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func toolRequest() ports.ModelRequest {
	choice := ports.ModelToolChoiceRequired
	req := chatAnswerRequest()
	req.Tools = []ports.ModelTool{{Name: "read_code", Parameters: json.RawMessage(`{"type":"object"}`)}}
	req.ToolChoice = choice
	return req
}

const htmlBadRequest = `<html><body><h1>400 Bad request</h1> Your browser sent an invalid request. </body></html>`

func TestChatSendsAutoWhenTheEndpointIsNotKnownToTakeRequired(t *testing.T) {
	s := &choiceServer{replies: []choiceReply{{status: http.StatusOK, body: chatFixture(t, "answer.json")}}}
	client := ChatClient{BaseURL: s.start(t).URL, Prices: chatPrice()}

	got, err := client.Complete(context.Background(), toolRequest())

	require.NoError(t, err)
	require.Equal(t, []string{"auto"}, s.choices, "without the capability flag the adapter never sends required")
	require.Equal(t, ports.ModelToolChoiceAuto, got.ToolChoice)
}

func TestChatFallsBackToAutoOnceWhenRequiredIsRefused(t *testing.T) {
	s := &choiceServer{replies: []choiceReply{
		{status: http.StatusBadRequest, contentType: "application/json",
			body: []byte(`{"error":{"message":"tool_choice required is not supported","type":"invalid_request_error"}}`)},
		{status: http.StatusOK, body: chatFixture(t, "tool_calls.json")},
	}}
	client := ChatClient{BaseURL: s.start(t).URL, RequiredToolChoice: true, Prices: chatPrice()}

	got, err := client.Complete(context.Background(), toolRequest())

	require.NoError(t, err)
	require.Equal(t, []string{"required", "auto"}, s.choices)
	require.Equal(t, ports.ModelToolChoiceAuto, got.ToolChoice)
	require.Contains(t, got.Fallback, "HTTP 400", "the response records that the adapter fell back, and why")
	require.Contains(t, got.Fallback, "not supported")
	require.Len(t, got.ToolCalls, 2)
}

func TestChatFallsBackOnlyOnceAndKeepsBothRefusals(t *testing.T) {
	s := &choiceServer{replies: []choiceReply{{status: http.StatusBadRequest, contentType: "text/html", body: []byte(htmlBadRequest)}}}
	client := ChatClient{BaseURL: s.start(t).URL, RequiredToolChoice: true}

	_, err := client.Complete(context.Background(), toolRequest())

	require.Equal(t, []string{"required", "auto"}, s.choices, "one fallback, never a loop")
	var modelErr *ports.ModelError
	require.ErrorAs(t, err, &modelErr)
	require.Equal(t, http.StatusBadRequest, modelErr.Status)
	require.Contains(t, err.Error(), "tool_choice required", "the error says the fallback ran")
}

func TestChatDoesNotFallBackOnAnErrorThatIsNotARefusedRequest(t *testing.T) {
	for _, reply := range []choiceReply{
		{status: http.StatusInternalServerError, body: chatFixture(t, "server_error.json")},
		{status: http.StatusTooManyRequests, body: chatFixture(t, "rate_limited.json")},
		{status: http.StatusUnauthorized, body: chatFixture(t, "auth.json")},
		{status: http.StatusBadRequest, body: chatFixture(t, "context_length.json")},
	} {
		s := &choiceServer{replies: []choiceReply{reply}}
		client := ChatClient{BaseURL: s.start(t).URL, RequiredToolChoice: true}

		_, err := client.Complete(context.Background(), toolRequest())

		require.Error(t, err)
		require.Equal(t, []string{"required"}, s.choices, "HTTP %d is not a refusal of tool_choice", reply.status)
	}
}

func TestChatRecordsStatusContentTypeAndRequestSizeOnARefusedCall(t *testing.T) {
	const marker = "SECRET-PROMPT-MARKER"
	long := htmlBadRequest + strings.Repeat("x", 2000) + "TAIL-OF-THE-BODY"
	s := &choiceServer{replies: []choiceReply{{status: http.StatusBadRequest, contentType: "text/html; charset=utf-8", body: []byte(long)}}}
	client := ChatClient{BaseURL: s.start(t).URL, Secrets: probeSecrets{"key": "sk-secret-key"}, KeyName: "key"}
	req := chatAnswerRequest()
	req.Messages[1].Content = marker
	want, err := chatRequestBody(req, "")
	require.NoError(t, err)

	_, err = client.Complete(context.Background(), req)

	var modelErr *ports.ModelError
	require.ErrorAs(t, err, &modelErr)
	require.Equal(t, http.StatusBadRequest, modelErr.Status)
	require.Equal(t, "text/html; charset=utf-8", modelErr.ContentType)
	require.Equal(t, len(want), modelErr.RequestBytes)
	text := err.Error()
	require.Contains(t, text, "HTTP 400")
	require.Contains(t, text, "text/html")
	require.Contains(t, text, "request")
	require.NotContains(t, text, "sk-secret-key")
	require.NotContains(t, text, marker, "the request body never reaches the error")
	require.NotContains(t, text, "TAIL-OF-THE-BODY", "the response body is cut, never copied whole")
}
