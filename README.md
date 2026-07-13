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

## Configuration (env)

| Variable | Default | Meaning |
|---|---|---|
| `K8S_MCP_KUBECTL` | `kubectl` | kubectl binary path |
| `K8S_MCP_TIMEOUT_SECONDS` | `30` | per-command timeout |
| `K8S_MCP_MAX_BYTES` | `50000` | output size ceiling |
| `K8S_MCP_ALLOW_SECRETS` | unset | `true`/`1` disables Secret redaction |

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
