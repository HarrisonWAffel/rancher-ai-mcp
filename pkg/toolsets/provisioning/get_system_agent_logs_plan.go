package provisioning

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rancher/rancher-ai-mcp/pkg/response"
	nodeUtils "github.com/rancher/rancher-ai-mcp/pkg/toolsets/provisioning/utils"
	"github.com/rancher/rancher-ai-mcp/pkg/utils"
	"go.uber.org/zap"
)

func (t *Tools) investigateFailedPlanApplicationPlan(_ context.Context, toolReq *mcp.CallToolRequest, params investigateFailedPlanApplicationParams) (*mcp.CallToolResult, any, error) {
	log := utils.NewChildLogger(toolReq, map[string]string{
		"clusterName": params.Cluster,
		"nodeName":    params.NodeName,
	})

	log.Debug("Planning failed plan execution investigation")
	toolboxImage, err := t.flags.GetString("toolboxImage-image")
	if err != nil {
		log.Error("failed to get toolboxImage image", zap.Error(err))
		return nil, nil, err
	}

	job, err := nodeUtils.CreateSystemdLogGathererJob(fmt.Sprintf("systemd-log-gatherer-%s", params.NodeName), params.NodeName, "rancher-system-agent", toolboxImage, log)
	if err != nil {
		log.Error("failed to create systemd job", zap.Error(err))
		return nil, nil, err
	}

	createResource := response.NewCreateResourceInput(job, params.Cluster)
	mcpResponse, err := response.CreatePlanResponse([]response.PlanResource{createResource})
	if err != nil {
		log.Error("failed to create plan response", zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}
