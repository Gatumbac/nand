package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gatumbac/nand"
)

func TestReadToolMetadata(t *testing.T) {
	tool := NewRead()
	if tool.Name() != "read" {
		t.Errorf("got name %q, want %q", tool.Name(), "read")
	}
	if tool.Permission() != nand.PermissionRead {
		t.Errorf("got permission %q, want %q", tool.Permission(), nand.PermissionRead)
	}
}

func TestReadToolFullAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\ndelta\n"), 0o600); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	tool := NewRead()
	fullArgs, _ := json.Marshal(map[string]any{"path": path})
	got, err := tool.Run(context.Background(), fullArgs)
	if err != nil {
		t.Fatalf("Run full error: %v", err)
	}
	wantFull := "1: alpha\n2: beta\n3: gamma\n4: delta"
	if got != wantFull {
		t.Errorf("got:\n%s\nwant:\n%s", got, wantFull)
	}

	rangeArgs, _ := json.Marshal(map[string]any{"path": path, "start_line": 2, "end_line": 3})
	got, err = tool.Run(context.Background(), rangeArgs)
	if err != nil {
		t.Fatalf("Run range error: %v", err)
	}
	wantRange := "2: beta\n3: gamma"
	if got != wantRange {
		t.Errorf("got:\n%s\nwant:\n%s", got, wantRange)
	}
}

func TestReadToolErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("only one line\n"), 0o600); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	tool := NewRead()
	tests := []struct {
		name string
		args string
		want string
	}{
		{name: "invalid json", args: `{`, want: "invalid read arguments"},
		{name: "missing path", args: `{}`, want: "path is required"},
		{name: "inverted range", args: `{"path":"a","start_line":5,"end_line":2}`, want: "start_line cannot be greater"},
		{name: "start past eof", args: `{"path":"` + filepath.ToSlash(path) + `","start_line":9}`, want: "exceeds file length"},
		{name: "missing file", args: `{"path":"` + filepath.ToSlash(filepath.Join(t.TempDir(), "nope.txt")) + `"}`, want: "read file"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Run(context.Background(), json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got err %v, want substring %q", err, tc.want)
			}
		})
	}
}
