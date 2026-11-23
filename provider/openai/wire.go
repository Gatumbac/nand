package openai

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Gatumbac/nand"
)

type chatRequest struct {
	Model    string            `json:"model"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []wireTool        `json:"tools,omitempty"`
}

type wireTool struct {
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatResponse struct {
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

type wireChoice struct {
	Message      json.RawMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireFunctionCall `json:"function"`
}

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func encodeRequest(defaultModel string, req nand.CompletionRequest) ([]byte, error) {
	model := req.Model
	if model == "" {
		model = defaultModel
	}

	messages := make([]json.RawMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		raw, err := marshalWireMessage(nand.Message{Role: nand.RoleSystem, Content: req.System})
		if err != nil {
			return nil, fmt.Errorf("encode system message: %w", err)
		}
		messages = append(messages, raw)
	}

	for i, msg := range req.Messages {
		raw, err := marshalWireMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("encode message %d: %w", i, err)
		}
		messages = append(messages, raw)
	}

	return json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Tools:    encodeToolDefinitions(req.Tools),
	})
}

func encodeToolDefinitions(defs []nand.ToolDefinition) []wireTool {
	if len(defs) == 0 {
		return nil
	}

	out := make([]wireTool, len(defs))
	for i, def := range defs {
		out[i] = wireTool{
			Type: "function",
			Function: wireToolFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.Schema,
			},
		}
	}
	return out
}

func marshalWireMessage(msg nand.Message) (json.RawMessage, error) {
	fields := make(map[string]any, len(msg.ProviderData)+4)
	for k, v := range msg.ProviderData {
		fields[k] = v
	}

	fields["role"] = msg.Role
	if msg.Content != "" || len(msg.ToolCalls) == 0 {
		fields["content"] = msg.Content
	}
	if msg.ToolCallID != "" {
		fields["tool_call_id"] = msg.ToolCallID
	}
	if len(msg.ToolCalls) > 0 {
		fields["tool_calls"] = encodeToolCalls(msg.ToolCalls)
	}

	return json.Marshal(fields)
}

func encodeToolCalls(calls []nand.ToolCall) []any {
	out := make([]any, len(calls))
	for i, tc := range calls {
		if len(tc.Raw) > 0 {
			out[i] = tc.Raw
			continue
		}
		out[i] = wireToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: wireFunctionCall{
				Name:      tc.Name,
				Arguments: string(tc.Arguments),
			},
		}
	}
	return out
}

func decodeResponse(body []byte) (nand.CompletionResponse, error) {
	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nand.CompletionResponse{}, fmt.Errorf("decode completion response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nand.CompletionResponse{}, errors.New("completion response contained no choices")
	}

	choice := resp.Choices[0]
	msg, err := unmarshalWireMessage(choice.Message)
	if err != nil {
		return nand.CompletionResponse{}, err
	}

	return nand.CompletionResponse{
		Message:      msg,
		FinishReason: choice.FinishReason,
		Usage: nand.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}, nil
}

func unmarshalWireMessage(raw json.RawMessage) (nand.Message, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nand.Message{}, fmt.Errorf("decode message object: %w", err)
	}

	var msg nand.Message
	for key, value := range fields {
		switch key {
		case "role":
			if err := json.Unmarshal(value, &msg.Role); err != nil {
				return nand.Message{}, fmt.Errorf("decode role: %w", err)
			}
		case "content":
			if string(value) == "null" {
				continue
			}
			if err := json.Unmarshal(value, &msg.Content); err != nil {
				return nand.Message{}, fmt.Errorf("decode content: %w", err)
			}
		case "tool_call_id":
			if string(value) == "null" {
				continue
			}
			if err := json.Unmarshal(value, &msg.ToolCallID); err != nil {
				return nand.Message{}, fmt.Errorf("decode tool_call_id: %w", err)
			}
		case "tool_calls":
			calls, err := decodeToolCalls(value)
			if err != nil {
				return nand.Message{}, err
			}
			msg.ToolCalls = calls
		default:
			if msg.ProviderData == nil {
				msg.ProviderData = make(map[string]json.RawMessage)
			}
			msg.ProviderData[key] = value
		}
	}

	return msg, nil
}

func decodeToolCalls(raw json.RawMessage) ([]nand.ToolCall, error) {
	if string(raw) == "null" {
		return nil, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode tool_calls: %w", err)
	}

	calls := make([]nand.ToolCall, len(items))
	for i, item := range items {
		var wtc wireToolCall
		if err := json.Unmarshal(item, &wtc); err != nil {
			return nil, fmt.Errorf("decode tool_call %d: %w", i, err)
		}
		calls[i] = nand.ToolCall{
			ID:        wtc.ID,
			Name:      wtc.Function.Name,
			Arguments: json.RawMessage(wtc.Function.Arguments),
			Raw:       item,
		}
	}
	return calls, nil
}
