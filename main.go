// Command k8s-mcp is an AI-accessible Kubernetes diagnostic/observability MCP
// server. It exposes kubectl-backed inspection and diagnosis tools over stdio so
// an assistant can triage a cluster in natural language — without being handed
// raw cluster credentials. It is read-only by default; a small set of write
// operations exists but is disabled unless K8S_MCP_ENABLE_WRITES is set.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
	"k8s-mcp/internal/tools"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

const usage = `k8s-mcp %s — AI-accessible Kubernetes diagnostic/observability MCP server.

Runs as an MCP server over stdio; launch it from an MCP host (e.g. Claude Code),
not directly. It shells out to kubectl, so kubectl and a kubeconfig must be
available. Read-only by default; set K8S_MCP_ENABLE_WRITES=1 to enable the
gated write operations.

Usage:
  k8s-mcp            run the MCP server over stdio
  k8s-mcp --version  print version and exit
  k8s-mcp --help     print this help and exit

See the README for the full tool list and environment variables.
`

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-v":
			fmt.Println(version)
			return
		case "--help", "-h":
			fmt.Printf(usage, version)
			return
		}
	}

	deps := &tools.Deps{
		Runner: kube.NewRunner(),
		Ctx:    kube.NewContextStore(),
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "k8s-mcp", Version: version}, nil)
	tools.RegisterAll(server, deps)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
