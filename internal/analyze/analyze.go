// Package analyze contains pure, kubectl-free diagnostic rules. Each entrypoint
// takes parsed `kubectl ... -o json` (and, where useful, warning events) and
// returns grounded findings. Being pure makes the rules unit-testable against
// canned JSON fixtures.
package analyze

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Severity levels, ordered most-severe first for sorting.
const (
	Critical = "critical"
	Warning  = "warning"
	Info     = "info"
)

var severityRank = map[string]int{Critical: 0, Warning: 1, Info: 2}

// Finding is one grounded diagnostic result.
type Finding struct {
	Severity   string `json:"severity"`
	Problem    string `json:"problem"`
	Container  string `json:"container,omitempty"`
	Cause      string `json:"cause,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	Evidence   string `json:"evidence"`
}

// Report is the analyzer output for one target.
type Report struct {
	Name     string            `json:"name"`
	Healthy  bool              `json:"healthy"`
	Signals  map[string]string `json:"signals"`
	Findings []Finding         `json:"findings"`
}

// Event is a minimal Kubernetes event.
type Event struct {
	Type    string
	Reason  string
	Message string
	Count   int
	Object  string
}

// ParseEvents extracts events from `kubectl get events -o json` output.
func ParseEvents(raw []byte) ([]Event, error) {
	var list struct {
		Items []struct {
			Type           string `json:"type"`
			Reason         string `json:"reason"`
			Message        string `json:"message"`
			Count          int    `json:"count"`
			InvolvedObject struct {
				Name string `json:"name"`
			} `json:"involvedObject"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, Event{
			Type: it.Type, Reason: it.Reason, Message: strings.TrimSpace(it.Message),
			Count: it.Count, Object: it.InvolvedObject.Name,
		})
	}
	return out, nil
}

// --- kubectl JSON shapes (minimal subset) ---

type objectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type containerState struct {
	Waiting    *stateWaiting    `json:"waiting"`
	Running    *stateRunning    `json:"running"`
	Terminated *stateTerminated `json:"terminated"`
}

type stateWaiting struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type stateRunning struct {
	StartedAt string `json:"startedAt"`
}

