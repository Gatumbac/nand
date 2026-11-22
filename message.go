package nand

import "encoding/json"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
	Raw       json.RawMessage
}

type Message struct {
	Role         Role
	Content      string
	ToolCallID   string
	ToolCalls    []ToolCall
	ProviderData map[string]json.RawMessage
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}
