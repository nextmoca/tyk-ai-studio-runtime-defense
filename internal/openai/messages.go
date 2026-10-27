package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const OmissionMarker = "[Next Moca omitted nonessential context for this request.]"

type Record struct {
	ID         string
	Kind       string
	Source     string
	SourceKind string
	Text       string
}

// Support classifies how much of a request body the adapter can govern.
type Support string

const (
	// Supported means every message is understood and candidate context can
	// be selected and rewritten.
	Supported Support = "supported"
	// NotChat means the body carries no usable messages array, so it is not a
	// chat request this adapter applies to.
	NotChat Support = "not_chat"
	// ContentParts means a message carries a content array (multimodal or
	// typed parts) that this version cannot select or rewrite.
	ContentParts Support = "content_parts"
	// InvalidMessage means a message is not an object with a string role.
	InvalidMessage Support = "invalid_message"
	// NoTask means there is no non-empty user message to use as the task.
	NoTask Support = "no_task_message"
	// DuplicateKeys means the request root or a message repeats a JSON key.
	// Parsers disagree on which value wins, so the adapter cannot know which
	// content the provider will read.
	DuplicateKeys Support = "duplicate_keys"
)

type Request struct {
	root             map[string]json.RawMessage
	messages         []message
	candidateIndexes []int
	taskIndex        int
	functionRole     bool
}

type message struct {
	fields  map[string]json.RawMessage
	role    string
	content string
}

// Parse inspects an OpenAI-compatible request body. The returned Support says
// whether the adapter can govern it; the Request is usable only when Support is
// Supported. An error means the body is not valid JSON.
func Parse(raw []byte) (Request, Support, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return Request{}, NotChat, fmt.Errorf("decode OpenAI-compatible request: %w", err)
	}
	rootKeys, rootDuplicate, err := scanObjectKeys(raw)
	if err != nil {
		return Request{}, NotChat, fmt.Errorf("decode OpenAI-compatible request: %w", err)
	}
	if _, hasMessages := rootKeys["messages"]; !hasMessages {
		return Request{}, NotChat, nil
	}
	if rootDuplicate {
		// Checked before the retained value of "messages" is inspected: a
		// parser that keeps the first of two "messages" keys sees a different
		// request from the one Go decoded.
		return Request{}, DuplicateKeys, nil
	}
	var rawMessages []json.RawMessage
	if err := json.Unmarshal(root["messages"], &rawMessages); err != nil {
		return Request{}, NotChat, nil
	}
	if len(rawMessages) == 0 {
		return Request{}, NotChat, nil
	}

	request := Request{root: root, taskIndex: -1}
	for _, rawMessage := range rawMessages {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawMessage, &fields); err != nil {
			return Request{}, InvalidMessage, nil
		}
		if _, duplicate, err := scanObjectKeys(rawMessage); err != nil || duplicate {
			return Request{}, DuplicateKeys, nil
		}
		role, ok := stringField(fields, "role")
		if !ok || role == "" {
			return Request{}, InvalidMessage, nil
		}
		content, hasContent, isString := stringOrMissingField(fields, "content")
		if hasContent && !isString {
			// Multimodal content arrays need a provider-aware adapter rather than
			// a lossy string coercion.
			return Request{}, ContentParts, nil
		}
		if role == "function" {
			// Legacy function-role results are never selected or rewritten.
			request.functionRole = true
		}
		request.messages = append(request.messages, message{
			fields:  fields,
			role:    role,
			content: content,
		})
	}

	for index := len(request.messages) - 1; index >= 0; index-- {
		if request.messages[index].role == "user" && strings.TrimSpace(request.messages[index].content) != "" {
			request.taskIndex = index
			break
		}
	}
	if request.taskIndex < 0 {
		return Request{}, NoTask, nil
	}

	for index, message := range request.messages {
		if !isCandidate(message.role, index, request.taskIndex) || strings.TrimSpace(message.content) == "" {
			continue
		}
		request.candidateIndexes = append(request.candidateIndexes, index)
	}
	return request, Supported, nil
}

