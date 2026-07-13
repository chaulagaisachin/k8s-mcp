package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

// rolloutTypes are the workload kinds that support rollouts.
var rolloutTypes = map[string]bool{
	"deployment":  true,
	"deploy":      true,
	"statefulset": true,
	"sts":         true,
	"daemonset":   true,
	"ds":          true,
}

type RolloutStatusInput struct {
	Type      string `json:"type,omitempty" jsonschema:"workload kind: deployment (default), statefulset, or daemonset"`
	Name      string `json:"name" jsonschema:"workload name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type RolloutHistoryInput struct {
	Type      string `json:"type,omitempty" jsonschema:"workload kind: deployment (default), statefulset, or daemonset"`
	Name      string `json:"name" jsonschema:"workload name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerRollout(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "rollout_status",
		Description: "Show the current rollout status of a deployment/statefulset/daemonset (does not wait).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RolloutStatusInput) (*mcp.CallToolResult, Result, error) {
		target, err := rolloutTarget(in.Type, in.Name)
		if err != nil {
			return nil, Result{}, err
		}
		// --watch=false returns the current state instead of blocking until complete.
		args := []string{"rollout", "status", target, "--watch=false"}
		args, err = appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		return d.run(ctx, in.Context, args...)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "rollout_history",
		Description: "Show the revision history of a deployment/statefulset/daemonset.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RolloutHistoryInput) (*mcp.CallToolResult, Result, error) {
		target, err := rolloutTarget(in.Type, in.Name)
		if err != nil {
			return nil, Result{}, err
		}
		args := []string{"rollout", "history", target}
		args, err = appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		return d.run(ctx, in.Context, args...)
	})
}

// rolloutTarget validates the workload type and name and builds "type/name".
func rolloutTarget(typ, name string) (string, error) {
	if typ == "" {
		typ = "deployment"
	}
	if err := kube.SafeArg("type", typ); err != nil {
		return "", err
	}
	if !rolloutTypes[strings.ToLower(typ)] {
		return "", fmt.Errorf("type %q does not support rollouts; use deployment, statefulset, or daemonset", typ)
	}
	if err := kube.SafeArg("name", name); err != nil {
		return "", err
	}
	return typ + "/" + name, nil
}
