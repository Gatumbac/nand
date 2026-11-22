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

var readSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to read."
		},
		"start_line": {
			"type": "integer",
			"description": "1-based first line to include."
		},
		"end_line": {
			"type": "integer",
			"description": "1-based last line to include (inclusive)."
		}
	},
	"required": ["path"]
}`)

type readArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type ReadTool struct{}

func NewRead() *ReadTool {
	return &ReadTool{}
}

func (r *ReadTool) Name() string                     { return "read" }
func (r *ReadTool) Description() string              { return "Read a text file with 1-based line numbers." }
func (r *ReadTool) Schema() json.RawMessage          { return readSchema }
func (r *ReadTool) Permission() nand.PermissionLevel { return nand.PermissionRead }

func (r *ReadTool) Run(_ context.Context, rawArgs json.RawMessage) (string, error) {
	args, err := parseReadArgs(rawArgs)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	lines := splitLines(string(data))
	sliced, startLine, err := sliceLines(lines, args.StartLine, args.EndLine)
	if err != nil {
		return "", err
	}
	return formatNumberedLines(sliced, startLine), nil
}

func parseReadArgs(raw json.RawMessage) (readArgs, error) {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return readArgs{}, fmt.Errorf("invalid read arguments: %w", err)
	}
	args.Path = strings.TrimSpace(args.Path)
	if args.Path == "" {
		return readArgs{}, errors.New("path is required")
	}
	if args.StartLine < 0 || args.EndLine < 0 {
		return readArgs{}, errors.New("line numbers must be positive")
	}
	if args.StartLine > 0 && args.EndLine > 0 && args.StartLine > args.EndLine {
		return readArgs{}, errors.New("start_line cannot be greater than end_line")
	}
	return args, nil
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	content = strings.TrimSuffix(content, "\n")
	return strings.Split(content, "\n")
}

func sliceLines(lines []string, startLine, endLine int) ([]string, int, error) {
	if len(lines) == 0 {
		return nil, 1, nil
	}

	start := 1
	if startLine > 0 {
		start = startLine
	}
	if start > len(lines) {
		return nil, 0, fmt.Errorf("start_line %d exceeds file length %d", start, len(lines))
	}

	end := len(lines)
	if endLine > 0 && endLine < end {
		end = endLine
	}
	return lines[start-1 : end], start, nil
}

func formatNumberedLines(lines []string, startLine int) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d: %s", startLine+i, line)
	}
	return b.String()
}
