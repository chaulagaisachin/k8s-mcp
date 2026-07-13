package tools

import (
	"slices"
	"testing"
)

func TestNormalizeFormat(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		def     string
		allowed []string
		want    string
		wantErr bool
	}{
		{"default when empty", "", "wide", []string{"wide", "json"}, "wide", false},
		{"allowed value", "json", "wide", []string{"wide", "json"}, "json", false},
		{"case-insensitive", "JSON", "wide", []string{"wide", "json"}, "json", false},
		{"rejected value", "xml", "wide", []string{"wide", "json"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeFormat(tt.in, tt.def, tt.allowed...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestAppendScope(t *testing.T) {
	got, err := appendScope([]string{"get", "pods"}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"get", "pods", "-A"}) {
		t.Fatalf("all-namespaces: got %v", got)
	}

	got, err = appendScope([]string{"get", "pods"}, "kube-system", false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"get", "pods", "-n", "kube-system"}) {
		t.Fatalf("namespaced: got %v", got)
	}

	if _, err := appendScope([]string{"get", "pods"}, "--bad", false); err == nil {
		t.Fatal("expected flag-injection namespace to be rejected")
	}
}

func TestRolloutTarget(t *testing.T) {
	got, err := rolloutTarget("", "web")
	if err != nil || got != "deployment/web" {
		t.Fatalf("default type: got %q err %v", got, err)
	}
	if _, err := rolloutTarget("statefulset", "db"); err != nil {
		t.Fatalf("statefulset should be allowed: %v", err)
	}
	if _, err := rolloutTarget("service", "web"); err == nil {
		t.Fatal("service does not support rollouts, expected error")
	}
	if _, err := rolloutTarget("deployment", "--bad"); err == nil {
		t.Fatal("expected flag-injection name to be rejected")
	}
}

func TestIsSecretType(t *testing.T) {
	for _, s := range []string{"secret", "secrets", "Secret", "SECRETS"} {
		if !isSecretType(s) {
			t.Errorf("expected %q to be a secret type", s)
		}
	}
	for _, s := range []string{"configmap", "pod", "secretproviderclass"} {
		if isSecretType(s) {
			t.Errorf("expected %q NOT to be a secret type", s)
		}
	}
}
