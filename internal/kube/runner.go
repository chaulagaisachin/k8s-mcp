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
	"auth":          true,
}

// allowedRolloutSub restricts `rollout` to its non-mutating subcommands.
var allowedRolloutSub = map[string]bool{"status": true, "history": true}

// allowedConfigSub restricts `config` to read-only subcommands.
var allowedConfigSub = map[string]bool{
	"get-contexts":    true,
	"current-context": true,
	"view":            true,
}

// allowedAuthSub restricts `auth` to the read-only permission query.
var allowedAuthSub = map[string]bool{"can-i": true}

// Runner executes kubectl with a fixed argv (never a shell string).
type Runner struct {
	Bin     string
	Timeout time.Duration
	audit   *auditor
}

// NewRunner builds a Runner from the environment (K8S_MCP_KUBECTL,
// K8S_MCP_TIMEOUT_SECONDS, and the K8S_MCP_AUDIT* variables).
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
	return &Runner{Bin: bin, Timeout: timeout, audit: newAuditor()}
}

// validate enforces the read-only verb/subcommand allowlist.
func validate(args []string) error {
	if len(args) == 0 {
		return errors.New("no kubectl command given")
	}
	verb := args[0]
	if !allowedVerbs[verb] {
		return fmt.Errorf("verb %q is not permitted (this server is read-only)", verb)
	}
	switch verb {
	case "rollout":
		if len(args) < 2 || !allowedRolloutSub[args[1]] {
			return errors.New("only 'rollout status' and 'rollout history' are permitted")
		}
	case "config":
		if len(args) < 2 || !allowedConfigSub[args[1]] {
			return errors.New("only read-only 'config' subcommands are permitted")
		}
	case "auth":
		if len(args) < 2 || !allowedAuthSub[args[1]] {
			return errors.New("only 'auth can-i' is permitted")
		}
	}
	return nil
}

// exec validates, runs kubectl with a per-call timeout, audits the invocation,
// and returns raw stdout/stderr plus the run error (without interpreting it).
func (r *Runner) exec(ctx context.Context, kctx string, args []string) (stdout, stderr string, timedOut bool, err error) {
	if verr := validate(args); verr != nil {
		return "", "", false, verr
	}

	full := append([]string{}, args...)
	if kctx != "" && args[0] != "config" {
		full = append(full, "--context", kctx)
	}

	tctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, r.Bin, full...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)
	timedOut = tctx.Err() == context.DeadlineExceeded

	r.audit.log(auditEntry{
		TS:         time.Now().UTC().Format(time.RFC3339),
		Verb:       args[0],
		Args:       full,
		Context:    kctx,
		DurationMS: dur.Milliseconds(),
		OK:         runErr == nil,
		Error:      errString(runErr),
	})

	return outBuf.String(), errBuf.String(), timedOut, runErr
}

// Run executes a read-only kubectl command. kctx, when non-empty, is injected as
// --context (except for `config`). It returns stdout, or an error carrying
// kubectl's stderr. A non-zero exit is treated as an error.
func (r *Runner) Run(ctx context.Context, kctx string, args ...string) (string, error) {
	stdout, stderr, timedOut, err := r.exec(ctx, kctx, args)
	if err != nil {
		if timedOut {
			return "", fmt.Errorf("kubectl timed out after %s", r.Timeout)
		}
		if msg := strings.TrimSpace(stderr); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return stdout, nil
}

// RunAllowNonZero runs kubectl but does NOT treat a non-zero exit as an error —
// it returns stdout and ok=false instead. This is for commands like
// `auth can-i` that signal their answer via the exit code ("no" => exit 1).
// Only failure-to-start and timeouts return an error.
func (r *Runner) RunAllowNonZero(ctx context.Context, kctx string, args ...string) (out string, ok bool, err error) {
	stdout, stderr, timedOut, exErr := r.exec(ctx, kctx, args)
	if timedOut {
		return "", false, fmt.Errorf("kubectl timed out after %s", r.Timeout)
	}
	if exErr != nil {
		var exit *exec.ExitError
		if errors.As(exErr, &exit) {
			answer := strings.TrimSpace(stdout)
			if answer == "" {
				answer = strings.TrimSpace(stderr)
			}
			return answer, false, nil
		}
		return "", false, exErr // validation or failed-to-start
	}
	return strings.TrimSpace(stdout), true, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
