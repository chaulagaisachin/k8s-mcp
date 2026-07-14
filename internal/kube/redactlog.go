package kube

import (
	"os"
	"regexp"
)

type logRedactor struct {
	re   *regexp.Regexp
	repl string
}

// logRedactors are high-confidence secret patterns applied to log output.
// Ordered specific-first, generic (blob/hex) last. This is best-effort: it may
// miss novel secrets and may over-redact long tokens/hashes.
var logRedactors = []logRedactor{
	{regexp.MustCompile(`(?s)-----BEGIN[^-]*PRIVATE KEY-----.*?-----END[^-]*PRIVATE KEY-----`), "<redacted-private-key>"},
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}`), "<redacted-jwt>"},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{12,}`), "Bearer <redacted-token>"},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), "<redacted-aws-key>"},
	{regexp.MustCompile(`(?i)([\w.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key))(\s*[=:]\s*)(\S+)`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`), "<redacted-blob>"},
	{regexp.MustCompile(`\b[0-9a-fA-F]{64,}\b`), "<redacted-hex>"},
}

// RedactLog scrubs high-confidence secret shapes from freeform log/evidence
// text. Best-effort; disabled by K8S_MCP_REDACT_LOGS=off.
func RedactLog(s string) string {
	if os.Getenv("K8S_MCP_REDACT_LOGS") == "off" {
		return s
	}
	for _, r := range logRedactors {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
