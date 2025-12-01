//go:generate go run ../../cmd/genparams/main.go

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp/internal/tools/response"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetNodesParams specifies the parameters needed to retrieve node metrics.
type GetNodesParams struct {
	Cluster string `json:"cluster" jsonschema:"the cluster of the resource"`
}

// GetNodes retrieves information and metrics for all nodes in a given cluster.
func (t *Tools) GetNodes(ctx context.Context, toolReq *mcp.CallToolRequest, params GetNodesParams) (*mcp.CallToolResult, any, error) {
	zap.L().Debug("getNodes called")

	nodeResource, err := t.getResources(ctx, ListParams{
		Cluster: params.Cluster,
		Kind:    "node",
		URL:     toolReq.Extra.Header.Get(urlHeader),
		Token:   toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get nodes", zap.String("tool", "getNodes"), zap.Error(err))
		return nil, nil, err
	}

	// ignore error as Metrics Server might not be installed in the cluster
	nodeMetricsResource, _ := t.getResources(ctx, ListParams{
		Cluster: params.Cluster,
		Kind:    "node.metrics.k8s.io",
		URL:     toolReq.Extra.Header.Get(urlHeader),
		Token:   toolReq.Extra.Header.Get(tokenHeader),
	})

	mcpResponse, err := response.CreateMcpResponse(append(nodeResource, nodeMetricsResource...), params.Cluster)
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "getNodes"), zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

//go:generate:params
type GetProvisioningClusterParams struct {
	Name string `json:"name" jsonschema:"the name of the provisioning cluster"`
}

// GetProvisioningCluster returns a v1 provisioning cluster by name.
func (t *Tools) GetProvisioningCluster(ctx context.Context, toolReq *mcp.CallToolRequest, params GetProvisioningClusterParams) (*mcp.CallToolResult, any, error) {
	clusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "clusters.provisioning.cattle.io",
		Namespace: "fleet-local",
		Name:      params.Name,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		return nil, nil, err
	}

	mcpResponse, err := response.CreateMcpResponse([]*unstructured.Unstructured{clusterResource}, params.Name)
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "getNodes"), zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

type GetClusterImagesParams struct {
	Clusters []string `json:"clusters" jsonschema:"the clusters where images are returned"`
}

func (t *Tools) GetClusterImages(ctx context.Context, toolReq *mcp.CallToolRequest, params GetClusterImagesParams) (*mcp.CallToolResult, any, error) {
	zap.L().Debug("getClusterImages called")

	var clusters []string
	if len(params.Clusters) == 0 {
		clusterList, err := t.getResources(ctx, ListParams{
			Cluster: "local",
			Kind:    "cluster",
			URL:     toolReq.Extra.Header.Get(urlHeader),
			Token:   toolReq.Extra.Header.Get(tokenHeader),
		})
		if err != nil {
			zap.L().Error("failed to get clusters", zap.String("tool", "getClusterImages"), zap.Error(err))
			return nil, nil, fmt.Errorf("failed to get clusters: %w", err)
		}
		for _, cluster := range clusterList {
			clusters = append(clusters, cluster.GetName())
		}
	} else {
		clusters = params.Clusters
	}

	imagesInClusters := map[string][]string{}

	for _, cluster := range clusters {
		var images []string
		clientset, err := t.client.CreateClientSet(toolReq.Extra.Header.Get(tokenHeader), toolReq.Extra.Header.Get(urlHeader), cluster)
		if err != nil {
			zap.L().Error("failed to create clientset", zap.String("tool", "getClusterImages"), zap.Error(err))
			return nil, nil, fmt.Errorf("failed to create clientset: %w", err)
		}
		pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
		if err != nil {
			zap.L().Error("failed to get pods", zap.String("tool", "getClusterImages"), zap.Error(err))
			return nil, nil, fmt.Errorf("failed to get pods: %w", err)
		}
		for _, pod := range pods.Items {
			for _, container := range pod.Spec.InitContainers {
				images = append(images, container.Image)
			}
			for _, container := range pod.Spec.Containers {
				images = append(images, container.Image)
			}
		}

		imagesInClusters[cluster] = images
	}

	response, err := json.Marshal(imagesInClusters)
	if err != nil {
		zap.L().Error("failed to create response", zap.String("tool", "getClusterImages"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to marsha JSON: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(response)}},
	}, nil, nil

}
