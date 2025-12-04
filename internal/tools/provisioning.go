//go:generate go run ../../cmd/genparams/main.go

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp/internal/tools/response"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	provisioningV1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//go:generate:params
type GetClusterMachineParams struct {
	ClusterName string `json:"clusterName" jsonschema:"the name of the cluster the machines belong to"`
	MachineName string `json:"machineName" jsonschema:"the name of the machine to retrieve, if not set all machines for the cluster are returned"`
}

// GetClusterMachine returns the cluster API machine for a given provisioning cluster and machine name.
func (t *Tools) GetClusterMachine(ctx context.Context, toolReq *mcp.CallToolRequest, params GetClusterMachineParams) (*mcp.CallToolResult, any, error) {
	zap.L().Info("GetClusterMachine called",
		zap.String("clusterName", params.ClusterName),
		zap.String("machineName", params.MachineName))

	machines, _, _, err := t.getCAPIMachineResources(ctx, toolReq, getCAPIMachineResourcesParams{
		namespace:     "fleet-default",
		targetCluster: params.ClusterName,
		machineName:   params.MachineName,
	})

	if err != nil {
		zap.L().Error("failed to lookup capi machine",
			zap.String("tool", "GetClusterMachine"),
			zap.String("machine", params.MachineName),
			zap.Error(err))
		return nil, nil, err
	}

	var resources []*unstructured.Unstructured
	for _, machine := range machines {
		if machine.GetName() == params.MachineName {
			resources = append(resources, machine)
			break
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

//go:generate:params
type InspectClusterMachinesParams struct {
	ClusterName string `json:"clusterName" jsonschema:"the name of the provisioning cluster"`
	Namespace   string `json:"namespace" jsonschema:"the namespace of the resource, defaults to fleet-local if not set"`
}

// InspectClusterMachines returns the cluster API machines, machine sets, and machine deployments, for a given provisioning cluster.
func (t *Tools) InspectClusterMachines(ctx context.Context, toolReq *mcp.CallToolRequest, params InspectClusterMachinesParams) (*mcp.CallToolResult, any, error) {
	zap.L().Info("InspectClusterMachines invoked",
		zap.String("clusterName", params.ClusterName),
		zap.String("namespace", params.Namespace))

	machines, machineSets, machineDeployments, err := t.getCAPIMachineResources(ctx, toolReq, getCAPIMachineResourcesParams{
		namespace:     "fleet-default",
		targetCluster: params.ClusterName,
	})
	if err != nil {
		zap.L().Error("failed to lookup capi machine resources",
			zap.String("tool", "GetClusterMachine"),
			zap.String("machine", params.ClusterName),
			zap.Error(err))
		return nil, nil, err
	}

	var resources []*unstructured.Unstructured
	resources = append(resources, machines...)
	resources = append(resources, machineSets...)
	resources = append(resources, machineDeployments...)

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
type InspectClusterParams struct {
	ClusterName string `json:"clusterName" jsonschema:"the name of the provisioning cluster"`
	Namespace   string `json:"namespace" jsonschema:"the namespace of the resource, defaults to fleet-local if not set"`
}

// InspectCluster returns a set of kubernetes resources that can be used to inspect the cluster for debugging and summary purposes.
func (t *Tools) InspectCluster(ctx context.Context, toolReq *mcp.CallToolRequest, params InspectClusterParams) (*mcp.CallToolResult, any, error) {
	ns := params.Namespace
	if ns == "" {
		ns = "fleet-default"
	}

	zap.L().Info("InspectCluster invoked",
		zap.String("clusterName", params.ClusterName),
		zap.String("namespace", ns))

	var resources []*unstructured.Unstructured

	// get the provisioning cluster, we need it to get the machine pools and management cluster
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

	// get the management cluster
	managementClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "managementcluster",
		Namespace: "",
		Name:      provCluster.Status.ClusterName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get management cluster",
			zap.String("tool", "InspectCluster"),
			zap.String("cluster", provCluster.Status.ClusterName),
			zap.String("namespace", ns),
			zap.Error(err))
		return nil, nil, err
	}
	resources = append(resources, managementClusterResource)
	zap.L().Info("found management cluster")

	// get the CAPI cluster
	capiClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "capicluster",
		Namespace: "fleet-default",
		Name:      provCluster.Name,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get CAPI cluster cluster",
			zap.String("tool", "InspectCluster"),
			zap.String("cluster", provCluster.Name),
			zap.String("namespace", ns),
			zap.Error(err))
		return nil, nil, err
	}
	resources = append(resources, capiClusterResource)
	zap.L().Info("found CAPI cluster")

	// get information on machine pools and machine configs
	pools := provCluster.Spec.RKEConfig.MachinePools
	for _, pool := range pools {
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
				zap.String("tool", "InspectCluster"),
				zap.String("machineConfigResource", fmt.Sprintf("%ss", strings.ToLower(configGVK.Kind))),
				zap.String("configName", pool.NodeConfig.Name),
				zap.String("poolName", pool.Name),
				zap.String("poolKind", pool.NodeConfig.Kind),
				zap.Error(err))
			return nil, nil, err
		}
		zap.L().Info("found machine config")
		resources = append(resources, config)
	}

	// get all the CAPI resources for the cluster machines
	machines, machineSets, machineDeployments, err := t.getCAPIMachineResources(ctx, toolReq, getCAPIMachineResourcesParams{
		namespace:     params.Namespace,
		targetCluster: params.ClusterName,
	})
	if err != nil {
		zap.L().Error("failed to lookup capi machines",
			zap.String("tool", "inspectCluster"),
			zap.String("cluster", params.ClusterName),
			zap.String("namespace", ns),
			zap.Error(err))
	}
	resources = append(resources, machines...)
	resources = append(resources, machineSets...)
	resources = append(resources, machineDeployments...)

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
		"default", // TBD, may be irrelevant to provisioning issues.
	}

	for _, ns := range standardEventNamespaces {
		events, err := clientset.EventsV1().Events(ns).List(ctx, metav1.ListOptions{
			Limit: 15,
		})
		if err != nil {
			zap.L().Error("failed to get events for namespace",
				zap.String("tool", "inspectCluster"),
				zap.String("namespace", ns),
				zap.Error(err))
			return nil, nil, err
		}
		for _, event := range events.Items {
			unstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&event)
			if err != nil {
				zap.L().Error("failed to convert event to unstructured object",
					zap.String("tool", "inspectCluster"),
					zap.String("namespace", ns),
					zap.String("event", event.String()),
					zap.Error(err))
				return nil, nil, err
			}
			resources = append(resources, &unstructured.Unstructured{Object: unstruct})
		}
	}

	mcpResponse, err := response.CreateMcpResponse(resources, "local")
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "inspectCluster"), zap.Error(err))
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
	zap.L().Info("getNodes called",
		zap.String("cluster", params.Cluster))

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
	zap.L().Info("getClusterImages called",
		zap.String("clusters", strings.Join(params.Clusters, ", ")))

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

