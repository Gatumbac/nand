package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gatumbac/nand"
)

var writeSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to create or overwrite."
		},
		"content": {
			"type": "string",
			"description": "Full text content to write to the file."
		}
	},
	"required": ["path", "content"]
}`)

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WriteTool struct{}

func NewWrite() *WriteTool {
	return &WriteTool{}
}

func (w *WriteTool) Name() string                     { return "write" }
func (w *WriteTool) Description() string              { return "Create or overwrite a text file, creating parent directories if needed." }
func (w *WriteTool) Schema() json.RawMessage          { return writeSchema }
func (w *WriteTool) Permission() nand.PermissionLevel { return nand.PermissionWrite }

func (w *WriteTool) Run(_ context.Context, rawArgs json.RawMessage) (string, error) {
	args, err := parseWriteArgs(rawArgs)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(args.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create parent directories: %w", err)
	}
	if err := os.WriteFile(args.Path, []byte(args.Content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
}

func parseWriteArgs(raw json.RawMessage) (writeArgs, error) {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return writeArgs{}, fmt.Errorf("invalid write arguments: %w", err)
	}
	args.Path = strings.TrimSpace(args.Path)
	if args.Path == "" {
		return writeArgs{}, errors.New("path is required")
	}
	return args, nil
}
