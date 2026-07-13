package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

type GetEventsInput struct {
	Namespace     string `json:"namespace,omitempty" jsonschema:"namespace"`
	AllNamespaces bool   `json:"all_namespaces,omitempty" jsonschema:"events across all namespaces"`
	Object        string `json:"object,omitempty" jsonschema:"only events for this object name (e.g. a pod name)"`
	Context       string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerEvents(s *mcp.Server, d *Deps) {
	addTool(s, &mcp.Tool{
		Name:        "get_events",
		Description: "List cluster events, most recent last. Optionally scope to a namespace or a single object.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetEventsInput) (*mcp.CallToolResult, Result, error) {
		args := []string{"get", "events", "--sort-by=.lastTimestamp"}
		args, err := appendScope(args, in.Namespace, in.AllNamespaces)
		if err != nil {
			return nil, Result{}, err
		}
		if in.Object != "" {
			if err := kube.SafeArg("object", in.Object); err != nil {
				return nil, Result{}, err
			}
			args = append(args, "--field-selector", "involvedObject.name="+in.Object)
		}
		return d.run(ctx, in.Context, args...)
	})
}
