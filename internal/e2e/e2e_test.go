//go:build e2e

// Package e2e drives the built k8s-mcp server against a real Kubernetes cluster
// over MCP, asserting on live responses. It is build-tagged `e2e` so it never
// runs in the normal `go test ./...`.
//
// SAFETY: it creates and deletes a namespace, so it refuses to run unless the
// current context is a kind cluster (name starts with "kind-"), or
// K8S_MCP_E2E_ALLOW_ANY=1 is set. Run with: go test -tags e2e ./internal/e2e/
package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/analyze"
	"k8s-mcp/internal/tools"
)

const namespace = "k8s-mcp-e2e"

func TestE2E(t *testing.T) {
	ctx := context.Background()
	guardCluster(t)

	// Seed deterministic workloads and clean up afterwards.
	kubectl(t, "create", "namespace", namespace)
	t.Cleanup(func() {
		_ = exec.Command("kubectl", "delete", "namespace", namespace, "--wait=false").Run()
	})
	kubectl(t, "apply", "-f", "testdata/seed.yaml")

	waitForWaitingReason(t, "crasher", "CrashLoopBackOff")
	waitForWaitingReason(t, "badimage", "ImagePullBackOff", "ErrImagePull")

	// Build and connect. Two sessions: read-only (default) and writes-enabled.
	bin := filepath.Join(t.TempDir(), "k8s-mcp")
	build(t, bin)
	session := connect(t, ctx, bin, false)
	defer session.Close()
	writeSession := connect(t, ctx, bin, true)
	defer writeSession.Close()

	t.Run("tool_annotations", func(t *testing.T) {
		writeNames := map[string]bool{
			"rollout_restart": true, "scale": true, "rollout_undo": true,
			"delete_pod": true, "cordon": true, "uncordon": true,
		}
		res, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Tools) < 26 {
			t.Fatalf("expected >=26 tools, got %d", len(res.Tools))
		}
		for _, tool := range res.Tools {
			ann := tool.Annotations
			if writeNames[tool.Name] {
				if ann == nil || ann.DestructiveHint == nil || !*ann.DestructiveHint {
					t.Errorf("write tool %q missing DestructiveHint", tool.Name)
				}
			} else if ann == nil || !ann.ReadOnlyHint {
				t.Errorf("read tool %q missing ReadOnlyHint", tool.Name)
			}
		}
	})

	t.Run("diagnose_pod_crashloop", func(t *testing.T) {
		out := diagnose(t, ctx, session, "diagnose_pod", map[string]any{"pod": "crasher", "namespace": namespace, "log_lines": 3})
		if out.Healthy {
			t.Fatal("crasher should be unhealthy")
		}
		if !hasCritical(out.Findings) {
			t.Fatalf("expected a critical finding, got %+v", out.Findings)
		}
		if out.Evidence == "" {
			t.Fatal("expected evidence to be attached")
		}
	})

	t.Run("diagnose_pod_imagepull", func(t *testing.T) {
		out := diagnose(t, ctx, session, "diagnose_pod", map[string]any{"pod": "badimage", "namespace": namespace})
		if out.Healthy || findingWith(out.Findings, "image") == nil {
			t.Fatalf("expected image-pull finding, got %+v", out.Findings)
		}
	})

	t.Run("diagnose_namespace", func(t *testing.T) {
		out := diagnose(t, ctx, session, "diagnose_namespace", map[string]any{"namespace": namespace})
		unhealthy, _ := strconv.Atoi(out.Signals["unhealthy_pods"])
		if out.Healthy || unhealthy < 2 {
			t.Fatalf("expected >=2 unhealthy pods, got %+v", out.Signals)
		}
	})

	t.Run("diagnose_deployment_healthy", func(t *testing.T) {
		out := diagnose(t, ctx, session, "diagnose_deployment", map[string]any{"name": "healthy-web", "namespace": namespace})
		if !out.Healthy {
			t.Fatalf("healthy-web should be healthy, got %+v", out.Findings)
		}
	})

	t.Run("list_resources_sees_seeded_pods", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_resources", Arguments: map[string]any{"type": "pods", "namespace": namespace, "format": "name"},
		})
		if err != nil || res.IsError {
			t.Fatalf("list_resources failed: %v isErr=%v", err, res != nil && res.IsError)
		}
		var r tools.Result
		decode(t, res.StructuredContent, &r)
		if !strings.Contains(r.Output, "crasher") || !strings.Contains(r.Output, "badimage") {
			t.Fatalf("expected seeded pods in output: %q", r.Output)
		}
	})

	t.Run("logs_are_redacted", func(t *testing.T) {
		waitForPhase(t, "logger", "Running")
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_logs", Arguments: map[string]any{"pod": "logger", "namespace": namespace, "tail": 20},
		})
		if err != nil || res.IsError {
			t.Fatalf("get_logs failed: %v isErr=%v", err, res != nil && res.IsError)
		}
		var r tools.Result
		decode(t, res.StructuredContent, &r)
		if strings.Contains(r.Output, "hunter2secret") || strings.Contains(r.Output, "AKIAIOSFODNN7EXAMPLE") {
			t.Fatalf("secret survived redaction: %q", r.Output)
		}
		if !strings.Contains(r.Output, "redacted") {
			t.Fatalf("expected redaction markers in logs: %q", r.Output)
		}
	})

	t.Run("auth_can_i", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "auth_can_i", Arguments: map[string]any{"verb": "get", "resource": "pods", "namespace": namespace},
		})
		if err != nil || res.IsError {
			t.Fatalf("auth_can_i failed: %v isErr=%v", err, res != nil && res.IsError)
		}
		var a tools.AuthCanIOutput
		decode(t, res.StructuredContent, &a)
		// kind's default context is cluster-admin, so this must be allowed.
		if !a.Allowed || a.Answer != "yes" {
			t.Fatalf("expected allowed=yes, got %+v", a)
		}
	})

	t.Run("write_refused_when_disabled", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "scale", Arguments: map[string]any{"name": "healthy-web", "namespace": namespace, "replicas": 2},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatal("scale must be refused when writes are disabled")
		}
	})

	t.Run("scale", func(t *testing.T) {
		res, err := writeSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "scale", Arguments: map[string]any{"name": "healthy-web", "namespace": namespace, "replicas": 2},
		})
		if err != nil || res.IsError {
			t.Fatalf("scale failed: %v isErr=%v", err, res != nil && res.IsError)
		}
		waitForReplicas(t, "healthy-web", "2")
	})

	t.Run("scale_dry_run_does_not_apply", func(t *testing.T) {
		res, err := writeSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "scale", Arguments: map[string]any{"name": "healthy-web", "namespace": namespace, "replicas": 5, "dry_run": true},
		})
		if err != nil || res.IsError {
			t.Fatalf("dry-run scale failed: %v isErr=%v", err, res != nil && res.IsError)
		}
		// spec.replicas must still be 2 (dry-run applied nothing).
		out, _ := exec.Command("kubectl", "get", "deploy", "healthy-web", "-n", namespace, "-o=jsonpath={.spec.replicas}").Output()
		if strings.TrimSpace(string(out)) != "2" {
			t.Fatalf("dry-run changed replicas: got %q, want 2", out)
		}
	})

	t.Run("rollout_restart", func(t *testing.T) {
		res, err := writeSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "rollout_restart", Arguments: map[string]any{"name": "healthy-web", "namespace": namespace},
		})
		if err != nil || res.IsError {
			t.Fatalf("rollout_restart failed: %v isErr=%v", err, res != nil && res.IsError)
		}
	})

	t.Run("cordon_uncordon", func(t *testing.T) {
		out, err := exec.Command("kubectl", "get", "nodes", "-o=jsonpath={.items[0].metadata.name}").Output()
		if err != nil {
			t.Fatalf("get node: %v", err)
		}
		node := strings.TrimSpace(string(out))
		for _, tool := range []string{"cordon", "uncordon"} {
			res, err := writeSession.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{"node": node}})
			if err != nil || res.IsError {
				t.Fatalf("%s failed: %v isErr=%v", tool, err, res != nil && res.IsError)
			}
		}
	})
}

