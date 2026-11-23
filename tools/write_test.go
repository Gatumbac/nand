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

func TestWriteToolCreatesParentsAndOverwrites(t *testing.T) {
	tool := NewWrite()
	if tool.Permission() != nand.PermissionWrite {
		t.Fatalf("got permission %q, want %q", tool.Permission(), nand.PermissionWrite)
	}

	path := filepath.Join(t.TempDir(), "nested", "dir", "file.txt")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "first"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(out, "wrote 5 bytes") {
		t.Errorf("unexpected output: %q", out)
	}

	overwriteArgs, _ := json.Marshal(map[string]string{"path": path, "content": "second"})
	if _, err := tool.Run(context.Background(), overwriteArgs); err != nil {
		t.Fatalf("overwrite Run error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("got %q, want %q", string(data), "second")
	}
}

func TestWriteToolValidation(t *testing.T) {
	tool := NewWrite()
	for _, raw := range []string{`{`, `{"content":"x"}`} {
		if _, err := tool.Run(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("expected error for %s", raw)
		}
	}
}
