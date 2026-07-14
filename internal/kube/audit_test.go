package kube

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditor_Log(t *testing.T) {
	var buf bytes.Buffer
	a := &auditor{w: &buf}
	a.log(auditEntry{TS: "2026-07-14T00:00:00Z", Verb: "get", Args: []string{"get", "pods"}, Context: "kind-x", DurationMS: 12, OK: true})

	line := strings.TrimSpace(buf.String())
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatal("audit entry must end with a newline")
	}
	var got auditEntry
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("audit line is not valid JSON: %v (%q)", err, line)
	}
	if got.Verb != "get" || !got.OK || got.Context != "kind-x" || got.DurationMS != 12 {
		t.Fatalf("unexpected entry: %+v", got)
	}
}

func TestAuditor_DisabledIsNoop(t *testing.T) {
	a := &auditor{} // nil writer
	a.log(auditEntry{Verb: "get"})
	// nil-receiver safety
	var nilA *auditor
	nilA.log(auditEntry{Verb: "get"})
}
