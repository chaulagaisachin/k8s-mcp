package kube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultCapBytes = 50_000

// capBytes returns the output ceiling (K8S_MCP_MAX_BYTES, default 50000).
func capBytes() int {
	if v := os.Getenv("K8S_MCP_MAX_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultCapBytes
}

// Cap truncates s to the byte ceiling, keeping head and tail with a marker in
// between. It reports whether truncation occurred.
func Cap(s string) (string, bool) {
	max := capBytes()
	if len(s) <= max {
		return s, false
	}
	head := max * 2 / 3
	tail := max - head
	omitted := len(s) - head - tail
	marker := fmt.Sprintf(
		"\n\n... %d bytes omitted (output capped at %d bytes; narrow the query, use --tail, or raise K8S_MCP_MAX_BYTES) ...\n\n",
		omitted, max,
	)
	return s[:head] + marker + s[len(s)-tail:], true
}

// secretsAllowed reports whether secret redaction is disabled via env.
func secretsAllowed() bool {
	v := os.Getenv("K8S_MCP_ALLOW_SECRETS")
	return v == "true" || v == "1"
}

// RedactSecret blanks the .data/.stringData values of Secret output (json/yaml),
// keeping keys and reporting byte counts. It is a no-op if K8S_MCP_ALLOW_SECRETS
// is set. On parse failure it returns a safe placeholder rather than the raw
// (possibly sensitive) output.
func RedactSecret(format, out string) string {
	if secretsAllowed() {
		return out
	}
	var v any
	switch format {
	case "json":
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			return "<redaction failed to parse secret output; withheld>"
		}
		redactWalk(v)
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return "<redaction failed to re-encode secret output; withheld>"
		}
		return strings.TrimRight(buf.String(), "\n")
	case "yaml":
		if err := yaml.Unmarshal([]byte(out), &v); err != nil {
			return "<redaction failed to parse secret output; withheld>"
		}
		redactWalk(v)
		b, err := yaml.Marshal(v)
		if err != nil {
			return "<redaction failed to re-encode secret output; withheld>"
		}
		return string(b)
	default:
		return out
	}
}

// redactWalk recursively replaces values under any "data"/"stringData" map with a
// redaction marker. Handles both single Secrets and List output.
func redactWalk(v any) {
	switch node := v.(type) {
	case map[string]any:
		for k, val := range node {
			if (k == "data" || k == "stringData") && val != nil {
				if m, ok := val.(map[string]any); ok {
					for dk, dv := range m {
						m[dk] = fmt.Sprintf("<redacted: %d bytes>", len(fmt.Sprint(dv)))
					}
					continue
				}
			}
			redactWalk(val)
		}
	case []any:
		for _, item := range node {
			redactWalk(item)
		}
	}
}
