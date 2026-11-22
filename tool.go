package nand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type PermissionLevel string

const (
	PermissionRead  PermissionLevel = "read"
	PermissionWrite PermissionLevel = "write"
	PermissionExec  PermissionLevel = "exec"
)

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Permission() PermissionLevel
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

type Registry struct {
	byName map[string]Tool
	order  []string
}

func NewRegistry(tools ...Tool) (*Registry, error) {
	reg := &Registry{
		byName: make(map[string]Tool, len(tools)),
		order:  make([]string, 0, len(tools)),
	}

	for _, tool := range tools {
		if tool == nil {
			return nil, errors.New("tool registry: nil tool")
		}
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			return nil, errors.New("tool registry: tool name is required")
		}
		if _, exists := reg.byName[name]; exists {
			return nil, fmt.Errorf("tool registry: duplicate tool %q", name)
		}
		reg.byName[name] = tool
		reg.order = append(reg.order, name)
	}

	return reg, nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	tool, ok := r.byName[name]
	return tool, ok
}

func (r *Registry) Definitions() []ToolDefinition {
	if r == nil || len(r.order) == 0 {
		return nil
	}

	defs := make([]ToolDefinition, 0, len(r.order))
	for _, name := range r.order {
		tool := r.byName[name]
		defs = append(defs, ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      tool.Schema(),
		})
	}
	return defs
}
