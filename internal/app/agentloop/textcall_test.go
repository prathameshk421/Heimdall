package agentloop

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// textTools is the registry the parser checks names and arguments against.
func textTools() map[string]ports.ModelTool {
	return map[string]ports.ModelTool{
		"read_code": {Name: "read_code", Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string"},"start":{"type":"integer"}},"required":["path"],"additionalProperties":false}`)},
		"finish_review": {Name: "finish_review", Parameters: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}}}`)},
		"no_schema":     {Name: "no_schema"},
	}
}

func parsed(t *testing.T, content string) []ports.ModelToolCall {
	t.Helper()
	return textCalls(content, textTools(), "t1")
}

func TestTextCallsReadsDeepSeekDSMLBlocks(t *testing.T) {
	content := "I will read the file first.\n\n<｜DSML｜function_calls>\n" +
		"<｜DSML｜invoke name=\"read_code\">\n" +
		"<｜DSML｜parameter name=\"path\" string=\"true\">mock/mock.go</｜DSML｜parameter>\n" +
		"<｜DSML｜parameter name=\"start\" string=\"false\">500</｜DSML｜parameter>\n" +
		"</｜DSML｜invoke>\n" +
		"<｜DSML｜invoke name=\"finish_review\">\n" +
		"<｜DSML｜parameter name=\"summary\" string=\"true\">say \"done\"</｜DSML｜parameter>\n" +
		"</｜DSML｜invoke>\n</｜DSML｜function_calls>"
	got := parsed(t, content)
	require.Len(t, got, 2)
	assert.Equal(t, "read_code", got[0].Name)
	assert.JSONEq(t, `{"path":"mock/mock.go","start":500}`, got[0].Arguments)
	assert.Equal(t, "finish_review", got[1].Name)
	assert.JSONEq(t, `{"summary":"say \"done\""}`, got[1].Arguments)
	assert.Equal(t, "t1-1", got[0].ID)
	assert.Equal(t, "t1-2", got[1].ID)
}

func TestTextCallsReadsTheToolCallsWrapperAndAsciiBars(t *testing.T) {
	content := "<|DSML|tool_calls><|DSML|invoke name=\"read_code\"><|DSML|parameter name=\"path\" string=\"true\">a.go" +
		"</|DSML|parameter></|DSML|invoke></|DSML|tool_calls>"
	got := parsed(t, content)
	require.Len(t, got, 1)
	assert.JSONEq(t, `{"path":"a.go"}`, got[0].Arguments)
}

func TestTextCallsReadsAParameterWithoutAStringFlagFromTheSchema(t *testing.T) {
	content := "<｜DSML｜invoke name=\"read_code\"><｜DSML｜parameter name=\"path\">a.go</｜DSML｜parameter>" +
		"<｜DSML｜parameter name=\"start\">12</｜DSML｜parameter></｜DSML｜invoke>"
	got := parsed(t, content)
	require.Len(t, got, 1)
	assert.JSONEq(t, `{"path":"a.go","start":12}`, got[0].Arguments)
}

func TestTextCallsReadsAFencedJSONCall(t *testing.T) {
	for name, content := range map[string]string{
		"object":           "Calling now.\n```json\n{\"name\": \"read_code\", \"arguments\": {\"path\": \"a.go\"}}\n```",
		"string arguments": "```json\n{\"name\":\"read_code\",\"arguments\":\"{\\\"path\\\":\\\"a.go\\\"}\"}\n```",
		"parameters key":   "```\n{\"name\":\"read_code\",\"parameters\":{\"path\":\"a.go\"}}\n```",
		"array":            "```json\n[{\"name\":\"read_code\",\"arguments\":{\"path\":\"a.go\"}}]\n```",
	} {
		got := parsed(t, content)
		require.Len(t, got, 1, name)
		assert.Equal(t, "read_code", got[0].Name, name)
		assert.JSONEq(t, `{"path":"a.go"}`, got[0].Arguments, name)
	}
}

func TestTextCallsReadsDeepSeekV3ToolTokens(t *testing.T) {
	content := "<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>function<｜tool▁sep｜>read_code\n```json\n{\"path\":\"a.go\"}\n```" +
		"<｜tool▁call▁end｜><｜tool▁calls▁end｜>"
	got := parsed(t, content)
	require.Len(t, got, 1)
	assert.Equal(t, "read_code", got[0].Name)
	assert.JSONEq(t, `{"path":"a.go"}`, got[0].Arguments)
}

func TestTextCallsTreatsAnythingElseAsText(t *testing.T) {
	for name, content := range map[string]string{
		"plain text":          "I found no bug. The change is fine.",
		"empty":               "",
		"unknown tool":        "```json\n{\"name\":\"delete_repo\",\"arguments\":{}}\n```",
		"unknown dsml tool":   "<｜DSML｜invoke name=\"rm\"><｜DSML｜parameter name=\"path\" string=\"true\">/</｜DSML｜parameter></｜DSML｜invoke>",
		"bad json":            "<tool_call>{\"name\":\"read_code\",\"arguments\":{\"path\":}</tool_call>",
		"arguments not obj":   "```json\n{\"name\":\"read_code\",\"arguments\":[1,2]}\n```",
		"missing required":    "```json\n{\"name\":\"read_code\",\"arguments\":{\"start\":3}}\n```",
		"wrong type":          "```json\n{\"name\":\"read_code\",\"arguments\":{\"path\":7}}\n```",
		"extra property":      "```json\n{\"name\":\"read_code\",\"arguments\":{\"path\":\"a\",\"rm\":true}}\n```",
		"dsml bad json value": "<｜DSML｜invoke name=\"read_code\"><｜DSML｜parameter name=\"path\" string=\"false\">a.go</｜DSML｜parameter></｜DSML｜invoke>",
		"json not a call":     "Here is the data:\n```json\n{\"path\":\"a.go\"}\n```",
		"one bad of two": "<tool_call>{\"name\":\"read_code\",\"arguments\":{\"path\":\"a\"}}</tool_call>" +
			"<tool_call>{\"name\":\"nope\",\"arguments\":{}}</tool_call>",
	} {
		assert.Nil(t, parsed(t, content), name)
	}
}

func FuzzTextCalls(f *testing.F) {
	for _, seed := range []string{
		"<｜DSML｜invoke name=\"read_code\"><｜DSML｜parameter name=\"path\" string=\"true\">a</｜DSML｜parameter></｜DSML｜invoke>",
		"```json\n{\"name\":\"read_code\",\"arguments\":{\"path\":\"a\"}}\n```",
		"<tool_call>{\"name\":\"finish_review\",\"arguments\":{}}</tool_call>",
		"<｜tool▁call▁begin｜>function<｜tool▁sep｜>read_code\n```json\n{\"path\":\"a\"}\n```<｜tool▁call▁end｜>",
		"<｜DSML｜invoke name=\"", "```", "<tool_call>", "\xff\xfe",
	} {
		f.Add(seed)
	}
	tools := textTools()
	f.Fuzz(func(t *testing.T, content string) {
		calls := textCalls(content, tools, "f")
		if len(calls) > maxTextCalls {
			t.Fatalf("%d calls passed the bound", len(calls))
		}
		for _, c := range calls {
			if _, ok := tools[c.Name]; !ok {
				t.Fatalf("accepted unknown tool %q", c.Name)
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal([]byte(c.Arguments), &obj); err != nil || obj == nil {
				t.Fatalf("accepted arguments that are not a JSON object: %q", c.Arguments)
			}
		}
	})
}
