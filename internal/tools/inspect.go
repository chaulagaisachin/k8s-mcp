package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

type ListNamespacesInput struct {
	Context string `json:"context,omitempty" jsonschema:"override the session context"`
}

type ListResourcesInput struct {
	Type          string `json:"type" jsonschema:"resource kind, e.g. pods, deployments, svc, nodes"`
	Namespace     string `json:"namespace,omitempty" jsonschema:"namespace (default: the context's default namespace)"`
	AllNamespaces bool   `json:"all_namespaces,omitempty" jsonschema:"list across all namespaces"`
	Selector      string `json:"selector,omitempty" jsonschema:"label selector, e.g. app=nginx"`
	Format        string `json:"format,omitempty" jsonschema:"output format: wide (default), json, yaml, or name"`
	Context       string `json:"context,omitempty" jsonschema:"override the session context"`
}

type DescribeResourceInput struct {
	Type      string `json:"type" jsonschema:"resource kind, e.g. pod, deployment"`
	Name      string `json:"name" jsonschema:"resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace (omit for cluster-scoped resources)"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type GetResourceInput struct {
	Type      string `json:"type" jsonschema:"resource kind, e.g. pod, deployment"`
	Name      string `json:"name" jsonschema:"resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace (omit for cluster-scoped resources)"`
	Format    string `json:"format,omitempty" jsonschema:"output format: yaml (default) or json"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type APIResourcesInput struct {
	Context string `json:"context,omitempty" jsonschema:"override the session context"`
}

type ClusterInfoInput struct {
	Context string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerInspect(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_namespaces",
		Description: "List all namespaces in the cluster.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListNamespacesInput) (*mcp.CallToolResult, Result, error) {
		return d.run(ctx, in.Context, "get", "namespaces", "-o", "wide")
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_resources",
		Description: "List resources of a given type. Supports namespace, --all-namespaces, label selector, and output format (wide|json|yaml|name).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListResourcesInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("type", in.Type); err != nil {
			return nil, Result{}, err
		}
		format, err := normalizeFormat(in.Format, "wide", "wide", "json", "yaml", "name")
		if err != nil {
			return nil, Result{}, err
		}
		args := []string{"get", in.Type}
		args, err = appendScope(args, in.Namespace, in.AllNamespaces)
		if err != nil {
			return nil, Result{}, err
		}
		if in.Selector != "" {
			args = append(args, "-l", in.Selector)
		}
		args = append(args, "-o", format)
		return d.runMaybeSecret(ctx, in.Context, in.Type, format, args...)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "describe_resource",
		Description: "Describe a resource (human-readable detail including events). Secret values are shown only as byte counts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DescribeResourceInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("type", in.Type); err != nil {
			return nil, Result{}, err
		}
		if err := kube.SafeArg("name", in.Name); err != nil {
			return nil, Result{}, err
		}
		args := []string{"describe", in.Type, in.Name}
		args, err := appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		return d.run(ctx, in.Context, args...)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_resource",
		Description: "Get a single resource's manifest as yaml (default) or json. Secret .data/.stringData values are redacted unless K8S_MCP_ALLOW_SECRETS is set.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetResourceInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("type", in.Type); err != nil {
			return nil, Result{}, err
		}
		if err := kube.SafeArg("name", in.Name); err != nil {
			return nil, Result{}, err
		}
		format, err := normalizeFormat(in.Format, "yaml", "yaml", "json")
		if err != nil {
			return nil, Result{}, err
		}
		args := []string{"get", in.Type, in.Name}
		args, err = appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		args = append(args, "-o", format)
		return d.runMaybeSecret(ctx, in.Context, in.Type, format, args...)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "api_resources",
		Description: "List the resource types (kinds) available in the cluster, including CRDs.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in APIResourcesInput) (*mcp.CallToolResult, Result, error) {
		return d.run(ctx, in.Context, "api-resources", "-o", "wide")
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "cluster_info",
		Description: "Show client/server version and cluster endpoints (a quick reachability and version check).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ClusterInfoInput) (*mcp.CallToolResult, Result, error) {
		version, err := d.Runner.Run(ctx, d.kctx(in.Context), "version")
		if err != nil {
			return nil, Result{}, err
		}
		info, err := d.Runner.Run(ctx, d.kctx(in.Context), "cluster-info")
		if err != nil {
			return nil, Result{}, err
		}
		combined := "# version\n" + version + "\n# cluster-info\n" + info
		return finalize([]string{"version", "&&", "cluster-info"}, combined)
	})
}

// runMaybeSecret runs a get, redacting Secret data when the type is a Secret and
// the format is structured (json/yaml).
func (d *Deps) runMaybeSecret(ctx context.Context, override, resourceType, format string, args ...string) (*mcp.CallToolResult, Result, error) {
	out, err := d.Runner.Run(ctx, d.kctx(override), args...)
	if err != nil {
		return nil, Result{}, err
	}
	if isSecretType(resourceType) && (format == "json" || format == "yaml") {
		out = kube.RedactSecret(format, out)
	}
	return finalize(args, out)
}

// normalizeFormat validates a format against an allowlist, applying a default.
func normalizeFormat(v, def string, allowed ...string) (string, error) {
	if v == "" {
		return def, nil
	}
	v = strings.ToLower(v)
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", fmt.Errorf("format %q not allowed; use one of: %s", v, strings.Join(allowed, ", "))
}

// appendScope adds either -A or -n <ns> to the args.
func appendScope(args []string, namespace string, all bool) ([]string, error) {
	if all {
		return append(args, "-A"), nil
	}
	return appendNamespace(args, namespace)
}

// appendNamespace adds -n <ns> when a namespace is given (validated).
func appendNamespace(args []string, namespace string) ([]string, error) {
	if namespace == "" {
		return args, nil
	}
	if err := kube.SafeArg("namespace", namespace); err != nil {
		return nil, err
	}
	return append(args, "-n", namespace), nil
}
