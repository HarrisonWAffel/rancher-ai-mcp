package toolsets

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rancher/rancher-ai-mcp/pkg/client"
	"github.com/rancher/rancher-ai-mcp/pkg/toolsets/core"
	"github.com/rancher/rancher-ai-mcp/pkg/toolsets/fleet"
	"github.com/rancher/rancher-ai-mcp/pkg/toolsets/provisioning"
	"github.com/spf13/pflag"
)

// toolsAdder is an interface for types that can add tools to an MCP server.
type toolsAdder interface {
	AddTools(mcpServer *mcp.Server)
}

// AddAllTools adds all available tools to the MCP server.
func AddAllTools(client *client.Client, mcpServer *mcp.Server, flags *pflag.FlagSet, readOnly bool) {
	for _, ta := range allToolSets(client, flags, readOnly) {
		ta.AddTools(mcpServer)
	}
}

func allToolSets(client *client.Client, flags *pflag.FlagSet, readOnly bool) []toolsAdder {
	return []toolsAdder{
		core.NewTools(client, readOnly),
		fleet.NewTools(client),
		provisioning.NewTools(client, flags, readOnly),
	}
}
