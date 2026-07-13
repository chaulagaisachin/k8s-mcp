// Package kube wraps read-only kubectl invocations for the MCP server.
package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// allowedVerbs is the read-only kubectl verb allowlist (defense-in-depth; each
// tool already hardcodes its verb).
var allowedVerbs = map[string]bool{
	"get":           true,
	"describe":      true,
	"logs":          true,
	"top":           true,
	"api-resources": true,
	"version":       true,
	"cluster-info":  true,
	"rollout":       true,
	"config":        true,
}

// allowedRolloutSub restricts `rollout` to its non-mutating subcommands.
var allowedRolloutSub = map[string]bool{"status": true, "history": true}

// allowedConfigSub restricts `config` to read-only subcommands.
var allowedConfigSub = map[string]bool{
	"get-contexts":    true,
	"current-context": true,
	"view":            true,
}

// Runner executes kubectl with a fixed argv (never a shell string).
type Runner struct {
	Bin     string
	Timeout time.Duration
}

// NewRunner builds a Runner from the environment (K8S_MCP_KUBECTL,
// K8S_MCP_TIMEOUT_SECONDS).
func NewRunner() *Runner {
	bin := os.Getenv("K8S_MCP_KUBECTL")
	if bin == "" {
		bin = "kubectl"
	}
	timeout := defaultTimeout
	if v := os.Getenv("K8S_MCP_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	return &Runner{Bin: bin, Timeout: timeout}
}

// Run executes a read-only kubectl command. kctx, when non-empty, is injected as
// --context (except for `config`, which reads the kubeconfig directly). It
// returns kubectl's stdout, or an error carrying kubectl's stderr.
func (r *Runner) Run(ctx context.Context, kctx string, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("no kubectl command given")
	}
	verb := args[0]
	if !allowedVerbs[verb] {
		return "", fmt.Errorf("verb %q is not permitted (this server is read-only)", verb)
	}
	if verb == "rollout" {
		if len(args) < 2 || !allowedRolloutSub[args[1]] {
			return "", errors.New("only 'rollout status' and 'rollout history' are permitted")
		}
	}
	if verb == "config" {
		if len(args) < 2 || !allowedConfigSub[args[1]] {
			return "", errors.New("only read-only 'config' subcommands are permitted")
		}
	}

	full := append([]string{}, args...)
	if kctx != "" && verb != "config" {
		full = append(full, "--context", kctx)
	}

	tctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, r.Bin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if tctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("kubectl timed out after %s", r.Timeout)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

// SafeArg validates a user-supplied argv value (resource type, name, namespace,
// context). It rejects empty values, leading dashes (flag injection) and
// whitespace.
func SafeArg(kind, v string) error {
	if v == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("%s %q must not start with '-'", kind, v)
	}
	if strings.ContainsAny(v, " \t\n\r") {
		return fmt.Errorf("%s %q must not contain whitespace", kind, v)
	}
	return nil
}
