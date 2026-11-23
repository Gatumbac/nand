package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Gatumbac/nand"
)

const (
	defaultBashTimeout = 30 * time.Second
	maxBashTimeout     = 120 * time.Second
)

var bashSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "Shell command to execute with bash -c."
		},
		"timeout_seconds": {
			"type": "integer",
			"description": "Optional timeout in seconds (default 30, max 120)."
		}
	},
	"required": ["command"]
}`)

type bashArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type BashTool struct {
	workDir string
}

func NewBash(workDir string) *BashTool {
	return &BashTool{workDir: workDir}
}

func (b *BashTool) Name() string                     { return "bash" }
func (b *BashTool) Description() string              { return "Execute a shell command and return its combined output." }
func (b *BashTool) Schema() json.RawMessage          { return bashSchema }
func (b *BashTool) Permission() nand.PermissionLevel { return nand.PermissionExec }

func (b *BashTool) Run(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	args, err := parseBashArgs(rawArgs)
	if err != nil {
		return "", err
	}

	timeout := resolveTimeout(args.TimeoutSeconds)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", args.Command)
	if b.workDir != "" {
		cmd.Dir = b.workDir
	}

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err = cmd.Run()
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("command timed out after %s", timeout)
	}
	return formatCommandOutput(combined.String(), err)
}

func parseBashArgs(raw json.RawMessage) (bashArgs, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return bashArgs{}, fmt.Errorf("invalid bash arguments: %w", err)
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return bashArgs{}, errors.New("command is required")
	}
	if args.TimeoutSeconds < 0 {
		return bashArgs{}, errors.New("timeout_seconds must be positive")
	}
	return args, nil
}

func resolveTimeout(seconds int) time.Duration {
	if seconds == 0 {
		return defaultBashTimeout
	}
	d := time.Duration(seconds) * time.Second
	if d > maxBashTimeout {
		return maxBashTimeout
	}
	return d
}

func formatCommandOutput(output string, runErr error) (string, error) {
	if runErr == nil {
		return output, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		trimmed := strings.TrimSpace(output)
		if trimmed == "" {
			return fmt.Sprintf("exit code %d", exitErr.ExitCode()), nil
		}
		return fmt.Sprintf("exit code %d\n%s", exitErr.ExitCode(), output), nil
	}
	return "", fmt.Errorf("run command: %w", runErr)
}
