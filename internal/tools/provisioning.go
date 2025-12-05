package tools

import (
	"context"
	"fmt"
	"strings"

	"mcp/internal/tools/converter"
	"mcp/internal/tools/response"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	provisioningV1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ClusterType string

const (
	CAPIMachineDeploymentKind = "MachineDeployment"
	CAPIMachineSetKind        = "MachineSet"

	ClusterTypeImported   ClusterType = "imported"
	ClusterTypeCustom     ClusterType = "custom"
	ClusterTypeNodeDriver ClusterType = "nodedriver"
	ClusterTypeHosted     ClusterType = "hosted"
)

type GetClusterMachineParams struct {
	Cluster     string `json:"cluster" jsonschema:"the name of the cluster the machines belong to"`
	MachineName string `json:"machineName" jsonschema:"the name of the machine to retrieve, if not set all machines for the cluster are returned"`
}

// GetClusterMachine returns the cluster API machine for a given provisioning cluster and machine name.
func (t *Tools) GetClusterMachine(ctx context.Context, toolReq *mcp.CallToolRequest, params GetClusterMachineParams) (*mcp.CallToolResult, any, error) {
	log := NewChildLogger(toolReq, map[string]string{
		"cluster":     params.Cluster,
		"machineName": params.MachineName,
	})
	machines, _, _, err := t.getCAPIMachineResources(ctx, toolReq, log, getCAPIMachineResourcesParams{
		namespace:     "fleet-default",
		targetCluster: params.Cluster,
		machineName:   params.MachineName,
	})
	if err != nil {
		log.Error("failed to lookup capi machine",
			zap.String("tool", "GetClusterMachine"),
			zap.String("machine", params.MachineName),
			zap.Error(err))
		return nil, nil, err
	}

	mcpResponse, err := response.CreateMcpResponse(machines, params.Cluster)
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "inspectProvisioningCluster"), zap.Error(err))
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

type InspectClusterMachinesParams struct {
	Cluster   string `json:"cluster" jsonschema:"the name of the provisioning cluster"`
	Namespace string `json:"namespace" jsonschema:"the namespace of the resource"`
}

