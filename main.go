// Command k8s-mcp is a read-only Kubernetes observability MCP server. It exposes
// kubectl-backed inspection tools over stdio and never mutates a cluster.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"k8s-mcp/internal/kube"
	"k8s-mcp/internal/tools"
)

const version = "0.1.0"

func main() {
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
