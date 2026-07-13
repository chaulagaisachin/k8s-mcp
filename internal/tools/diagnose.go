package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/analyze"
	"k8s-mcp/internal/kube"
)

const defaultDiagLogLines = 50

// DiagnoseOutput is the structured result of a diagnostic tool.
type DiagnoseOutput struct {
	Target   string            `json:"target"`
	Kind     string            `json:"kind"`
	Healthy  bool              `json:"healthy"`
	Signals  map[string]string `json:"signals,omitempty"`
	Findings []analyze.Finding `json:"findings"`
	Pods     []analyze.Report  `json:"pods,omitempty"`     // drill-down for deployment/namespace
	Events   []string          `json:"events,omitempty"`   // notable namespace warning events
	Evidence string            `json:"evidence,omitempty"` // raw describe/logs, capped
	Notes    []string          `json:"notes,omitempty"`    // partial-result / limitation notes
}

type DiagnosePodInput struct {
	Pod       string `json:"pod" jsonschema:"pod name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	LogLines  int    `json:"log_lines,omitempty" jsonschema:"lines of log evidence per container (default 50)"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type DiagnoseDeploymentInput struct {
	Type      string `json:"type,omitempty" jsonschema:"deployment (default), statefulset, or daemonset"`
	Name      string `json:"name" jsonschema:"workload name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type DiagnoseNamespaceInput struct {
	Namespace string `json:"namespace" jsonschema:"namespace to triage"`
	Context   string `json:"context,omitempty" jsonschema:"override the session context"`
}

type DiagnoseNodeInput struct {
	Node    string `json:"node" jsonschema:"node name"`
	Context string `json:"context,omitempty" jsonschema:"override the session context"`
}

func registerDiagnose(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "diagnose_pod",
		Description: "Diagnose why a pod is unhealthy: detects CrashLoopBackOff, OOMKilled, image-pull, config, init-container, probe, scheduling and volume failures, with describe+logs as evidence. Read-only; detects Kubernetes-level failures, not application bugs.",
	}, d.diagnosePod)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "diagnose_deployment",
		Description: "Diagnose a deployment/statefulset/daemonset: rollout state and replica availability, drilling into its unhealthy pods.",
	}, d.diagnoseDeployment)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "diagnose_namespace",
		Description: "Triage a namespace: lists not-ready/failing pods with their top finding plus recent warning events. Skips completed jobs.",
	}, d.diagnoseNamespace)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "diagnose_node",
		Description: "Diagnose a node: Ready/MemoryPressure/DiskPressure/PIDPressure conditions, cordon status, and capacity.",
	}, d.diagnoseNode)
}

func (d *Deps) diagnosePod(ctx context.Context, _ *mcp.CallToolRequest, in DiagnosePodInput) (*mcp.CallToolResult, DiagnoseOutput, error) {
	if err := kube.SafeArg("pod", in.Pod); err != nil {
		return nil, DiagnoseOutput{}, err
	}
	nsArgs, err := appendNamespace(nil, in.Namespace)
	if err != nil {
		return nil, DiagnoseOutput{}, err
	}
	kctx := in.Context

	raw, err := d.getJSON(ctx, kctx, "pod", in.Pod, nsArgs)
	if err != nil {
		return nil, DiagnoseOutput{}, notFoundOr("pod", in.Pod, in.Namespace, err)
	}

	out := DiagnoseOutput{Target: in.Pod, Kind: "pod"}
	events, evErr := d.objectEvents(ctx, kctx, in.Pod, nsArgs)
	if evErr != nil {
		out.Notes = append(out.Notes, "could not fetch events: "+evErr.Error())
	}

	report, err := analyze.Pod(raw, events)
	if err != nil {
		return nil, DiagnoseOutput{}, err
	}
	out.Healthy = report.Healthy
	out.Signals = report.Signals
	out.Findings = report.Findings

	logLines := in.LogLines
	if logLines <= 0 {
		logLines = defaultDiagLogLines
	}
	out.Evidence = d.podEvidence(ctx, kctx, in.Pod, nsArgs, failingContainer(report), logLines)
	out.Notes = append(out.Notes, "detects Kubernetes-level failures, not application bugs — review the log evidence for app errors")
	return nil, out, nil
}

func (d *Deps) diagnoseDeployment(ctx context.Context, _ *mcp.CallToolRequest, in DiagnoseDeploymentInput) (*mcp.CallToolResult, DiagnoseOutput, error) {
	typ := in.Type
	if typ == "" {
		typ = "deployment"
	}
	if !rolloutTypes[strings.ToLower(typ)] {
		return nil, DiagnoseOutput{}, fmt.Errorf("type %q not supported; use deployment, statefulset, or daemonset", typ)
	}
	if err := kube.SafeArg("name", in.Name); err != nil {
		return nil, DiagnoseOutput{}, err
	}
	nsArgs, err := appendNamespace(nil, in.Namespace)
	if err != nil {
		return nil, DiagnoseOutput{}, err
	}
	kctx := in.Context

	raw, err := d.getJSON(ctx, kctx, typ, in.Name, nsArgs)
	if err != nil {
		return nil, DiagnoseOutput{}, notFoundOr(typ, in.Name, in.Namespace, err)
	}
	report, selector, err := analyze.Deployment(raw)
	if err != nil {
		return nil, DiagnoseOutput{}, err
	}
	out := DiagnoseOutput{Target: in.Name, Kind: typ, Healthy: report.Healthy, Signals: report.Signals, Findings: report.Findings}

	if selector != "" {
		podsRaw, perr := d.Runner.Run(ctx, d.kctx(kctx), append([]string{"get", "pods", "-l", selector, "-o", "json"}, nsArgs...)...)
		if perr != nil {
			out.Notes = append(out.Notes, "could not list pods: "+perr.Error())
		} else {
			for _, item := range podItems([]byte(podsRaw)) {
				pr, aerr := analyze.Pod(item, nil)
				if aerr == nil && !pr.Healthy {
					out.Pods = append(out.Pods, pr)
					out.Healthy = false
				}
			}
		}
	}
	return nil, out, nil
}

func (d *Deps) diagnoseNamespace(ctx context.Context, _ *mcp.CallToolRequest, in DiagnoseNamespaceInput) (*mcp.CallToolResult, DiagnoseOutput, error) {
	if err := kube.SafeArg("namespace", in.Namespace); err != nil {
		return nil, DiagnoseOutput{}, err
	}
	kctx := in.Context
	podsRaw, err := d.Runner.Run(ctx, d.kctx(kctx), "get", "pods", "-n", in.Namespace, "-o", "json")
	if err != nil {
		return nil, DiagnoseOutput{}, err
	}
	items := podItems([]byte(podsRaw))
	out := DiagnoseOutput{Target: in.Namespace, Kind: "namespace", Healthy: true}
	for _, item := range items {
		pr, aerr := analyze.Pod(item, nil)
		if aerr != nil {
			continue
		}
		if pr.Signals["phase"] == "Succeeded" { // completed job pods are not failures
			continue
		}
		if !pr.Healthy {
			out.Pods = append(out.Pods, pr)
			out.Healthy = false
		}
	}
	out.Signals = map[string]string{
		"total_pods":     fmt.Sprintf("%d", len(items)),
		"unhealthy_pods": fmt.Sprintf("%d", len(out.Pods)),
	}
	if evRaw, evErr := d.Runner.Run(ctx, d.kctx(kctx), "get", "events", "-n", in.Namespace, "--field-selector", "type=Warning", "-o", "json"); evErr == nil {
		if events, perr := analyze.ParseEvents([]byte(evRaw)); perr == nil {
			out.Events = topEvents(events, 10)
		}
	}
	return nil, out, nil
}

func (d *Deps) diagnoseNode(ctx context.Context, _ *mcp.CallToolRequest, in DiagnoseNodeInput) (*mcp.CallToolResult, DiagnoseOutput, error) {
	if err := kube.SafeArg("node", in.Node); err != nil {
		return nil, DiagnoseOutput{}, err
	}
	raw, err := d.getJSON(ctx, in.Context, "node", in.Node, nil)
	if err != nil {
		return nil, DiagnoseOutput{}, notFoundOr("node", in.Node, "", err)
	}
	report, err := analyze.Node(raw)
	if err != nil {
		return nil, DiagnoseOutput{}, err
	}
	return nil, DiagnoseOutput{
		Target: in.Node, Kind: "node", Healthy: report.Healthy,
		Signals: report.Signals, Findings: report.Findings,
	}, nil
}

// --- helpers ---

// getJSON fetches one object as JSON.
func (d *Deps) getJSON(ctx context.Context, kctx, kind, name string, nsArgs []string) ([]byte, error) {
	args := append([]string{"get", kind, name, "-o", "json"}, nsArgs...)
	out, err := d.Runner.Run(ctx, d.kctx(kctx), args...)
	return []byte(out), err
}

// objectEvents fetches warning events scoped to a single object.
func (d *Deps) objectEvents(ctx context.Context, kctx, name string, nsArgs []string) ([]analyze.Event, error) {
	args := append([]string{"get", "events", "--field-selector", "involvedObject.name=" + name, "-o", "json"}, nsArgs...)
	out, err := d.Runner.Run(ctx, d.kctx(kctx), args...)
	if err != nil {
		return nil, err
	}
	return analyze.ParseEvents([]byte(out))
}

// podEvidence gathers describe + current/previous logs, capped. Failures in
// sub-fetches are tolerated (partial evidence).
func (d *Deps) podEvidence(ctx context.Context, kctx, pod string, nsArgs []string, container string, logLines int) string {
	var b strings.Builder
	if desc, err := d.Runner.Run(ctx, d.kctx(kctx), append([]string{"describe", "pod", pod}, nsArgs...)...); err == nil {
		b.WriteString("# describe\n" + desc + "\n")
	}
	logArgs := []string{"logs", pod}
	logArgs = append(logArgs, nsArgs...)
	if container != "" {
		logArgs = append(logArgs, "-c", container)
	}
	logArgs = append(logArgs, fmt.Sprintf("--tail=%d", logLines))
	if cur, err := d.Runner.Run(ctx, d.kctx(kctx), logArgs...); err == nil && strings.TrimSpace(cur) != "" {
		b.WriteString("# logs (current)\n" + cur + "\n")
	}
	if prev, err := d.Runner.Run(ctx, d.kctx(kctx), append(logArgs, "--previous")...); err == nil && strings.TrimSpace(prev) != "" {
		b.WriteString("# logs (previous)\n" + prev + "\n")
	}
	capped, _ := kube.Cap(b.String())
	return capped
}

// failingContainer returns the container name of the first finding, stripping
// the "init:" prefix so it can be passed to kubectl logs -c.
func failingContainer(r analyze.Report) string {
	for _, f := range r.Findings {
		if f.Container != "" {
			return strings.TrimPrefix(f.Container, "init:")
		}
	}
	return ""
}

// podItems splits a `kubectl get pods -o json` list into per-pod JSON.
func podItems(raw []byte) [][]byte {
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([][]byte, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it)
	}
	return out
}

// topEvents returns up to n event summaries.
func topEvents(events []analyze.Event, n int) []string {
	var out []string
	for _, e := range events {
		if e.Type != "Warning" {
			continue
		}
		out = append(out, fmt.Sprintf("%s/%s: %s", e.Object, e.Reason, e.Message))
		if len(out) >= n {
			break
		}
	}
	return out
}

// notFoundOr converts a kubectl not-found error into a clean message.
func notFoundOr(kind, name, namespace string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found")) {
		where := ""
		if namespace != "" {
			where = " in namespace " + namespace
		}
		return fmt.Errorf("%s %q not found%s", kind, name, where)
	}
	return err
}