// InspectClusterMachines returns the cluster API machines, machine sets, and machine deployments, for a given provisioning cluster.
func (t *Tools) InspectClusterMachines(ctx context.Context, toolReq *mcp.CallToolRequest, params InspectClusterMachinesParams) (*mcp.CallToolResult, any, error) {
	ns := params.Namespace
	if ns == "" {
		ns = "fleet-default"
	}

	log := NewChildLogger(toolReq, map[string]string{
		"cluster":   params.Cluster,
		"namespace": params.Namespace,
	})

	machines, machineSets, machineDeployments, err := t.getCAPIMachineResources(ctx, toolReq, log, getCAPIMachineResourcesParams{
		namespace:     ns,
		targetCluster: params.Cluster,
	})
	if err != nil {
		log.Error("failed to lookup CAPI machine resources", zap.Error(err))
		return nil, nil, err
	}

	var resources []*unstructured.Unstructured
	resources = append(resources, machines...)
	resources = append(resources, machineSets...)
	resources = append(resources, machineDeployments...)

	mcpResponse, err := response.CreateMcpResponse(resources, params.Cluster)
	if err != nil {
		log.Error("failed to create mcp response", zap.Error(err))
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

type InspectClusterParams struct {
	Cluster   string `json:"cluster" jsonschema:"the name of the provisioning cluster"`
	Namespace string `json:"namespace" jsonschema:"the namespace of the resource, defaults to fleet-local if not set"`
}

// InspectCluster returns a set of kubernetes resources that can be used to inspect the cluster for debugging and summary purposes.
func (t *Tools) InspectCluster(ctx context.Context, toolReq *mcp.CallToolRequest, params InspectClusterParams) (*mcp.CallToolResult, any, error) {
	ns := params.Namespace
	if ns == "" {
		ns = "fleet-default"
	}

	if params.Cluster != "local" {
		ns = "fleet-default"
	}

	log := NewChildLogger(toolReq, map[string]string{
		"cluster":   params.Cluster,
		"namespace": params.Namespace,
	})
	var resources []*unstructured.Unstructured

	provClusterResource, provCluster, err := t.getProvisioningCluster(ctx, toolReq, log, ns, params.Cluster)
	if err != nil {
		log.Error("failed to get provisioning cluster", zap.Error(err))
		return nil, nil, err
	}
	if provClusterResource == nil {
		log.Warn("provisioning cluster resource is nil, unsupported cluster type for tool")
		return nil, nil, fmt.Errorf("provisioning cluster %s not found in namespace %s, incorrect tool usage", params.Cluster, ns)
	}

	resources = append(resources, provClusterResource)

	// get the CAPI cluster
	capiClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   LocalCluster,
		Kind:      converter.CAPIClusterResourceKind,
		Namespace: "fleet-default",
		Name:      provCluster.Name,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error("failed to get CAPI cluster", zap.Error(err))
		return nil, nil, err
	} else {
		log.Info("found CAPI cluster")
		resources = append(resources, capiClusterResource)
	}

	// get all machine configs for node driver clusters.
	machineConfigs, err := t.getMachinePoolConfigs(ctx, toolReq, log, provCluster)
	if err != nil {
		log.Error("failed to get machine pool configs", zap.Error(err))
		return nil, nil, err
	}
	resources = append(resources, machineConfigs...)

	// get all the CAPI machine resources
	machines, machineSets, machineDeployments, err := t.getCAPIMachineResources(ctx, toolReq, log, getCAPIMachineResourcesParams{
		namespace:     params.Namespace,
		targetCluster: params.Cluster,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error("failed to lookup capi machines", zap.Error(err))
		return nil, nil, err
	} else {
		log.Info("found capi machines",
			zap.Int("machines", len(machines)),
			zap.Int("machineSets", len(machineSets)),
			zap.Int("machineDeployments", len(machineDeployments)))
	}
	resources = append(resources, machines...)
	resources = append(resources, machineSets...)
	resources = append(resources, machineDeployments...)

	// get the management cluster, its status may be relevant.
	// NB: We can't directly import the v3.Cluster type, since it
	// pulls in a lot of indirect dependencies. So we just use unstructured here.
	managementClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   LocalCluster,
		Kind:      converter.ManagementClusterResourceKind,
		Namespace: "",
		Name:      provCluster.Status.ClusterName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		log.Error("failed to get management cluster", zap.Error(err))
		return nil, nil, err
	}
	resources = append(resources, managementClusterResource)
	log.Info("found management cluster")

	// get registration tokens for custom and generic imported clusters. Don't do this for
	// other types of clusters to prevent the LLM from incorrectly suggesting that
	// users manually run them.
	poolSize := 0
	if provCluster.Spec.RKEConfig != nil {
		poolSize = len(provCluster.Spec.RKEConfig.MachinePools)
	}
	clusterType, err := t.getClusterType(poolSize, managementClusterResource)
	// explicitly inform the LLM of the cluster type, so it can reference the correct docs
	resources = append(resources, &unstructured.Unstructured{Object: map[string]interface{}{
		"clusterType": string(clusterType),
	}})
	if clusterType == ClusterTypeCustom || clusterType == ClusterTypeImported {
		registrationToken, err := t.getResource(ctx, GetParams{
			Cluster:   LocalCluster,
			Kind:      converter.ClusterRegistrationTokenResourceKind,
			Namespace: provCluster.Status.ClusterName,
			Name:      "default-token",
			URL:       toolReq.Extra.Header.Get(urlHeader),
			Token:     toolReq.Extra.Header.Get(tokenHeader),
		})
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("failed to get cluster registration token: %v", err)
		}
		if registrationToken != nil {
			log.Info("found registration token")
			resources = append(resources, registrationToken)
		}
	}

	mcpResponse, err := response.CreateMcpResponse(resources, params.Cluster)
	if err != nil {
		log.Error("failed to create mcp response", zap.String("tool", "inspectCluster"), zap.Error(err))
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
	log := NewChildLogger(toolReq, map[string]string{
		"cluster": params.Cluster,
	})
	nodeResource, err := t.getResources(ctx, ListParams{
		Cluster: params.Cluster,
		Kind:    "node",
		URL:     toolReq.Extra.Header.Get(urlHeader),
		Token:   toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		log.Error("failed to get nodes", zap.String("tool", "getNodes"), zap.Error(err))
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
		log.Error("failed to create mcp response", zap.String("tool", "getNodes"), zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

type getCAPIMachineResourcesParams struct {
	namespace     string
	targetCluster string
	machineName   string
}

// getCAPIMachineResources retrieves the cluster API machines, machine sets, and machine deployments for a given provisioning cluster.
func (t *Tools) getCAPIMachineResources(ctx context.Context, toolReq *mcp.CallToolRequest, log *zap.Logger, params getCAPIMachineResourcesParams) ([]*unstructured.Unstructured, []*unstructured.Unstructured, []*unstructured.Unstructured, error) {
	if params.namespace == "" {
		params.namespace = "fleet-default"
	}

	clusterSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"cluster.x-k8s.io/cluster-name": params.targetCluster,
		},
	})
	if err != nil {
		log.Error("failed to get pool selector", zap.Error(err))
		return nil, nil, nil, fmt.Errorf("failed to create machine selector for cluster machines")
	}

	// get all the machines from the cluster
	machines, err := t.getResources(ctx, ListParams{
		Cluster:       LocalCluster,
		Kind:          converter.CAPIMachineResourceKind,
		Namespace:     params.namespace,
		URL:           toolReq.Extra.Header.Get(urlHeader),
		Token:         toolReq.Extra.Header.Get(tokenHeader),
		LabelSelector: clusterSelector.String(),
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("no CAPI machines found for cluster")
			return nil, nil, nil, nil
		}

		log.Error("failed to get machines", zap.Error(err))
		return nil, nil, nil, err
	}

	// there can be a lot of machines within a single set and deployment,
	// we only care about unique instances of machine sets and deployments
	machineSetMap := make(map[string]struct{})
	machineDeploymentMap := make(map[string]struct{})
	var capiMachines, capiMachineSets, capiMachineDeployments []*unstructured.Unstructured

	for _, machine := range machines {
		// if we have a specific machine name, filter out just that one
		if params.machineName != "" && machine.GetName() != params.machineName {
			continue
		}

		capiMachines = append(capiMachines, machine)
		setName := ""
		for _, owner := range machine.GetOwnerReferences() {
			if owner.Kind == CAPIMachineSetKind {
				setName = owner.Name
			}
		}

		if setName == "" {
			log.Warn("found a CAPI machine without an associated machine set")
			continue
		}

		var machineSet, machineDeployment *unstructured.Unstructured
		if _, ok := machineSetMap[setName]; !ok {
			machineSet, err = t.getResource(ctx, GetParams{
				Cluster:   "local",
				Kind:      converter.CAPIMachineSetResourceKind,
				Namespace: params.namespace,
				Name:      setName,
				URL:       toolReq.Extra.Header.Get(urlHeader),
				Token:     toolReq.Extra.Header.Get(tokenHeader),
			})
			if err != nil {
				log.Error("failed to get machine set", zap.Error(err))
				return nil, nil, nil, err
			}
			capiMachineSets = append(capiMachineSets, machineSet)
			machineSetMap[machineSet.GetName()] = struct{}{}
		}

		if machineSet == nil {
			continue
		}

		for _, owner := range machineSet.GetOwnerReferences() {
			if owner.Kind != CAPIMachineDeploymentKind {
				continue
			}
			if _, ok := machineDeploymentMap[owner.Name]; ok {
				continue
			}

			machineDeployment, err = t.getResource(ctx, GetParams{
				Cluster:   "local",
				Kind:      converter.CAPIMachineDeploymentResourceKind,
				Namespace: params.namespace,
				Name:      owner.Name,
				URL:       toolReq.Extra.Header.Get(urlHeader),
				Token:     toolReq.Extra.Header.Get(tokenHeader),
			})
			if err != nil {
				log.Error("failed to get machine deployment", zap.Error(err))
				return nil, nil, nil, err
			}
			capiMachineDeployments = append(capiMachineDeployments, machineDeployment)
			machineDeploymentMap[owner.Name] = struct{}{}
		}
	}

	return capiMachines, capiMachineSets, capiMachineDeployments, nil
}

func (t *Tools) getProvisioningCluster(ctx context.Context, toolReq *mcp.CallToolRequest, log *zap.Logger, ns, clusterName string) (*unstructured.Unstructured, provisioningV1.Cluster, error) {
	provisioningClusterResource, err := t.getResource(ctx, GetParams{
		Cluster:   LocalCluster,
		Kind:      converter.ProvisioningClusterResourceKind,
		Namespace: ns,
		Name:      clusterName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("provisioning cluster not found")
		}
		log.Error("failed to get provisioning cluster", zap.Error(err))
		return nil, provisioningV1.Cluster{}, err
	}

	provCluster := provisioningV1.Cluster{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(provisioningClusterResource.Object, &provCluster)
	if err != nil {
		log.Error("failed to convert to provisioning cluster", zap.Error(err))
		return nil, provCluster, err
	}

	return provisioningClusterResource, provCluster, nil
}

func (t *Tools) getMachinePoolConfigs(ctx context.Context, toolReq *mcp.CallToolRequest, log *zap.Logger, provCluster provisioningV1.Cluster) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured
	pools := provCluster.Spec.RKEConfig.MachinePools
	for _, pool := range pools {
		configGVK := pool.NodeConfig.GroupVersionKind()
		config, err := t.getResourceByGVR(ctx, GetParams{
			Cluster:   LocalCluster,
			Namespace: "fleet-default",
			Name:      pool.NodeConfig.Name,
			URL:       toolReq.Extra.Header.Get(urlHeader),
			Token:     toolReq.Extra.Header.Get(tokenHeader),
		}, schema.GroupVersionResource{
			Group:   "rke-machine-config.cattle.io",
			Version: "v1",
			// the object reference doesn't explicitly state the exact resource,
			// but we know that all node driver config ref's will follow the below format.
			Resource: fmt.Sprintf("%ss", strings.ToLower(configGVK.Kind)),
		})
		if apierrors.IsNotFound(err) {
			log.Info("machine config not found for pool, skipping", zap.String("pool", pool.Name))
			continue
		}
		if err != nil {
			log.Error("failed to get machine config from pool", zap.Error(err))
			return nil, err
		}
		resources = append(resources, config)
	}
	return resources, nil
}

func (t *Tools) getEventsForNamespaces(ctx context.Context, toolReq *mcp.CallToolRequest, log *zap.Logger, namespaces []string) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured
	clientset, err := t.client.CreateClientSet(toolReq.Extra.Header.Get(tokenHeader), toolReq.Extra.Header.Get(urlHeader), "local")
	if err != nil {
		log.Error("failed to create kubernetes client set", zap.Error(err))
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	for _, ns := range namespaces {
		events, err := clientset.EventsV1().Events(ns).List(ctx, metav1.ListOptions{
			Limit: 15,
		})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			log.Error("failed to get events for namespace", zap.Error(err))
			return nil, err
		}
		for _, event := range events.Items {
			unstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&event)
			if err != nil {
				log.Error("failed to convert event to unstructured object", zap.Error(err))
				return nil, err
			}
			resources = append(resources, &unstructured.Unstructured{Object: unstruct})
		}
	}
	return resources, nil
}

