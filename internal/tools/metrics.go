package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TopNodesInput struct {
	Context string `json:"context,omitempty" jsonschema:"override the session context"`
}

type TopPodsInput struct {
	Namespace     string `json:"namespace,omitempty" jsonschema:"namespace"`
	AllNamespaces bool   `json:"all_namespaces,omitempty" jsonschema:"across all namespaces"`
	Containers    bool   `json:"containers,omitempty" jsonschema:"break down usage per container"`
	Context       string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerMetrics(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "top_nodes",
		Description: "Show CPU/memory usage per node (requires metrics-server).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TopNodesInput) (*mcp.CallToolResult, Result, error) {
		out, err := d.Runner.Run(ctx, d.kctx(in.Context), "top", "nodes")
		if err != nil {
			return nil, Result{}, metricsErr(err)
		}
		return finalize([]string{"top", "nodes"}, out)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "top_pods",
		Description: "Show CPU/memory usage per pod (requires metrics-server).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TopPodsInput) (*mcp.CallToolResult, Result, error) {
		args := []string{"top", "pods"}
		args, err := appendScope(args, in.Namespace, in.AllNamespaces)
		if err != nil {
			return nil, Result{}, err
		}
		if in.Containers {
			args = append(args, "--containers")
		}
		out, err := d.Runner.Run(ctx, d.kctx(in.Context), args...)
		if err != nil {
			return nil, Result{}, metricsErr(err)
		}
		return finalize(args, out)
	})
}

// metricsErr adds a hint when the failure is a missing/unreachable metrics-server.
func metricsErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "metrics") || strings.Contains(msg, "Metrics API") {
		return errors.New(msg + "\n\nhint: metrics require metrics-server to be installed and reachable in the cluster")
	}
	return err
}
