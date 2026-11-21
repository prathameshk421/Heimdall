package agentloop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The bounds of the text tool call parser. An answer longer than maxTextCallBytes is not parsed, and an answer that
// holds more than maxTextCalls calls is text: the loop never runs a part of what the model wrote.
const (
	maxTextCallBytes = 64 << 10
	maxTextCalls     = 8
)

// The formats a model uses to write a tool call as text. Each pattern is RE2, so a match runs in linear time.
var (
	// DeepSeek DSML: <｜DSML｜invoke name="x"><｜DSML｜parameter name="p" string="true">v</｜DSML｜parameter></｜DSML｜invoke>,
	// inside a function_calls or tool_calls block. The bar is U+FF5C; an ASCII bar is read too.
	dsmlInvoke = regexp.MustCompile(`<[｜|]DSML[｜|]invoke\s+name="([^"]{1,128})"\s*>((?s:.*?))</[｜|]DSML[｜|]invoke>`)
	dsmlParam  = regexp.MustCompile(`<[｜|]DSML[｜|]parameter\s+name="([^"]{1,128})"(?:\s+string="(true|false)")?\s*>((?s:.*?))</[｜|]DSML[｜|]parameter>`)
	// DeepSeek V3 tool tokens: <｜tool▁call▁begin｜>function<｜tool▁sep｜>name ```json {args} ```<｜tool▁call▁end｜>.
	v3Call = regexp.MustCompile("<｜tool▁call▁begin｜>\\s*(?:function)?\\s*<｜tool▁sep｜>\\s*([A-Za-z0-9_.-]{1,128})\\s*```(?:json)?((?s:.*?))```\\s*<｜tool▁call▁end｜>")
	// Hermes and Qwen tags: <tool_call>{"name": "x", "arguments": {...}}</tool_call>.
	tagCall = regexp.MustCompile(`<tool_call>((?s:.*?))</tool_call>`)
	// A fenced JSON block: ```json {"name": "x", "arguments": {...}} ```, or an array of such objects.
	fencedJSON = regexp.MustCompile("```(?i:json)?((?s:.*?))```")
)

// textCall is one call found in the text, before its checks.
type textCall struct {
	name string
	args json.RawMessage
	err  error
}

// textCalls returns the tool calls the model wrote as text in content, or nil. The formats are tried in a fixed order
// and the first one that holds a call wins. Every call must name one of tools and carry arguments that are valid for
// that tool's schema; when one does not, the whole answer is text. The ids are prefix-1, prefix-2 and so on.
func textCalls(content string, tools map[string]ports.ModelTool, prefix string) []ports.ModelToolCall {
	if len(content) > maxTextCallBytes || strings.TrimSpace(content) == "" {
		return nil
	}
	var found []textCall
	for _, parse := range []func(string, map[string]ports.ModelTool) []textCall{dsmlCalls, v3Calls, tagCalls, fencedCalls} {
		if found = parse(content, tools); len(found) > 0 {
			break
		}
	}
	if len(found) == 0 || len(found) > maxTextCalls {
		return nil
	}
	out := make([]ports.ModelToolCall, 0, len(found))
	for i, c := range found {
		spec, ok := tools[c.name]
		if !ok || c.err != nil {
			return nil
		}
		args, err := checkArguments(spec, c.args)
		if err != nil {
			return nil
		}
		out = append(out, ports.ModelToolCall{ID: fmt.Sprintf("%s-%d", prefix, i+1), Name: c.name, Arguments: string(args)})
	}
	return out
}

func dsmlCalls(content string, tools map[string]ports.ModelTool) []textCall {
	var out []textCall
	for _, m := range dsmlInvoke.FindAllStringSubmatch(content, maxTextCalls+1) {
		name := m[1]
		args := map[string]json.RawMessage{}
		var err error
		for _, p := range dsmlParam.FindAllStringSubmatch(m[2], -1) {
			var v json.RawMessage
			if v, err = dsmlValue(tools[name], p[1], p[2], p[3]); err != nil {
				break
			}
			args[p[1]] = v
		}
		raw, mErr := json.Marshal(args)
		out = append(out, textCall{name: name, args: raw, err: errors.Join(err, mErr)})
	}
	return out
}

// dsmlValue reads one DSML parameter. string="true" is raw text and string="false" is JSON. Without the flag, the
// tool's schema decides: a string property is raw text, anything else is JSON.
func dsmlValue(spec ports.ModelTool, name, flag, value string) (json.RawMessage, error) {
	asString := flag == "true"
	if flag == "" {
		asString = propertyType(spec, name) == "string"
	}
	if asString {
		return json.Marshal(value)
	}
	v := json.RawMessage(strings.TrimSpace(value))
	if !json.Valid(v) {
		return nil, fmt.Errorf("parameter %q is not JSON", name)
	}
	return v, nil
}

