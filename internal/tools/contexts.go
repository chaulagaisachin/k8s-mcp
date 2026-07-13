package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

type emptyInput struct{}

type ContextsOutput struct {
	Contexts        []string `json:"contexts" jsonschema:"available context names from the kubeconfig"`
	Current         string   `json:"current" jsonschema:"the effective current context"`
	SessionOverride string   `json:"session_override,omitempty" jsonschema:"context set for this session, if any"`
}

type CurrentContextOutput struct {
	Context string `json:"context" jsonschema:"the effective current context"`
	Source  string `json:"source" jsonschema:"'session' if set via set_context, else 'kubeconfig'"`
}

type SetContextInput struct {
	Name string `json:"name" jsonschema:"the context name to use for this session"`
}

type SetContextOutput struct {
	Context string `json:"context" jsonschema:"the context now active for this session"`
	Message string `json:"message"`
}

func registerContexts(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_contexts",
		Description: "List the kubeconfig contexts and the effective current context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, ContextsOutput, error) {
		names, err := d.contextNames(ctx)
		if err != nil {
			return nil, ContextsOutput{}, err
		}
		current, _, err := d.currentContext(ctx)
		if err != nil {
			return nil, ContextsOutput{}, err
		}
		return nil, ContextsOutput{
			Contexts:        names,
			Current:         current,
			SessionOverride: d.Ctx.Override(),
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_current_context",
		Description: "Show the context currently in effect and where it came from.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, CurrentContextOutput, error) {
		current, source, err := d.currentContext(ctx)
		if err != nil {
			return nil, CurrentContextOutput{}, err
		}
		return nil, CurrentContextOutput{Context: current, Source: source}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_context",
		Description: "Set the default context for this session (in memory only; does not modify the kubeconfig).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SetContextInput) (*mcp.CallToolResult, SetContextOutput, error) {
		if err := kube.SafeArg("context", in.Name); err != nil {
			return nil, SetContextOutput{}, err
		}
		names, err := d.contextNames(ctx)
		if err != nil {
			return nil, SetContextOutput{}, err
		}
		if !contains(names, in.Name) {
			return nil, SetContextOutput{}, &unknownContextError{name: in.Name, known: names}
		}
		d.Ctx.Set(in.Name)
		return nil, SetContextOutput{
			Context: in.Name,
			Message: "session context set to " + in.Name,
		}, nil
	})
}

// contextNames returns the context names from the kubeconfig.
func (d *Deps) contextNames(ctx context.Context) ([]string, error) {
	out, err := d.Runner.Run(ctx, "", "config", "get-contexts", "-o", "name")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// currentContext returns the effective context and its source.
func (d *Deps) currentContext(ctx context.Context) (name, source string, err error) {
	if o := d.Ctx.Override(); o != "" {
		return o, "session", nil
	}
	out, err := d.Runner.Run(ctx, "", "config", "current-context")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(out), "kubeconfig", nil
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

type unknownContextError struct {
	name  string
	known []string
}

func (e *unknownContextError) Error() string {
	return "unknown context " + e.name + "; available: " + strings.Join(e.known, ", ")
}
