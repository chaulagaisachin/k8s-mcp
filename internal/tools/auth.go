package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

type AuthCanIInput struct {
	Verb      string `json:"verb,omitempty" jsonschema:"action to check: get, list, watch, create, update, patch, delete, etc. (required unless list=true)"`
	Resource  string `json:"resource,omitempty" jsonschema:"resource to check, e.g. pods, deployments, secrets"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace to check in"`
	List      bool   `json:"list,omitempty" jsonschema:"list all permissions in the namespace (auth can-i --list)"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type AuthCanIOutput struct {
	Allowed bool   `json:"allowed" jsonschema:"whether the action is permitted (n/a for list)"`
	Answer  string `json:"answer" jsonschema:"'yes'/'no' for a single check, or the permission table for list"`
	Command string `json:"command"`
}

func registerAuth(s *mcp.Server, d *Deps) {
	addTool(s, &mcp.Tool{
		Name:        "auth_can_i",
		Description: "Check whether the current credentials may perform an action (wraps 'kubectl auth can-i'). Use list=true to list all permissions in a namespace.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AuthCanIInput) (*mcp.CallToolResult, AuthCanIOutput, error) {
		if in.List {
			args := []string{"auth", "can-i", "--list"}
			args, err := appendNamespace(args, in.Namespace)
			if err != nil {
				return nil, AuthCanIOutput{}, err
			}
			out, err := d.Runner.Run(ctx, d.kctx(in.Context), args...)
			if err != nil {
				return nil, AuthCanIOutput{}, err
			}
			capped, _ := kube.Cap(out)
			return nil, AuthCanIOutput{Allowed: true, Answer: capped, Command: "kubectl " + strings.Join(args, " ")}, nil
		}

		if err := kube.SafeArg("verb", in.Verb); err != nil {
			return nil, AuthCanIOutput{}, err
		}
		args := []string{"auth", "can-i", in.Verb}
		if in.Resource != "" {
			if err := kube.SafeArg("resource", in.Resource); err != nil {
				return nil, AuthCanIOutput{}, err
			}
			args = append(args, in.Resource)
		}
		args, err := appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, AuthCanIOutput{}, err
		}
		// `can-i` signals "no" via exit code 1, so a non-zero exit is a valid answer.
		answer, ok, err := d.Runner.RunAllowNonZero(ctx, d.kctx(in.Context), args...)
		if err != nil {
			return nil, AuthCanIOutput{}, err
		}
		return nil, AuthCanIOutput{Allowed: ok, Answer: answer, Command: "kubectl " + strings.Join(args, " ")}, nil
	})
}
