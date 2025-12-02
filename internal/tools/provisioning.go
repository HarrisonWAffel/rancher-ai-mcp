//go:generate go run ../../cmd/genparams/main.go

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp/internal/tools/response"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	provisioningV1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//go:generate:params
type InspectClusterMachinesParams struct {
	ClusterName string `json:"clusterName" jsonschema:"the name of the provisioning cluster"`
	Namespace   string `json:"namespace" jsonschema:"the namespace of the resource, defaults to fleet-local if not set"`
}

// InspectClusterMachines is a generated function. Implement me.
func (t *Tools) InspectClusterMachines(ctx context.Context, toolReq *mcp.CallToolRequest, params InspectClusterMachinesParams) (*mcp.CallToolResult, any, error) {
	ns := params.Namespace
	if ns == "" {
		ns = "fleet-default"
	}
	zap.L().Info("InspectClusterMachines invoked")

	var resources []*unstructured.Unstructured
	provisioningClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "provisioningcluster",
		Namespace: ns,
		Name:      params.ClusterName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get provisioning cluster",
			zap.String("tool", "inspectProvisioningCluster"),
			zap.String("cluster", params.ClusterName),
			zap.String("namespace", ns),
			zap.Error(err))
		return nil, nil, err
	}
	resources = append(resources, provisioningClusterResource)

	provCluster := provisioningV1.Cluster{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(provisioningClusterResource.Object, &provCluster)
	if err != nil {
		zap.L().Error("failed to convert to provisioning cluster",
			zap.String("tool", "inspectProvisioningCluster"),
			zap.Error(err))
		return nil, nil, err
	}
	zap.L().Info("found provisioning cluster")

	machineSets := map[string]*unstructured.Unstructured{}
	pools := provCluster.Spec.RKEConfig.MachinePools
	for _, pool := range pools {
		poolSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				"cluster.x-k8s.io/cluster-name":       provCluster.Name,
				"rke.cattle.io/rke-machine-pool-name": pool.Name,
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create machine selector for cluster machines")
		}

		// get all the machines from the pool, to show status
		machines, err := t.getResources(ctx, ListParams{
			Cluster:       "local",
			Kind:          "machine",
			Namespace:     "fleet-default",
			URL:           toolReq.Extra.Header.Get(urlHeader),
			Token:         toolReq.Extra.Header.Get(tokenHeader),
			LabelSelector: poolSelector.String(),
		})
		if err != nil {
			zap.L().Error("failed to get machines",
				zap.String("pool", pool.Name),
				zap.String("cluster", provCluster.Name),
				zap.Error(err))
			return nil, nil, err
		}

		if len(machines) > 0 {
			zap.L().Info(fmt.Sprintf("found %d machines", len(machines)),
				zap.String("pool", pool.Name),
				zap.String("cluster", provCluster.Name))
			resources = append(resources, machines...)
			for _, machine := range machines {
				for _, owner := range machine.GetOwnerReferences() {
					if owner.Kind == "MachineSet" {
						machineSet, err := t.getResource(ctx, GetParams{
							Cluster:   "local",
							Kind:      "machineset",
							Namespace: "fleet-default",
							Name:      owner.Name,
							URL:       toolReq.Extra.Header.Get(urlHeader),
							Token:     toolReq.Extra.Header.Get(tokenHeader),
						})
						if err != nil {
							return nil, nil, err
						}
						machineSets[owner.Name] = machineSet
					}
				}
			}
		}
	}

	for _, v := range machineSets {
		resources = append(resources, v)
	}

	mcpResponse, err := response.CreateMcpResponse(resources, "local")
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "inspectProvisioningCluster"), zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

//go:generate:params
type InspectProvisioningClusterParams struct {
	ClusterName string `json:"clusterName" jsonschema:"the name of the provisioning cluster"`
	Namespace   string `json:"namespace" jsonschema:"the namespace of the resource, defaults to fleet-local if not set"`
}

