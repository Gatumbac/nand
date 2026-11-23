package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Gatumbac/nand"
)

type terminalApprover struct {
	reader *bufio.Reader
	out    io.Writer
}

func newTerminalApprover(in io.Reader, out io.Writer) *terminalApprover {
	return &terminalApprover{
		reader: bufio.NewReader(in),
		out:    out,
	}
}

func (t *terminalApprover) Approve(ctx context.Context, req nand.ApprovalRequest) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if _, err := fmt.Fprintf(t.out, "¿Permitir %s (%s) con %s? [s/N]: ", req.ToolName, req.Permission, strings.TrimSpace(string(req.Arguments))); err != nil {
		return false, err
	}

	line, err := t.reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}

	return isAffirmative(line), nil
}

func isAffirmative(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "s", "si", "sí", "y", "yes":
		return true
	default:
		return false
	}
}
