package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

const defaultTailLines = 1000

type GetLogsInput struct {
	Pod       string `json:"pod" jsonschema:"pod name"`
	Container string `json:"container,omitempty" jsonschema:"container name (for multi-container pods)"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	Tail      int    `json:"tail,omitempty" jsonschema:"number of trailing lines (default 1000; -1 for all)"`
	Previous  bool   `json:"previous,omitempty" jsonschema:"logs from the previous (crashed) container instance"`
	Since     string `json:"since,omitempty" jsonschema:"only logs newer than a duration, e.g. 1h or 10m"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerLogs(s *mcp.Server, d *Deps) {
	addTool(s, &mcp.Tool{
		Name:        "get_logs",
		Description: "Get container logs for a pod. Output is capped; use tail/since to narrow it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetLogsInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("pod", in.Pod); err != nil {
			return nil, Result{}, err
		}
		args := []string{"logs", in.Pod}
		var err error
		args, err = appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		if in.Container != "" {
			if err := kube.SafeArg("container", in.Container); err != nil {
				return nil, Result{}, err
			}
			args = append(args, "-c", in.Container)
		}
		tail := in.Tail
		if tail == 0 {
			tail = defaultTailLines
		}
		args = append(args, fmt.Sprintf("--tail=%d", tail))
		if in.Previous {
			args = append(args, "--previous")
		}
		if in.Since != "" {
			if err := kube.SafeArg("since", in.Since); err != nil {
				return nil, Result{}, err
			}
			args = append(args, "--since="+in.Since)
		}
		out, err := d.Runner.Run(ctx, d.kctx(in.Context), args...)
		if err != nil {
			return nil, Result{}, err
		}
		return finalize(args, kube.RedactLog(out))
	})
}
