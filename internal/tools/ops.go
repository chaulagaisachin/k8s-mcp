package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
)

// scaleTypes are the workload kinds that support `kubectl scale`.
var scaleTypes = map[string]bool{
	"deployment": true, "deploy": true,
	"statefulset": true, "sts": true,
	"replicaset": true, "rs": true,
}

// Every write tool's description ends with this so the LLM warns the user of the
// impact BEFORE executing.
const warnUser = " IMPACTFUL — before running, tell the user what this will affect and confirm they want it. Requires K8S_MCP_ENABLE_WRITES."

type RolloutRestartInput struct {
	Type      string `json:"type,omitempty" jsonschema:"deployment (default), statefulset, or daemonset"`
	Name      string `json:"name" jsonschema:"workload name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"preview via server-side dry-run instead of applying"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type ScaleInput struct {
	Type      string `json:"type,omitempty" jsonschema:"deployment (default), statefulset, or replicaset"`
	Name      string `json:"name" jsonschema:"workload name"`
	Replicas  int    `json:"replicas" jsonschema:"desired replica count (>= 0)"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"preview via server-side dry-run instead of applying"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type RolloutUndoInput struct {
	Type       string `json:"type,omitempty" jsonschema:"deployment (default), statefulset, or daemonset"`
	Name       string `json:"name" jsonschema:"workload name"`
	ToRevision int    `json:"to_revision,omitempty" jsonschema:"revision to roll back to (0 = previous)"`
	Namespace  string `json:"namespace,omitempty" jsonschema:"namespace"`
	DryRun     bool   `json:"dry_run,omitempty" jsonschema:"preview via server-side dry-run instead of applying"`
	Context    string `json:"context,omitempty" jsonschema:"override the session context"`
}

type DeletePodInput struct {
	Pod       string `json:"pod" jsonschema:"pod name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"preview via server-side dry-run instead of applying"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type NodeOpInput struct {
	Node    string `json:"node" jsonschema:"node name"`
	Context string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerOps(s *mcp.Server, d *Deps) {
	addWriteTool(s, &mcp.Tool{
		Name:        "rollout_restart",
		Description: "Restart a deployment/statefulset/daemonset (rolling recreate of ALL its pods; can briefly disrupt traffic and drop in-flight requests)." + warnUser,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RolloutRestartInput) (*mcp.CallToolResult, Result, error) {
		target, err := rolloutTarget(in.Type, in.Name)
		if err != nil {
			return nil, Result{}, err
		}
		args := appendDryRun([]string{"rollout", "restart", target}, in.DryRun)
		args, err = appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		warn := fmt.Sprintf("MEDIUM IMPACT: restarts all pods of %s in a rolling fashion — expect brief disruption and possible dropped in-flight requests.", target)
		return d.runWrite(ctx, in.Context, dryRunWarn(warn, in.DryRun), args...)
	})

	addWriteTool(s, &mcp.Tool{
		Name:        "scale",
		Description: "Set the replica count of a deployment/statefulset/replicaset. Scaling DOWN reduces capacity/availability; scaling to 0 takes the workload completely OFFLINE." + warnUser,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ScaleInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("name", in.Name); err != nil {
			return nil, Result{}, err
		}
		typ := in.Type
		if typ == "" {
			typ = "deployment"
		}
		if err := kube.SafeArg("type", typ); err != nil {
			return nil, Result{}, err
		}
		if !scaleTypes[strings.ToLower(typ)] {
			return nil, Result{}, fmt.Errorf("type %q is not scalable; use deployment, statefulset, or replicaset", typ)
		}
		if in.Replicas < 0 {
			return nil, Result{}, fmt.Errorf("replicas must be >= 0, got %d", in.Replicas)
		}
		args := []string{"scale", typ + "/" + in.Name, fmt.Sprintf("--replicas=%d", in.Replicas)}
		args = appendDryRun(args, in.DryRun)
		args, err := appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		var warn string
		if in.Replicas == 0 {
			warn = fmt.Sprintf("HIGH IMPACT: scaling %s/%s to 0 will take it completely OFFLINE (no running pods).", typ, in.Name)
		} else {
			warn = fmt.Sprintf("IMPACT: setting %s/%s to %d replicas — scaling below the current count reduces capacity and availability.", typ, in.Name, in.Replicas)
		}
		return d.runWrite(ctx, in.Context, dryRunWarn(warn, in.DryRun), args...)
	})

	addWriteTool(s, &mcp.Tool{
		Name:        "rollout_undo",
		Description: "Roll back a deployment/statefulset/daemonset to a previous revision (reverts the running version; may reintroduce old bugs/config)." + warnUser,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RolloutUndoInput) (*mcp.CallToolResult, Result, error) {
		target, err := rolloutTarget(in.Type, in.Name)
		if err != nil {
			return nil, Result{}, err
		}
		args := []string{"rollout", "undo", target}
		if in.ToRevision > 0 {
			args = append(args, fmt.Sprintf("--to-revision=%d", in.ToRevision))
		}
		args = appendDryRun(args, in.DryRun)
		args, err = appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		warn := fmt.Sprintf("MEDIUM IMPACT: rolls %s back to a previous revision — the currently running version/config will be replaced.", target)
		return d.runWrite(ctx, in.Context, dryRunWarn(warn, in.DryRun), args...)
	})

	addWriteTool(s, &mcp.Tool{
		Name:        "delete_pod",
		Description: "Delete a pod. A controller-managed pod is recreated (self-healing); a bare pod is gone for good, and in-flight work on the pod is lost." + warnUser,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DeletePodInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("pod", in.Pod); err != nil {
			return nil, Result{}, err
		}
		args := appendDryRun([]string{"delete", "pod", in.Pod}, in.DryRun)
		args, err := appendNamespace(args, in.Namespace)
		if err != nil {
			return nil, Result{}, err
		}
		warn := fmt.Sprintf("IMPACT: terminates pod %s — in-flight work is lost. Controller-managed pods are recreated; a bare pod is NOT.", in.Pod)
		return d.runWrite(ctx, in.Context, dryRunWarn(warn, in.DryRun), args...)
	})

	addWriteTool(s, &mcp.Tool{
		Name:        "cordon",
		Description: "Mark a node unschedulable (cordon). Existing pods keep running, but no NEW pods will be scheduled there." + warnUser,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NodeOpInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("node", in.Node); err != nil {
			return nil, Result{}, err
		}
		warn := fmt.Sprintf("IMPACT: %s will no longer accept new pods (existing pods keep running). Remember to uncordon it later.", in.Node)
		return d.runWrite(ctx, in.Context, warn, "cordon", in.Node)
	})

	addWriteTool(s, &mcp.Tool{
		Name:        "uncordon",
		Description: "Mark a node schedulable again (uncordon). Low impact — restores normal scheduling." + warnUser,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NodeOpInput) (*mcp.CallToolResult, Result, error) {
		if err := kube.SafeArg("node", in.Node); err != nil {
			return nil, Result{}, err
		}
		warn := fmt.Sprintf("LOW IMPACT: %s can schedule pods again.", in.Node)
		return d.runWrite(ctx, in.Context, warn, "uncordon", in.Node)
	})
}

// appendDryRun adds server-side dry-run when requested.
func appendDryRun(args []string, dryRun bool) []string {
	if dryRun {
		return append(args, "--dry-run=server")
	}
	return args
}

// dryRunWarn prefixes the impact warning with a dry-run note when previewing.
func dryRunWarn(warn string, dryRun bool) string {
	if dryRun {
		return "(dry-run: nothing was changed) " + warn
	}
	return warn
}
