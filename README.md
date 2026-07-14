# k8s-mcp

A **read-only** Kubernetes observability MCP server, written in Go. It gives an
MCP client (e.g. Claude Code) safe, structured access to a cluster — inspect
resources, read logs, see events, check resource usage — with **no ability to
mutate** anything.

It works by wrapping your local `kubectl`: every tool builds a fixed argument
list (never a shell string), only ever uses read-only verbs, and always targets
a context explicitly without touching your kubeconfig file.

## Requirements

- Go 1.26+ (only to build)
- `kubectl` on `PATH`, with a working kubeconfig

## Build

```sh
make build      # produces ./k8s-mcp
```

## Tools

| Tool | Purpose |
|---|---|
| `list_contexts`, `get_current_context`, `set_context` | Discover and switch the session context (in memory only) |
| `list_namespaces` | List namespaces |
| `list_resources` | List a resource type (`format`: wide/json/yaml/name; namespace, `all_namespaces`, label selector) |
| `describe_resource` | `kubectl describe` for one object |
| `get_resource` | One object's manifest (`format`: yaml/json) |
| `api_resources` | Available kinds, including CRDs |
| `cluster_info` | Version + endpoints (reachability check) |
| `get_logs` | Container logs (`tail`, `previous`, `since`, container) |
| `get_events` | Cluster events, optionally scoped to a namespace or object |
| `top_nodes`, `top_pods` | CPU/memory usage (requires metrics-server) |
| `rollout_status`, `rollout_history` | Workload rollout state (does not wait) |
| `diagnose_pod` | Analyze one pod: crashloop, OOMKilled, image-pull, config, init-container, probe, scheduling, volume failures — with describe+logs evidence |
| `diagnose_deployment` | Rollout/replica health, drilling into unhealthy pods |
| `diagnose_namespace` | Triage a namespace: not-ready/failing pods + warning events (skips completed jobs) |
| `diagnose_node` | Node conditions (Ready/pressure), cordon status, capacity |
| `auth_can_i` | Check whether the current credentials may perform an action (`kubectl auth can-i`); `list=true` lists all permissions |

### Diagnostics

The `diagnose_*` tools are a rule-based analyzer: they fetch the relevant
signals (object status **and** warning events) in one call, detect well-known
failure signatures, and return **structured findings** — `severity`, `problem`,
the offending `container`, probable `cause`, an advisory `suggestion`, and the
`evidence` each finding is grounded in. They stay read-only: suggestions are
text only and are never executed. Every `diagnose_pod` result also attaches raw
describe/log evidence so the model can verify or override a finding.

They detect **Kubernetes-level** failures (scheduling, images, OOM, probes,
volumes), not application-level bugs — those show up in the attached logs.

## Safety

- **Read-only by construction.** Verbs are hardcoded per tool and re-checked
  against an allowlist (`get`, `describe`, `logs`, `top`, `events`,
  `api-resources`, `version`, `cluster-info`, `rollout status/history`,
  read-only `config`). No `apply`/`delete`/`edit`/`scale`/`patch`/`exec`.
- **Secrets redacted by default.** `get_resource`/`list_resources` on Secrets
  blank `.data`/`.stringData` values (keys and byte counts kept). Set
  `K8S_MCP_ALLOW_SECRETS=true` to disable.
- **Output capped** to `K8S_MCP_MAX_BYTES` (default 50000) with a truncation marker.
- `set_context` never writes to `~/.kube/config`.
- **Log-secret redaction (best-effort, default on).** `get_logs` and `diagnose_pod` evidence are scrubbed for high-confidence secret shapes (JWTs, Bearer tokens, AWS keys, PEM private keys, `password=`/`token=` assignments, long base64/hex blobs). Best-effort only — may miss novel secrets or over-redact. Disable with `K8S_MCP_REDACT_LOGS=off`.
- **Audit log.** Every kubectl invocation is logged as a JSON line to stderr (`{ts, verb, args, context, duration_ms, ok, error}`). Set `K8S_MCP_AUDIT_LOG=/path` to also append to a file, or `K8S_MCP_AUDIT=off` to disable.

## Configuration (env)

| Variable | Default | Meaning |
|---|---|---|
| `K8S_MCP_KUBECTL` | `kubectl` | kubectl binary path |
| `K8S_MCP_TIMEOUT_SECONDS` | `30` | per-command timeout |
| `K8S_MCP_MAX_BYTES` | `50000` | output size ceiling |
| `K8S_MCP_ALLOW_SECRETS` | unset | `true`/`1` disables Secret-object redaction |
| `K8S_MCP_REDACT_LOGS` | on | `off` disables best-effort log-secret redaction |
| `K8S_MCP_AUDIT` | on | `off` disables the audit log |
| `K8S_MCP_AUDIT_LOG` | unset | file path to also append audit JSON lines |

## Use with Claude Code

```sh
claude mcp add k8s -- /absolute/path/to/k8s-mcp
```

Or add to your MCP config:

```json
{
  "mcpServers": {
    "k8s": { "command": "/absolute/path/to/k8s-mcp" }
  }
}
```

## Develop

```sh
make test    # unit tests
make lint    # go vet + golangci-lint
make tidy    # go mod tidy
```
