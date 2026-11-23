package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gatumbac/nand"
)

func TestBashToolCapturesOutputAndExitCode(t *testing.T) {
	tool := NewBash(t.TempDir())
	if tool.Permission() != nand.PermissionExec {
		t.Fatalf("got permission %q, want %q", tool.Permission(), nand.PermissionExec)
	}

	okArgs, _ := json.Marshal(map[string]any{"command": "echo hello && echo err >&2"})
	out, err := tool.Run(context.Background(), okArgs)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "err") {
		t.Errorf("unexpected output: %q", out)
	}

	failArgs, _ := json.Marshal(map[string]any{"command": "echo oops && exit 7"})
	out, err = tool.Run(context.Background(), failArgs)
	if err != nil {
		t.Fatalf("expected exit code output, got err: %v", err)
	}
	if !strings.HasPrefix(out, "exit code 7\n") || !strings.Contains(out, "oops") {
		t.Errorf("unexpected non-zero output: %q", out)
	}
}

func TestBashToolTimeoutAndValidation(t *testing.T) {
	tool := NewBash("")
	if _, err := tool.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for empty command")
	}

	timeoutArgs, _ := json.Marshal(map[string]any{"command": "sleep 3", "timeout_seconds": 1})
	_, err := tool.Run(context.Background(), timeoutArgs)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("got err %v, want timeout error", err)
	}
}
