package kube

import "testing"

func TestSafeArg(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"plain", "nginx", false},
		{"dotted", "kube-system", false},
		{"empty", "", true},
		{"leading dash flag injection", "--all", true},
		{"single dash", "-A", true},
		{"whitespace", "foo bar", true},
		{"newline", "foo\nbar", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SafeArg("arg", tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SafeArg(%q) err=%v, wantErr=%v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestAllowedVerbs(t *testing.T) {
	for _, v := range []string{"get", "describe", "logs", "top", "api-resources", "version", "cluster-info", "rollout", "config"} {
		if !allowedVerbs[v] {
			t.Errorf("expected %q to be allowed", v)
		}
	}
	for _, v := range []string{"apply", "delete", "edit", "scale", "patch", "exec", "port-forward"} {
		if allowedVerbs[v] {
			t.Errorf("expected %q to be forbidden", v)
		}
	}
}
