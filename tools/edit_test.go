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

func TestEditToolReplacesUniqueAndDeletes(t *testing.T) {
	tool := NewEdit()
	if tool.Permission() != nand.PermissionWrite {
		t.Fatalf("got permission %q, want %q", tool.Permission(), nand.PermissionWrite)
	}

	path := filepath.Join(t.TempDir(), "code.go")
	if err := os.WriteFile(path, []byte("package main\n\nvar x = 1\n"), 0o600); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	args, _ := json.Marshal(map[string]string{
		"path":     path,
		"old_text": "var x = 1",
		"new_text": "var x = 2",
	})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run replace error: %v", err)
	}

	delArgs, _ := json.Marshal(map[string]string{
		"path":     path,
		"old_text": "\nvar x = 2\n",
		"new_text": "",
	})
	if _, err := tool.Run(context.Background(), delArgs); err != nil {
		t.Fatalf("Run delete error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "package main\n" {
		t.Errorf("got %q, want %q", string(data), "package main\n")
	}
}

func TestEditToolErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dup.txt")
	if err := os.WriteFile(path, []byte("foo\nfoo\n"), 0o600); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	tool := NewEdit()
	tests := []struct {
		name string
		args map[string]string
		want string
	}{
		{name: "missing path", args: map[string]string{"old_text": "a"}, want: "path is required"},
		{name: "empty old_text", args: map[string]string{"path": path, "old_text": ""}, want: "old_text is required"},
		{name: "not found", args: map[string]string{"path": path, "old_text": "bar"}, want: "not found"},
		{name: "ambiguous", args: map[string]string{"path": path, "old_text": "foo"}, want: "matched 2 times"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.args)
			_, err := tool.Run(context.Background(), raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got err %v, want substring %q", err, tc.want)
			}
		})
	}
}
