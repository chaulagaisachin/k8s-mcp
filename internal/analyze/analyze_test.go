package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// findingBy returns the first finding whose problem contains sub.
func findingBy(r Report, sub string) *Finding {
	for i := range r.Findings {
		if strings.Contains(r.Findings[i].Problem, sub) {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestPod_CrashLoop(t *testing.T) {
	r, err := Pod(load(t, "crashloop.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Healthy {
		t.Fatal("expected unhealthy")
	}
	f := findingBy(r, "CrashLoopBackOff")
	if f == nil {
		t.Fatalf("no crashloop finding: %+v", r.Findings)
	}
	if f.Severity != Critical || f.Container != "app" {
		t.Fatalf("bad finding: %+v", f)
	}
	if !strings.Contains(f.Evidence, "CrashLoopBackOff") {
		t.Fatalf("evidence should cite the reason: %q", f.Evidence)
	}
}

func TestPod_OOMKilled(t *testing.T) {
	r, _ := Pod(load(t, "oomkilled.json"), nil)
	f := findingBy(r, "CrashLoopBackOff")
	if f == nil || !strings.Contains(f.Cause, "OOMKilled") {
		t.Fatalf("expected OOMKilled cause, got %+v", f)
	}
}

func TestPod_ImagePull(t *testing.T) {
	r, _ := Pod(load(t, "imagepull.json"), nil)
	if f := findingBy(r, "image cannot be pulled"); f == nil || f.Severity != Critical {
		t.Fatalf("expected image pull finding, got %+v", r.Findings)
	}
}

func TestPod_InitCrashLoop(t *testing.T) {
	r, _ := Pod(load(t, "init_crashloop.json"), nil)
	f := findingBy(r, "CrashLoopBackOff")
	if f == nil || f.Container != "init:migrate" {
		t.Fatalf("expected init container attribution, got %+v", f)
	}
}

func TestPod_RunningNotReady(t *testing.T) {
	r, _ := Pod(load(t, "running_not_ready.json"), nil)
	if f := findingBy(r, "not Ready"); f == nil || f.Severity != Warning {
		t.Fatalf("expected probe/not-ready finding, got %+v", r.Findings)
	}
}

func TestPod_PendingScheduling(t *testing.T) {
	events := []Event{{Type: "Warning", Reason: "FailedScheduling", Message: "0/7 nodes are available: 7 Insufficient memory."}}
	r, _ := Pod(load(t, "pending.json"), events)
	f := findingBy(r, "cannot be scheduled")
	if f == nil || !strings.Contains(f.Cause, "Insufficient memory") {
		t.Fatalf("expected scheduling finding with reason, got %+v", r.Findings)
	}
}

func TestPod_VolumeMount(t *testing.T) {
	events := []Event{{Type: "Warning", Reason: "FailedMount", Message: "Unable to attach or mount volumes: unbound PersistentVolumeClaim"}}
	r, _ := Pod(load(t, "volume.json"), events)
	if f := findingBy(r, "volume mount"); f == nil || f.Severity != Critical {
		t.Fatalf("expected volume finding, got %+v", r.Findings)
	}
}

func TestPod_Healthy(t *testing.T) {
	r, _ := Pod(load(t, "healthy.json"), nil)
	if !r.Healthy || len(r.Findings) != 0 {
		t.Fatalf("expected healthy with no findings, got %+v", r)
	}
	if r.Signals["ready"] != "1/1" {
		t.Fatalf("expected ready 1/1, got %q", r.Signals["ready"])
	}
}

func TestPod_CompletedNotFlagged(t *testing.T) {
	r, _ := Pod(load(t, "completed.json"), nil)
	if !r.Healthy || len(r.Findings) != 0 {
		t.Fatalf("completed job pod should not be flagged, got %+v", r.Findings)
	}
}

func TestPod_Evicted(t *testing.T) {
	r, _ := Pod(load(t, "evicted.json"), nil)
	f := findingBy(r, "Evicted")
	if f == nil || f.Severity != Warning {
		t.Fatalf("expected distinct evicted finding, got %+v", r.Findings)
	}
}

func TestPod_SortedBySeverity(t *testing.T) {
	// A pod with both a critical (image) and info finding: critical must come first.
	events := []Event{{Type: "Warning", Reason: "Unhealthy", Message: "probe failed"}}
	r, _ := Pod(load(t, "imagepull.json"), events)
	if len(r.Findings) < 2 {
		t.Skip("need multiple findings")
	}
	for i := 1; i < len(r.Findings); i++ {
		if severityRank[r.Findings[i-1].Severity] > severityRank[r.Findings[i].Severity] {
			t.Fatalf("findings not sorted by severity: %+v", r.Findings)
		}
	}
}

func TestWorkload_Deployment_Stuck(t *testing.T) {
	r, selector, err := Workload(load(t, "deployment_stuck.json"), "deployment")
	if err != nil {
		t.Fatal(err)
	}
	if r.Healthy {
		t.Fatal("expected unhealthy deployment")
	}
	if findingBy(r, "ProgressDeadlineExceeded") == nil {
		t.Fatalf("expected stalled-rollout finding, got %+v", r.Findings)
	}
	if selector != "app=web,tier=frontend" {
		t.Fatalf("bad selector: %q", selector)
	}
}

func TestWorkload_DaemonSet_Degraded(t *testing.T) {
	r, selector, err := Workload(load(t, "daemonset_degraded.json"), "daemonset")
	if err != nil {
		t.Fatal(err)
	}
	if r.Healthy {
		t.Fatal("expected unhealthy daemonset")
	}
	f := findingBy(r, "pods scheduled")
	if f == nil {
		t.Fatalf("expected daemonset availability finding (pods scheduled), got %+v", r.Findings)
	}
	if r.Signals["desired"] != "6" || r.Signals["available"] != "4" {
		t.Fatalf("daemonset signals wrong: %+v", r.Signals)
	}
	if selector != "k8s-app=canal" {
		t.Fatalf("bad selector: %q", selector)
	}
}

func TestWorkload_DaemonSet_Healthy(t *testing.T) {
	// A DaemonSet with all pods ready must NOT be flagged (regression: the old
	// deployment-only analyzer reported a false "0/1 replicas available").
	r, _, err := Workload(load(t, "daemonset_healthy.json"), "daemonset")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Healthy || len(r.Findings) != 0 {
		t.Fatalf("healthy daemonset should not be flagged, got %+v", r.Findings)
	}
}

func TestWorkload_StatefulSet_Healthy(t *testing.T) {
	r, selector, err := Workload(load(t, "statefulset_healthy.json"), "statefulset")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Healthy || len(r.Findings) != 0 {
		t.Fatalf("healthy statefulset should not be flagged, got %+v", r.Findings)
	}
	if r.Signals["ready"] != "3" || selector != "app=postgres" {
		t.Fatalf("statefulset signals/selector wrong: %+v selector=%q", r.Signals, selector)
	}
}

func TestNode_MemoryPressure(t *testing.T) {
	r, _ := Node(load(t, "node_pressure.json"))
	if f := findingBy(r, "MemoryPressure"); f == nil || f.Severity != Warning {
		t.Fatalf("expected memory pressure finding, got %+v", r.Findings)
	}
	if r.Signals["cpu_capacity"] != "8" {
		t.Fatalf("expected cpu capacity signal, got %q", r.Signals["cpu_capacity"])
	}
}

func TestParseEvents(t *testing.T) {
	raw := []byte(`{"items":[{"type":"Warning","reason":"FailedMount","message":"boom","count":3,"involvedObject":{"name":"p1"}}]}`)
	ev, err := ParseEvents(raw)
	if err != nil || len(ev) != 1 || ev[0].Reason != "FailedMount" || ev[0].Object != "p1" {
		t.Fatalf("parse events failed: %+v err=%v", ev, err)
	}
}
