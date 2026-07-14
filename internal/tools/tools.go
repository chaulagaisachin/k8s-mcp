// Package tools registers the read-only Kubernetes MCP tools.
package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

// Deps are the shared dependencies handed to every tool.
type Deps struct {
	Runner *kube.Runner
	Ctx    *kube.ContextStore
}

// Result is the common tool output: the command that ran plus its (capped) text.
type Result struct {
	Command   string `json:"command" jsonschema:"the kubectl command that was executed"`
	Output    string `json:"output" jsonschema:"the command output"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"true if the output was capped"`
}

// addTool registers a tool, stamping ReadOnlyHint on it. Every tool in this
// server is read-only by construction, so this is applied uniformly — it lets
// MCP hosts safely auto-approve these tools.
func addTool[In, Out any](s *mcp.Server, t *mcp.Tool, h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)) {
	if t.Annotations == nil {
		t.Annotations = &mcp.ToolAnnotations{}
	}
	t.Annotations.ReadOnlyHint = true
	mcp.AddTool(s, t, h)
}

// RegisterAll wires every tool onto the server.
func RegisterAll(s *mcp.Server, d *Deps) {
	registerContexts(s, d)
	registerInspect(s, d)
	registerLogs(s, d)
	registerEvents(s, d)
	registerMetrics(s, d)
	registerRollout(s, d)
	registerDiagnose(s, d)
	registerAuth(s, d)
}

// kctx resolves the effective context: the per-call override, else the session
// default (empty means kubectl uses the kubeconfig current-context).
func (d *Deps) kctx(override string) string {
	if override != "" {
		return override
	}
	return d.Ctx.Override()
}

// run executes a read-only kubectl command and packages the capped result.
func (d *Deps) run(ctx context.Context, override string, args ...string) (*mcp.CallToolResult, Result, error) {
	out, err := d.Runner.Run(ctx, d.kctx(override), args...)
	if err != nil {
		return nil, Result{}, err
	}
	return finalize(args, out)
}

// finalize caps the output and builds the Result. Returning a nil
// CallToolResult lets the SDK marshal Result into both structured and text content.
func finalize(args []string, out string) (*mcp.CallToolResult, Result, error) {
	capped, truncated := kube.Cap(out)
	return nil, Result{
		Command:   "kubectl " + strings.Join(args, " "),
		Output:    capped,
		Truncated: truncated,
	}, nil
}

// isSecretType reports whether a resource type refers to Secrets.
func isSecretType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "secret", "secrets", "secrets.v1.", "secret.v1.":
		return true
	}
	return false
}
