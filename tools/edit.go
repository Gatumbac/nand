package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Gatumbac/nand"
)

var editSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to edit."
		},
		"old_text": {
			"type": "string",
			"description": "Exact text block to replace (must match uniquely)."
		},
		"new_text": {
			"type": "string",
			"description": "Replacement text (may be empty to delete old_text)."
		}
	},
	"required": ["path", "old_text", "new_text"]
}`)

type editArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type EditTool struct{}

func NewEdit() *EditTool {
	return &EditTool{}
}

func (e *EditTool) Name() string                     { return "edit" }
func (e *EditTool) Description() string              { return "Replace a unique exact text block in a file." }
func (e *EditTool) Schema() json.RawMessage          { return editSchema }
func (e *EditTool) Permission() nand.PermissionLevel { return nand.PermissionWrite }

func (e *EditTool) Run(_ context.Context, rawArgs json.RawMessage) (string, error) {
	args, err := parseEditArgs(rawArgs)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	updated, err := replaceUnique(string(data), args.OldText, args.NewText, args.Path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(args.Path, []byte(updated), info.Mode()); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fmt.Sprintf("edited %s", args.Path), nil
}

func parseEditArgs(raw json.RawMessage) (editArgs, error) {
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return editArgs{}, fmt.Errorf("invalid edit arguments: %w", err)
	}
	args.Path = strings.TrimSpace(args.Path)
	if args.Path == "" {
		return editArgs{}, errors.New("path is required")
	}
	if args.OldText == "" {
		return editArgs{}, errors.New("old_text is required")
	}
	return args, nil
}

func replaceUnique(content, oldText, newText, path string) (string, error) {
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("old_text not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_text matched %d times in %s; provide more context", count, path)
	}
	return strings.Replace(content, oldText, newText, 1), nil
}