// --- helpers ---

func guardCluster(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not found; skipping e2e")
	}
	out, err := exec.Command("kubectl", "config", "current-context").Output()
	if err != nil {
		t.Skip("no current kube context; skipping e2e")
	}
	kctx := strings.TrimSpace(string(out))
	if !strings.HasPrefix(kctx, "kind-") && os.Getenv("K8S_MCP_E2E_ALLOW_ANY") != "1" {
		t.Skipf("refusing to run e2e against non-kind context %q (set K8S_MCP_E2E_ALLOW_ANY=1 to override)", kctx)
	}
}

// connect builds a client session to a freshly spawned server. writes toggles
// K8S_MCP_ENABLE_WRITES on the server process.
func connect(t *testing.T, ctx context.Context, bin string, writes bool) *mcp.ClientSession {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = os.Environ()
	if writes {
		cmd.Env = append(cmd.Env, "K8S_MCP_ENABLE_WRITES=1")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil)
	s, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect (writes=%v): %v", writes, err)
	}
	return s
}

// waitForReplicas polls until a deployment's spec.replicas equals want.
func waitForReplicas(t *testing.T, deploy, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("kubectl", "get", "deploy", deploy, "-n", namespace, "-o=jsonpath={.spec.replicas}").Output()
		if strings.TrimSpace(string(out)) == want {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("deployment %s never reached %s replicas", deploy, want)
}

func build(t *testing.T, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, "k8s-mcp")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, b)
	}
}

func kubectl(t *testing.T, args ...string) {
	t.Helper()
	if b, err := exec.Command("kubectl", args...).CombinedOutput(); err != nil {
		t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, b)
	}
}

// waitForWaitingReason polls until the pod's first container reports one of the
// given waiting reasons.
func waitForWaitingReason(t *testing.T, pod string, reasons ...string) {
	t.Helper()
	jsonpath := "-o=jsonpath={.status.containerStatuses[0].state.waiting.reason}"
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("kubectl", "get", "pod", pod, "-n", namespace, jsonpath).Output()
		got := strings.TrimSpace(string(out))
		for _, r := range reasons {
			if got == r {
				return
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("pod %s never reached waiting reason %v", pod, reasons)
}

// waitForPhase polls until the pod reaches the given .status.phase.
func waitForPhase(t *testing.T, pod, phase string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("kubectl", "get", "pod", pod, "-n", namespace, "-o=jsonpath={.status.phase}").Output()
		if strings.TrimSpace(string(out)) == phase {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("pod %s never reached phase %s", pod, phase)
}

func diagnose(t *testing.T, ctx context.Context, s *mcp.ClientSession, name string, args map[string]any) tools.DiagnoseOutput {
	t.Helper()
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s call error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned isError: %v", name, res.Content)
	}
	var out tools.DiagnoseOutput
	decode(t, res.StructuredContent, &out)
	return out
}

func decode(t *testing.T, v any, into any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func hasCritical(fs []analyze.Finding) bool {
	for _, f := range fs {
		if f.Severity == analyze.Critical {
			return true
		}
	}
	return false
}

func findingWith(fs []analyze.Finding, sub string) *analyze.Finding {
	for i := range fs {
		if strings.Contains(fs[i].Problem, sub) {
			return &fs[i]
		}
	}
	return nil
}