func (t *Tools) getClusterType(poolSize int, managementCluster *unstructured.Unstructured) (ClusterType, error) {
	provider, providerFound, err := unstructured.NestedString(managementCluster.Object, "status", "provider")
	if err != nil {
		return "", fmt.Errorf("failed to get management cluster provider info: %v", err)
	}

	driver, driverFound, err := unstructured.NestedString(managementCluster.Object, "status", "driver")
	if err != nil {
		return "", fmt.Errorf("failed to get management cluster driver info: %v", err)
	}

	if !providerFound && !driverFound {
		return "", fmt.Errorf("failed to get management cluster driver info: no provider info")
	}

	importedHosted := []string{"aksConfig", "eksConfig", "gkeConfig", "aliConfig"}
	isImportedHosted := false
	isHosted := false
	for _, hosted := range importedHosted {
		imported, foundImported, err := unstructured.NestedBool(managementCluster.Object, "spec", hosted, "imported")
		if err != nil {
			return "", fmt.Errorf("failed to get management cluster hosted imported info: %v", err)
		}
		if foundImported {
			isHosted = true
			if imported {
				isImportedHosted = true
			}
			break
		}
	}
	if isHosted {
		if isImportedHosted {
			return ClusterTypeImported, nil
		}
		return ClusterTypeHosted, nil
	}

	// If the cluster is imported or custom, get the registration commands.
	// We don't want to include registration tokens for non-imported/cluster clusters
	// as we don't want the LLM to incorrectly suggest that users manually run them.
	if provider == "rke2" || provider == "k3s" {
		if provider == driver {
			// not an imported cluster
			return ClusterTypeImported, nil
		}
	}

	if poolSize == 0 {
		// custom cluster
		return ClusterTypeCustom, nil
	}

	return ClusterTypeNodeDriver, nil
}
