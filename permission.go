package nand

import (
	"context"
	"encoding/json"
)

type ApprovalRequest struct {
	ToolName   string
	Permission PermissionLevel
	Arguments  json.RawMessage
}

type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (bool, error)
}

type ApproverFunc func(ctx context.Context, req ApprovalRequest) (bool, error)

func (f ApproverFunc) Approve(ctx context.Context, req ApprovalRequest) (bool, error) {
	return f(ctx, req)
}
