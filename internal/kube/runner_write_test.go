package kube

import (
	"context"
	"strings"
	"testing"
)

func TestValidateWrite(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"scale ok", []string{"scale", "deploy/web", "--replicas=3"}, false},
		{"delete pod ok", []string{"delete", "pod", "x"}, false},
		{"rollout restart ok", []string{"rollout", "restart", "deployment/web"}, false},
		{"rollout undo ok", []string{"rollout", "undo", "deployment/web"}, false},
		{"cordon ok", []string{"cordon", "node-1"}, false},
		{"uncordon ok", []string{"uncordon", "node-1"}, false},
		{"delete deployment rejected", []string{"delete", "deployment", "web"}, true},
		{"delete namespace rejected", []string{"delete", "namespace", "prod"}, true},
		{"rollout status not a write", []string{"rollout", "status", "deployment/web"}, true},
		{"apply rejected", []string{"apply", "-f", "x.yaml"}, true},
		{"get rejected on write path", []string{"get", "pods"}, true},
		{"empty rejected", []string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateWrite(tt.args); (err != nil) != tt.wantErr {
				t.Fatalf("validateWrite(%v) err=%v wantErr=%v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestRunWrite_DisabledByDefault(t *testing.T) {
	r := &Runner{Bin: "kubectl", Timeout: defaultTimeout, audit: &auditor{}} // writesEnabled defaults false
	_, err := r.RunWrite(context.Background(), "", "scale", "deploy/web", "--replicas=1")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected writes-disabled error, got %v", err)
	}
}

func TestNewRunner_WritesFromEnv(t *testing.T) {
	t.Setenv("K8S_MCP_ENABLE_WRITES", "1")
	if !NewRunner().WritesEnabled() {
		t.Fatal("expected writes enabled when K8S_MCP_ENABLE_WRITES=1")
	}
	t.Setenv("K8S_MCP_ENABLE_WRITES", "")
	if NewRunner().WritesEnabled() {
		t.Fatal("expected writes disabled by default")
	}
}