type getCAPIMachineResourcesParams struct {
	namespace     string
	targetCluster string
	machineName   string
}

// getCAPIMachineResources retrieves the cluster API machines, machine sets, and machine deployments for a given provisioning cluster.
func (t *Tools) getCAPIMachineResources(ctx context.Context, toolReq *mcp.CallToolRequest, params getCAPIMachineResourcesParams) (machines, machineSets, machineDeployments []*unstructured.Unstructured, err error) {
	if params.namespace == "" {
		params.namespace = "fleet-default"
	}
	zap.L().Info("getCAPIMachineResources called",
		zap.String("namespace", params.namespace),
		zap.String("targetCluster", params.targetCluster))

	provisioningClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   "local",
		Kind:      "provisioningcluster",
		Namespace: params.namespace,
		Name:      params.targetCluster,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get provisioning cluster while inspecting CAPI machines",
			zap.String("tool", "inspectProvisioningCluster"),
			zap.String("cluster", "local"),
			zap.String("namespace", params.namespace),
			zap.Error(err))
		return nil, nil, nil, err
	}

	provCluster := provisioningV1.Cluster{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(provisioningClusterResource.Object, &provCluster)
	if err != nil {
		zap.L().Error("failed to convert to provisioning cluster",
			zap.String("tool", "inspectProvisioningCluster"),
			zap.Error(err))
		return nil, nil, nil, err
	}

	machineSetMap := make(map[string]*unstructured.Unstructured)
	machineDeploymentMap := make(map[string]*unstructured.Unstructured)
	pools := provCluster.Spec.RKEConfig.MachinePools
	for _, pool := range pools {
		poolSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				"cluster.x-k8s.io/cluster-name":       provCluster.Name,
				"rke.cattle.io/rke-machine-pool-name": pool.Name,
			},
		})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create machine selector for cluster machines")
		}

		// get all the machines from the pool, to show status
		machines, err = t.getResources(ctx, ListParams{
			Cluster:       "local",
			Kind:          "machine",
			Namespace:     params.namespace,
			URL:           toolReq.Extra.Header.Get(urlHeader),
			Token:         toolReq.Extra.Header.Get(tokenHeader),
			LabelSelector: poolSelector.String(),
		})
		if err != nil {
			zap.L().Error("failed to get machines",
				zap.String("pool", pool.Name),
				zap.String("cluster", provCluster.Name),
				zap.Error(err))
			return nil, nil, nil, err
		}

		for _, machine := range machines {
			if machine.GetName() == params.machineName || params.machineName == "" {
				machines = append(machines, machine)
				setName := ""
				for _, owner := range machine.GetOwnerReferences() {
					if owner.Kind == "MachineSet" {
						setName = owner.Name
					}
				}

				var machineSet *unstructured.Unstructured
				if _, ok := machineSetMap[machine.GetName()]; !ok {
					machineSet, err = t.getResource(ctx, GetParams{
						Cluster:   "local",
						Kind:      "machineset",
						Namespace: params.namespace,
						Name:      setName,
						URL:       toolReq.Extra.Header.Get(urlHeader),
						Token:     toolReq.Extra.Header.Get(tokenHeader),
					})
					if err != nil {
						zap.L().Error("failed to get machine set",
							zap.String("machineSet", setName),
							zap.String("machine", machine.GetName()),
							zap.Error(err))
						return nil, nil, nil, err
					}
					machineSets = append(machineSets, machineSet)
					machineSetMap[machineSet.GetName()] = machineSet
				}

				if _, ok := machineDeploymentMap[machine.GetName()]; !ok && machineSet != nil {
					for _, owner := range machineSet.GetOwnerReferences() {
						if owner.Kind == "MachineDeployment" {
							machineDeployment, err := t.getResource(ctx, GetParams{
								Cluster:   "local",
								Kind:      "machinedeployment",
								Namespace: params.namespace,
								Name:      owner.Name,
								URL:       toolReq.Extra.Header.Get(urlHeader),
								Token:     toolReq.Extra.Header.Get(tokenHeader),
							})
							if err != nil {
								zap.L().Error("failed to get machine deployment",
									zap.String("machineDeployment", owner.Name),
									zap.String("machineSet", setName),
									zap.String("machine", machine.GetName()),
									zap.Error(err))
								return nil, nil, nil, err
							}
							machineDeployments = append(machineDeployments, machineDeployment)
							machineDeploymentMap[machineDeployment.GetName()] = machineDeployment
						}
					}
				}
			}
		}

	}

	if len(machines) == 0 {
		return nil, nil, nil, fmt.Errorf("failed to find any cluster API machine resources for cluster %s", params.targetCluster)
	}

	return machines, machineSets, machineDeployments, nil
}