// InspectCluster is a generated function. Implement me.
func (t *Tools) InspectCluster(ctx context.Context, toolReq *mcp.CallToolRequest, params InspectProvisioningClusterParams) (*mcp.CallToolResult, any, error) {
	ns := params.Namespace
	if ns == "" {
		ns = "fleet-default"
	}
	zap.L().Info("InspectCluster invoked")

	var resources []*unstructured.Unstructured
	provisioningClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "provisioningcluster",
		Namespace: ns,
		Name:      params.ClusterName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get provisioning cluster",
			zap.String("tool", "InspectCluster"),
			zap.String("cluster", params.ClusterName),
			zap.String("namespace", ns),
			zap.Error(err))
		return nil, nil, err
	}
	resources = append(resources, provisioningClusterResource)
	zap.L().Info("found provisioning cluster")

	provCluster := provisioningV1.Cluster{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(provisioningClusterResource.Object, &provCluster)
	if err != nil {
		zap.L().Error("failed to convert to provisioning cluster",
			zap.String("tool", "InspectCluster"),
			zap.Error(err))
		return nil, nil, err
	}

	pools := provCluster.Spec.RKEConfig.MachinePools
	for _, pool := range pools {
		poolSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				"cluster.x-k8s.io/cluster-name":       provCluster.Name,
				"rke.cattle.io/rke-machine-pool-name": pool.Name,
			},
		})
		if err != nil {
			zap.L().Error("failed to create label selector for machines",
				zap.String("pool", pool.Name),
				zap.Error(err))
			return nil, nil, err
		}

		zap.L().Info("Created selector",
			zap.String("selector", poolSelector.String()))

		configGVK := pool.NodeConfig.GroupVersionKind()
		config, err := t.getResourceByGVR(ctx, GetParams{
			Cluster:   "local",
			Namespace: "fleet-default",
			Name:      pool.NodeConfig.Name,
			URL:       toolReq.Extra.Header.Get(urlHeader),
			Token:     toolReq.Extra.Header.Get(tokenHeader),
		}, schema.GroupVersionResource{
			Group:   "rke-machine-config.cattle.io",
			Version: "v1",
			// the object reference doesn't explicitly state the resource,
			// but we know that all node driver config ref's will follow the below format.
			Resource: fmt.Sprintf("%ss", strings.ToLower(configGVK.Kind)),
		})
		if err != nil {
			zap.L().Error("failed to get machine config from pool",
				zap.String("machineConfigResource", fmt.Sprintf("%ss", strings.ToLower(configGVK.Kind))),
				zap.String("configName", pool.NodeConfig.Name),
				zap.String("poolName", pool.Name),
				zap.String("poolKind", pool.NodeConfig.Kind),
				zap.String("tool", "InspectCluster"),
				zap.Error(err))
			return nil, nil, err
		}
		resources = append(resources, config)
		zap.L().Info("found machine config")
		zap.L().Info("Getting machines for pool")
		// get all the machines from the pool, to show their status
		machines, err := t.getResources(ctx, ListParams{
			Cluster:       "local",
			Kind:          "machine",
			Namespace:     "fleet-default",
			URL:           toolReq.Extra.Header.Get(urlHeader),
			Token:         toolReq.Extra.Header.Get(tokenHeader),
			LabelSelector: poolSelector.String(),
		})
		if err != nil {
			zap.L().Error("failed to list machines",
				zap.String("pool", pool.Name),
				zap.Error(err))
			return nil, nil, err
		}
		resources = append(resources, machines...)
	}

	// get the provisioning log
	provisioningLogNamespace := provCluster.Status.ClusterName
	log, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "configmap",
		Namespace: provisioningLogNamespace,
		Name:      "provisioning-log",
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	resources = append(resources, log)

	// get recent cluster events
	clientset, err := t.client.CreateClientSet(toolReq.Extra.Header.Get(tokenHeader), toolReq.Extra.Header.Get(urlHeader), "local")
	if err != nil {
		zap.L().Error("failed to create clientset", zap.String("tool", "getClusterImages"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	standardEventNamespaces := []string{
		"cattle-system",
		"kube-system",
		"default",
	}

	for _, ns := range standardEventNamespaces {
		events, err := clientset.EventsV1().Events(ns).List(ctx, metav1.ListOptions{
			Limit: 15,
		})
		if err != nil {
			zap.L().Error("failed to get events for namespace",
				zap.String("namespace", ns),
				zap.Error(err))
			return nil, nil, err
		}
		for _, event := range events.Items {
			unstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(event)
			if err != nil {
				zap.L().Error("failed to convert event to unstructured object",
					zap.String("namespace", ns),
					zap.String("event", event.Name),
					zap.Error(err))
				return nil, nil, err
			}
			resources = append(resources, &unstructured.Unstructured{Object: unstruct})
		}
	}

	mcpResponse, err := response.CreateMcpResponse(resources, "local")
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "inspectProvisioningCluster"), zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

// GetNodesParams specifies the parameters needed to retrieve node metrics.
type GetNodesParams struct {
	Cluster string `json:"cluster" jsonschema:"the cluster of the resource"`
}

// GetNodes retrieves information and metrics for all nodes in a given cluster.
func (t *Tools) GetNodes(ctx context.Context, toolReq *mcp.CallToolRequest, params GetNodesParams) (*mcp.CallToolResult, any, error) {
	zap.L().Info("getNodes called")

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

type GetClusterImagesParams struct {
	Clusters []string `json:"clusters" jsonschema:"the clusters where images are returned"`
}

func (t *Tools) GetClusterImages(ctx context.Context, toolReq *mcp.CallToolRequest, params GetClusterImagesParams) (*mcp.CallToolResult, any, error) {
	zap.L().Info("getClusterImages called")

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