func v3Calls(content string, _ map[string]ports.ModelTool) []textCall {
	var out []textCall
	for _, m := range v3Call.FindAllStringSubmatch(content, maxTextCalls+1) {
		out = append(out, textCall{name: m[1], args: json.RawMessage(strings.TrimSpace(m[2]))})
	}
	return out
}

func tagCalls(content string, _ map[string]ports.ModelTool) []textCall {
	var out []textCall
	for _, m := range tagCall.FindAllStringSubmatch(content, maxTextCalls+1) {
		c, ok := jsonCall([]byte(m[1]))
		if !ok {
			c = textCall{err: errors.New("a tool_call tag that is not a call")}
		}
		out = append(out, c)
	}
	return out
}

// fencedCalls reads the fenced blocks that hold a call object or an array of call objects. A block that holds other
// JSON, or no JSON, is prose and is skipped.
func fencedCalls(content string, _ map[string]ports.ModelTool) []textCall {
	var out []textCall
	for _, m := range fencedJSON.FindAllStringSubmatch(content, maxTextCalls+1) {
		body := bytes.TrimSpace([]byte(m[1]))
		if c, ok := jsonCall(body); ok {
			out = append(out, c)
			continue
		}
		var list []json.RawMessage
		if json.Unmarshal(body, &list) != nil || len(list) == 0 || len(list) > maxTextCalls {
			continue
		}
		var calls []textCall
		for _, item := range list {
			c, ok := jsonCall(item)
			if !ok {
				calls = nil
				break
			}
			calls = append(calls, c)
		}
		out = append(out, calls...)
	}
	return out
}

// jsonCall reads {"name": "x", "arguments": ...}. The arguments may be an object or a JSON string that holds one, as in
// the OpenAI wire form, and "parameters" is read in place of "arguments". ok is false for JSON that is not a call.
func jsonCall(raw []byte) (textCall, bool) {
	var obj struct {
		Name       *string         `json:"name"`
		Arguments  json.RawMessage `json:"arguments"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if json.Unmarshal(raw, &obj) != nil || obj.Name == nil {
		return textCall{}, false
	}
	args := obj.Arguments
	if args == nil {
		args = obj.Parameters
	}
	if args == nil {
		return textCall{}, false
	}
	var inner string
	if json.Unmarshal(args, &inner) == nil {
		args = json.RawMessage(inner)
	}
	return textCall{name: *obj.Name, args: args}, true
}

// toolSchema is the part of a JSON schema the parser checks: the property types, the required properties and
// whether other properties are allowed.
type toolSchema struct {
	Properties map[string]struct {
		Type any `json:"type"`
	} `json:"properties"`
	Required             []string        `json:"required"`
	AdditionalProperties json.RawMessage `json:"additionalProperties"`
}

func propertyType(spec ports.ModelTool, name string) string {
	var s toolSchema
	if len(spec.Parameters) == 0 || json.Unmarshal(spec.Parameters, &s) != nil {
		return ""
	}
	t, _ := s.Properties[name].Type.(string)
	return t
}

// checkArguments accepts a JSON object that holds the schema's required properties, with the declared type for each
// property that has one, and no other property when the schema forbids them. A tool with no schema takes any object.
// It returns the arguments compacted.
func checkArguments(spec ports.ModelTool, raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("tool arguments are not a JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("compact tool arguments: %w", err)
	}
	if len(spec.Parameters) == 0 {
		return compact.Bytes(), nil
	}
	var s toolSchema
	if err := json.Unmarshal(spec.Parameters, &s); err != nil {
		return nil, fmt.Errorf("tool schema: %w", err)
	}
	for _, name := range s.Required {
		if _, ok := obj[name]; !ok {
			return nil, fmt.Errorf("tool arguments lack %q", name)
		}
	}
	if err := s.checkProperties(obj); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

// checkProperties checks each property against its declared type, and refuses an undeclared one when the schema
// forbids other properties.
func (s toolSchema) checkProperties(obj map[string]json.RawMessage) error {
	closed := strings.TrimSpace(string(s.AdditionalProperties)) == "false"
	for name, value := range obj {
		prop, declared := s.Properties[name]
		if !declared {
			if closed {
				return fmt.Errorf("tool arguments hold unknown %q", name)
			}
			continue
		}
		if t, ok := prop.Type.(string); ok && !jsonHasType(value, t) {
			return fmt.Errorf("tool argument %q is not a %s", name, t)
		}
	}
	return nil
}

// jsonHasType reports whether a JSON value has the JSON schema type t. An unknown type name is not checked.
func jsonHasType(value json.RawMessage, t string) bool {
	var v any
	if json.Unmarshal(value, &v) != nil {
		return false
	}
	switch t {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "number":
		_, ok := v.(float64)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "null":
		return v == nil
	}
	return true
}