// HasFunctionRole reports whether the request carries a legacy function-role
// message. Those messages are left unchanged and never sent for selection.
func (r Request) HasFunctionRole() bool {
	return r.functionRole
}

// scanObjectKeys returns the keys of the top-level JSON object in raw and
// whether any key repeats. Keys are compared after unescaping, as a parser
// would see them. A value that is not an object has no keys.
func scanObjectKeys(raw []byte) (map[string]struct{}, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, false, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, false, nil
	}
	seen := make(map[string]struct{})
	duplicate := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, false, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, false, fmt.Errorf("object key is not a string")
		}
		if _, repeated := seen[key]; repeated {
			duplicate = true
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false, err
		}
	}
	return seen, duplicate, nil
}

func (r Request) Records() []Record {
	records := make([]Record, 0, len(r.candidateIndexes))
	for _, index := range r.candidateIndexes {
		message := r.messages[index]
		records = append(records, Record{
			ID:         recordID(index),
			Kind:       recordKind(message.role),
			Source:     "tyk-ai-studio",
			SourceKind: sourceKind(message.role),
			Text:       message.content,
		})
	}
	return records
}

func (r Request) TaskPrompt() string {
	if r.taskIndex < 0 || r.taskIndex >= len(r.messages) {
		return ""
	}
	return r.messages[r.taskIndex].content
}

func (r Request) CandidateCount() int {
	return len(r.candidateIndexes)
}

func (r Request) HasCandidate(id string) bool {
	for _, index := range r.candidateIndexes {
		if recordID(index) == id {
			return true
		}
	}
	return false
}

func (r Request) RewriteSelected(excerpts map[string]string) ([]byte, error) {
	if len(excerpts) == 0 {
		return nil, fmt.Errorf("at least one selected record excerpt is required")
	}

	rewrittenMessages := make([]json.RawMessage, 0, len(r.messages))
	for index, message := range r.messages {
		fields := copyFields(message.fields)
		if containsIndex(r.candidateIndexes, index) {
			content, selected := excerpts[recordID(index)]
			if !selected {
				content = OmissionMarker
			}
			encoded, err := json.Marshal(content)
			if err != nil {
				return nil, fmt.Errorf("encode selected content: %w", err)
			}
			fields["content"] = encoded
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("encode message %d: %w", index, err)
		}
		rewrittenMessages = append(rewrittenMessages, encoded)
	}

	encodedMessages, err := json.Marshal(rewrittenMessages)
	if err != nil {
		return nil, fmt.Errorf("encode messages: %w", err)
	}
	root := copyFields(r.root)
	root["messages"] = encodedMessages
	encodedRoot, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten request: %w", err)
	}
	return encodedRoot, nil
}

func (r Request) RewriteSingleCandidate(rendered string) ([]byte, error) {
	if r.CandidateCount() != 1 {
		return nil, fmt.Errorf("single-candidate rewrite requires exactly one candidate record")
	}
	return r.RewriteSelected(map[string]string{recordID(r.candidateIndexes[0]): rendered})
}

func isCandidate(role string, index, taskIndex int) bool {
	switch role {
	case "tool", "assistant":
		return true
	case "user":
		return index != taskIndex
	default:
		return false
	}
}

func recordKind(role string) string {
	switch role {
	case "tool":
		return "tool_result"
	case "assistant":
		return "llm_response"
	default:
		return "user_input"
	}
}

func sourceKind(role string) string {
	switch role {
	case "tool":
		return "tool"
	case "assistant":
		return "agent"
	default:
		return "user"
	}
}

func recordID(index int) string {
	return fmt.Sprintf("tyk:%d", index)
}

func stringField(fields map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func stringOrMissingField(fields map[string]json.RawMessage, key string) (string, bool, bool) {
	raw, ok := fields[key]
	if !ok || string(raw) == "null" {
		return "", ok, true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, false
	}
	return value, true, true
}

func copyFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	copy := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		copy[key] = append(json.RawMessage(nil), value...)
	}
	return copy
}

func containsIndex(indexes []int, target int) bool {
	for _, index := range indexes {
		if index == target {
			return true
		}
	}
	return false
}
