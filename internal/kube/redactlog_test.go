package kube

import (
	"strings"
	"testing"
)

func TestRedactLog(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		mustGone   string // substring that must NOT survive
		mustRemain string // substring that must survive
	}{
		{"jwt", "token=eyJhbGciOiJI.eyJzdWIiOiIxMjM0.SflKxwRJSMeKKF2QT4", "SflKxwRJSMeKKF2QT4", "token="},
		{"bearer", "Authorization: Bearer abcdef0123456789ABCDEF", "abcdef0123456789ABCDEF", "Authorization"},
		{"aws key", "key AKIAIOSFODNN7EXAMPLE here", "AKIAIOSFODNN7EXAMPLE", "here"},
		{"password assignment", `db_password=hunter2secret`, "hunter2secret", "db_password"},
		{"private key", "x\n-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----\ny", "MIIabc", "x"},
		{"base64 blob", "data " + strings.Repeat("QUJD", 12), strings.Repeat("QUJD", 12), "data"},
		{"normal line untouched", "starting server on port 8080", "", "starting server on port 8080"},
		{"short value kept", "id=42", "", "id=42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactLog(tt.in)
			if tt.mustGone != "" && strings.Contains(got, tt.mustGone) {
				t.Fatalf("secret survived: %q -> %q", tt.in, got)
			}
			if tt.mustRemain != "" && !strings.Contains(got, tt.mustRemain) {
				t.Fatalf("expected %q to remain in %q", tt.mustRemain, got)
			}
		})
	}
}

func TestRedactLog_Disabled(t *testing.T) {
	t.Setenv("K8S_MCP_REDACT_LOGS", "off")
	in := "db_password=hunter2secret"
	if RedactLog(in) != in {
		t.Fatal("expected passthrough when disabled")
	}
}
