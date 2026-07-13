package kube

import (
	"strings"
	"testing"
)

func TestCap(t *testing.T) {
	t.Run("under limit is unchanged", func(t *testing.T) {
		t.Setenv("K8S_MCP_MAX_BYTES", "100")
		in := "short output"
		out, truncated := Cap(in)
		if truncated {
			t.Fatal("expected not truncated")
		}
		if out != in {
			t.Fatalf("expected unchanged, got %q", out)
		}
	})

	t.Run("over limit truncates with marker and keeps head+tail", func(t *testing.T) {
		t.Setenv("K8S_MCP_MAX_BYTES", "100")
		in := "HEAD" + strings.Repeat("x", 500) + "TAIL"
		out, truncated := Cap(in)
		if !truncated {
			t.Fatal("expected truncated")
		}
		if !strings.Contains(out, "bytes omitted") {
			t.Fatalf("expected omitted marker, got %q", out)
		}
		if !strings.HasPrefix(out, "HEAD") {
			t.Fatalf("expected head preserved, got %q", out[:10])
		}
		if !strings.HasSuffix(out, "TAIL") {
			t.Fatalf("expected tail preserved, got %q", out[len(out)-10:])
		}
	})
}

func TestRedactSecretJSON(t *testing.T) {
	t.Setenv("K8S_MCP_ALLOW_SECRETS", "")
	in := `{"kind":"Secret","metadata":{"name":"db"},"data":{"password":"c3VwZXItc2VjcmV0"}}`
	out := RedactSecret("json", in)
	if strings.Contains(out, "c3VwZXItc2VjcmV0") {
		t.Fatalf("secret value leaked: %q", out)
	}
	if !strings.Contains(out, "password") {
		t.Fatalf("expected key preserved, got %q", out)
	}
	if !strings.Contains(out, "<redacted:") {
		t.Fatalf("expected redaction marker, got %q", out)
	}
}

func TestRedactSecretYAML(t *testing.T) {
	t.Setenv("K8S_MCP_ALLOW_SECRETS", "")
	in := "kind: Secret\ndata:\n  token: c2VjcmV0\n"
	out := RedactSecret("yaml", in)
	if strings.Contains(out, "c2VjcmV0") {
		t.Fatalf("secret value leaked: %q", out)
	}
	if !strings.Contains(out, "token") || !strings.Contains(out, "<redacted:") {
		t.Fatalf("expected key preserved and redaction, got %q", out)
	}
}

func TestRedactSecretAllowed(t *testing.T) {
	t.Setenv("K8S_MCP_ALLOW_SECRETS", "true")
	in := `{"data":{"password":"c3VwZXItc2VjcmV0"}}`
	out := RedactSecret("json", in)
	if !strings.Contains(out, "c3VwZXItc2VjcmV0") {
		t.Fatalf("expected raw value when allowed, got %q", out)
	}
}

func TestRedactSecretList(t *testing.T) {
	t.Setenv("K8S_MCP_ALLOW_SECRETS", "")
	in := `{"kind":"SecretList","items":[{"data":{"a":"eA=="}},{"data":{"b":"eQ=="}}]}`
	out := RedactSecret("json", in)
	if strings.Contains(out, "eA==") || strings.Contains(out, "eQ==") {
		t.Fatalf("secret value leaked in list: %q", out)
	}
}
