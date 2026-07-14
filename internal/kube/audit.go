package kube

import (
	"encoding/json"
	"io"
	"os"
	"sync"
)

// auditEntry is one JSON audit record per kubectl invocation.
type auditEntry struct {
	TS         string   `json:"ts"`
	Verb       string   `json:"verb"`
	Args       []string `json:"args"`
	Context    string   `json:"context,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	OK         bool     `json:"ok"`
	Error      string   `json:"error,omitempty"`
}

// auditor writes audit entries as JSON lines. A nil writer disables auditing.
type auditor struct {
	mu sync.Mutex
	w  io.Writer
}

// newAuditor configures auditing from the environment. It logs to stderr by
// default (never stdout — that is the MCP transport), additionally appends to
// K8S_MCP_AUDIT_LOG if set, and is disabled entirely by K8S_MCP_AUDIT=off.
func newAuditor() *auditor {
	if os.Getenv("K8S_MCP_AUDIT") == "off" {
		return &auditor{}
	}
	writers := []io.Writer{os.Stderr}
	if path := os.Getenv("K8S_MCP_AUDIT_LOG"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			writers = append(writers, f)
		}
	}
	return &auditor{w: io.MultiWriter(writers...)}
}

// log writes one audit line. It is a no-op when auditing is disabled.
func (a *auditor) log(e auditEntry) {
	if a == nil || a.w == nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _ = a.w.Write(append(b, '\n'))
}