type stateTerminated struct {
	ExitCode int    `json:"exitCode"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

type containerStatus struct {
	Name         string         `json:"name"`
	Ready        bool           `json:"ready"`
	RestartCount int            `json:"restartCount"`
	State        containerState `json:"state"`
	LastState    containerState `json:"lastState"`
}

type pod struct {
	Metadata objectMeta `json:"metadata"`
	Status   struct {
		Phase                 string            `json:"phase"`
		Reason                string            `json:"reason"`
		Message               string            `json:"message"`
		Conditions            []condition       `json:"conditions"`
		ContainerStatuses     []containerStatus `json:"containerStatuses"`
		InitContainerStatuses []containerStatus `json:"initContainerStatuses"`
	} `json:"status"`
}

// Pod analyzes a single pod's JSON plus its warning events.
func Pod(raw []byte, events []Event) (Report, error) {
	var p pod
	if err := json.Unmarshal(raw, &p); err != nil {
		return Report{}, err
	}
	r := Report{Name: p.Metadata.Name, Signals: map[string]string{}}
	covered := map[string]bool{}

	ready, total := 0, len(p.Status.ContainerStatuses)
	restarts := 0
	for _, c := range p.Status.ContainerStatuses {
		if c.Ready {
			ready++
		}
		restarts += c.RestartCount
	}
	r.Signals["phase"] = p.Status.Phase
	r.Signals["ready"] = fmt.Sprintf("%d/%d", ready, total)
	r.Signals["restarts"] = fmt.Sprintf("%d", restarts)
	if p.Status.Reason != "" {
		r.Signals["reason"] = p.Status.Reason
	}

	// Evicted pods are distinct from crashes.
	if p.Status.Phase == "Failed" && p.Status.Reason == "Evicted" {
		r.Findings = append(r.Findings, Finding{
			Severity: Warning, Problem: "pod was Evicted",
			Cause:      p.Status.Message,
			Suggestion: "check node resource pressure (disk/memory) and pod resource requests",
			Evidence:   "status.phase=Failed status.reason=Evicted",
		})
		covered["evicted"] = true
	}

	analyzeContainers(&r, p.Status.InitContainerStatuses, true, covered)
	analyzeContainers(&r, p.Status.ContainerStatuses, false, covered)

	// Pending: the real reason is usually in a FailedScheduling event.
	if p.Status.Phase == "Pending" && !anyRunning(p) {
		if sched := findEvent(events, "FailedScheduling"); sched != nil {
			r.Findings = append(r.Findings, Finding{
				Severity: Critical, Problem: "pod cannot be scheduled",
				Cause:      sched.Message,
				Suggestion: "check node capacity, taints/tolerations, and affinity/selectors",
				Evidence:   "event FailedScheduling: " + sched.Message,
			})
			covered["scheduling"] = true
		}
	}

	mineEvents(&r, events, covered)

	sortFindings(r.Findings)
	r.Healthy = !hasProblem(r.Findings)
	return r, nil
}

// analyzeContainers walks container statuses and appends findings.
func analyzeContainers(r *Report, statuses []containerStatus, init bool, covered map[string]bool) {
	for _, c := range statuses {
		name := c.Name
		if init {
			name = "init:" + c.Name
		}
		switch {
		case c.State.Waiting != nil:
			analyzeWaiting(r, name, c, covered)
		case c.State.Terminated != nil:
			t := c.State.Terminated
			if t.Reason != "Completed" && t.ExitCode != 0 {
				r.Findings = append(r.Findings, Finding{
					Severity: Critical, Container: name,
					Problem:  fmt.Sprintf("container terminated with exit code %d", t.ExitCode),
					Cause:    terminationCause(t),
					Evidence: fmt.Sprintf("state.terminated exitCode=%d reason=%s", t.ExitCode, t.Reason),
				})
			}
		case c.State.Running != nil && !c.Ready:
			r.Findings = append(r.Findings, Finding{
				Severity: Warning, Container: name,
				Problem:    "container is running but not Ready",
				Cause:      "readiness probe likely failing",
				Suggestion: "check the readiness probe and application startup; see Unhealthy events",
				Evidence:   "state.running with ready=false",
			})
			covered["probe"] = true
		}
	}
}

// analyzeWaiting maps a container's waiting reason to a finding.
func analyzeWaiting(r *Report, name string, c containerStatus, covered map[string]bool) {
	w := c.State.Waiting
	switch w.Reason {
	case "CrashLoopBackOff":
		r.Findings = append(r.Findings, Finding{
			Severity: Critical, Container: name,
			Problem:    "container in CrashLoopBackOff",
			Cause:      terminationCause(c.LastState.Terminated),
			Suggestion: "inspect container logs (including --previous) for the crash cause",
			Evidence:   fmt.Sprintf("state.waiting.reason=CrashLoopBackOff restartCount=%d", c.RestartCount),
		})
		covered["crashloop"] = true
	case "ImagePullBackOff", "ErrImagePull":
		r.Findings = append(r.Findings, Finding{
			Severity: Critical, Container: name,
			Problem:    "image cannot be pulled",
			Cause:      w.Message,
			Suggestion: "verify the image name/tag exists and imagePullSecrets are correct",
			Evidence:   "state.waiting.reason=" + w.Reason,
		})
		covered["image"] = true
	case "CreateContainerConfigError":
		r.Findings = append(r.Findings, Finding{
			Severity: Critical, Container: name,
			Problem:    "container config error",
			Cause:      w.Message,
			Suggestion: "check referenced ConfigMaps/Secrets and env/volume mounts exist",
			Evidence:   "state.waiting.reason=CreateContainerConfigError",
		})
		covered["config"] = true
	case "ContainerCreating", "PodInitializing":
		r.Findings = append(r.Findings, Finding{
			Severity: Info, Container: name,
			Problem:  "container still being created",
			Evidence: "state.waiting.reason=" + w.Reason,
		})
	default:
		if w.Reason != "" {
			r.Findings = append(r.Findings, Finding{
				Severity: Warning, Container: name,
				Problem:  "container waiting: " + w.Reason,
				Cause:    w.Message,
				Evidence: "state.waiting.reason=" + w.Reason,
			})
		}
	}
}

// mineEvents turns warning events into findings for causes not already covered
// by status analysis.
func mineEvents(r *Report, events []Event, covered map[string]bool) {
	for _, e := range events {
		if e.Type != "Warning" {
			continue
		}
		switch {
		case strings.HasPrefix(e.Reason, "Failed") && (strings.Contains(e.Reason, "Mount") || strings.Contains(e.Reason, "AttachVolume")):
			r.Findings = append(r.Findings, Finding{
				Severity: Critical, Problem: "volume mount/attach failed",
				Cause:      e.Message,
				Suggestion: "check the PVC is Bound and the storage backend is healthy",
				Evidence:   "event " + e.Reason + ": " + e.Message,
			})
		case e.Reason == "FailedScheduling" && !covered["scheduling"]:
			r.Findings = append(r.Findings, Finding{
				Severity: Critical, Problem: "pod cannot be scheduled",
				Cause: e.Message, Evidence: "event FailedScheduling: " + e.Message,
			})
			covered["scheduling"] = true
		case e.Reason == "Unhealthy" && !covered["probe"]:
			r.Findings = append(r.Findings, Finding{
				Severity: Warning, Problem: "health probe failing",
				Cause: e.Message, Evidence: "event Unhealthy: " + e.Message,
			})
			covered["probe"] = true
		case e.Reason == "FailedCreatePodSandBox":
			r.Findings = append(r.Findings, Finding{
				Severity: Critical, Problem: "pod sandbox creation failed",
				Cause: e.Message, Evidence: "event FailedCreatePodSandBox: " + e.Message,
			})
		}
	}
}

func terminationCause(t *stateTerminated) string {
	if t == nil {
		return ""
	}
	if t.Reason == "OOMKilled" || t.ExitCode == 137 {
		return "OOMKilled (exit 137) — container exceeded its memory limit"
	}
	if t.Reason != "" {
		return fmt.Sprintf("%s (exit %d)", t.Reason, t.ExitCode)
	}
	return fmt.Sprintf("exit code %d", t.ExitCode)
}

func anyRunning(p pod) bool {
	for _, c := range p.Status.ContainerStatuses {
		if c.State.Running != nil {
			return true
		}
	}
	return false
}

func findEvent(events []Event, reason string) *Event {
	for i := range events {
		if events[i].Reason == reason {
			return &events[i]
		}
	}
	return nil
}

// hasProblem reports whether any finding is a critical or warning (info-only is
// still considered healthy).
func hasProblem(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == Critical || f.Severity == Warning {
			return true
		}
	}
	return false
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		return severityRank[fs[i].Severity] < severityRank[fs[j].Severity]
	})
}
