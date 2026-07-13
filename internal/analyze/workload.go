package analyze

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type workload struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		Replicas *int `json:"replicas"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
	} `json:"spec"`
	Status struct {
		// Deployment / StatefulSet
		Replicas          int         `json:"replicas"`
		ReadyReplicas     int         `json:"readyReplicas"`
		AvailableReplicas int         `json:"availableReplicas"`
		UpdatedReplicas   int         `json:"updatedReplicas"`
		Conditions        []condition `json:"conditions"`
		// DaemonSet
		DesiredNumberScheduled int `json:"desiredNumberScheduled"`
		NumberReady            int `json:"numberReady"`
		NumberAvailable        int `json:"numberAvailable"`
		UpdatedNumberScheduled int `json:"updatedNumberScheduled"`
		NumberMisscheduled     int `json:"numberMisscheduled"`
	} `json:"status"`
}

// isDaemonSet reports whether the kind (and its aliases) is a DaemonSet.
func isDaemonSet(kind string) bool {
	switch strings.ToLower(kind) {
	case "daemonset", "daemonsets", "ds":
		return true
	}
	return false
}

// Workload analyzes a deployment/statefulset/daemonset. It is kind-aware because
// DaemonSets expose entirely different status fields (numberReady/desiredNumberScheduled)
// and have no spec.replicas. It returns the report and the pod label selector.
func Workload(raw []byte, kind string) (Report, string, error) {
	var w workload
	if err := json.Unmarshal(raw, &w); err != nil {
		return Report{}, "", err
	}
	r := Report{Name: w.Metadata.Name, Signals: map[string]string{}}

	var desired, ready, available, updated int
	if isDaemonSet(kind) {
		desired = w.Status.DesiredNumberScheduled
		ready = w.Status.NumberReady
		available = w.Status.NumberAvailable
		updated = w.Status.UpdatedNumberScheduled
		if w.Status.NumberMisscheduled > 0 {
			r.Findings = append(r.Findings, Finding{
				Severity: Warning,
				Problem:  fmt.Sprintf("%d pods are misscheduled", w.Status.NumberMisscheduled),
				Evidence: fmt.Sprintf("status.numberMisscheduled=%d", w.Status.NumberMisscheduled),
			})
		}
	} else {
		desired = 1
		if w.Spec.Replicas != nil {
			desired = *w.Spec.Replicas
		}
		ready = w.Status.ReadyReplicas
		available = w.Status.AvailableReplicas
		updated = w.Status.UpdatedReplicas
	}

	r.Signals["desired"] = fmt.Sprintf("%d", desired)
	r.Signals["available"] = fmt.Sprintf("%d", available)
	r.Signals["ready"] = fmt.Sprintf("%d", ready)
	r.Signals["updated"] = fmt.Sprintf("%d", updated)

	// Conditions exist only on Deployments; the loop is a no-op for STS/DS.
	for _, c := range w.Status.Conditions {
		if c.Type == "Progressing" && c.Reason == "ProgressDeadlineExceeded" {
			r.Findings = append(r.Findings, Finding{
				Severity: Critical, Problem: "rollout stalled (ProgressDeadlineExceeded)",
				Cause:      c.Message,
				Suggestion: "diagnose the workload's pods for the underlying failure",
				Evidence:   "condition Progressing reason=ProgressDeadlineExceeded",
			})
		}
		if c.Type == "Available" && c.Status == "False" {
			r.Findings = append(r.Findings, Finding{
				Severity: Warning, Problem: "workload is not Available",
				Cause: c.Message, Evidence: "condition Available status=False",
			})
		}
	}

	if available < desired {
		unit := "replicas"
		if isDaemonSet(kind) {
			unit = "pods scheduled"
		}
		r.Findings = append(r.Findings, Finding{
			Severity: Warning,
			Problem:  fmt.Sprintf("only %d/%d %s available", available, desired, unit),
			Evidence: fmt.Sprintf("available=%d desired=%d", available, desired),
		})
	}

	sortFindings(r.Findings)
	r.Healthy = !hasProblem(r.Findings)
	return r, selectorString(w.Spec.Selector.MatchLabels), nil
}

func selectorString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

type nodeObj struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Conditions  []condition       `json:"conditions"`
		Capacity    map[string]string `json:"capacity"`
		Allocatable map[string]string `json:"allocatable"`
	} `json:"status"`
}

// Node analyzes a node's conditions and schedulability.
func Node(raw []byte) (Report, error) {
	var n nodeObj
	if err := json.Unmarshal(raw, &n); err != nil {
		return Report{}, err
	}
	r := Report{Name: n.Metadata.Name, Signals: map[string]string{}}
	r.Signals["cpu_capacity"] = n.Status.Capacity["cpu"]
	r.Signals["memory_capacity"] = n.Status.Capacity["memory"]
	r.Signals["unschedulable"] = fmt.Sprintf("%t", n.Spec.Unschedulable)

	for _, c := range n.Status.Conditions {
		switch c.Type {
		case "Ready":
			r.Signals["ready"] = c.Status
			if c.Status != "True" {
				r.Findings = append(r.Findings, Finding{
					Severity: Critical, Problem: "node is not Ready",
					Cause: c.Message, Evidence: "condition Ready status=" + c.Status,
				})
			}
		case "MemoryPressure", "DiskPressure", "PIDPressure":
			if c.Status == "True" {
				r.Findings = append(r.Findings, Finding{
					Severity: Warning, Problem: "node under " + c.Type,
					Cause:      c.Message,
					Suggestion: "free resources or scale the cluster; pods here may be evicted",
					Evidence:   "condition " + c.Type + " status=True",
				})
			}
		}
	}

	if n.Spec.Unschedulable {
		r.Findings = append(r.Findings, Finding{
			Severity: Warning, Problem: "node is cordoned (unschedulable)",
			Evidence: "spec.unschedulable=true",
		})
	}

	sortFindings(r.Findings)
	r.Healthy = !hasProblem(r.Findings)
	return r, nil
}
