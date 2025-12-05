package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp/internal/tools/converter"
	"strings"

	"mcp/internal/tools/response"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// InspectPod retrieves detailed information about a specific pod, its owner, metrics, and logs.
func (t *Tools) InspectPod(ctx context.Context, toolReq *mcp.CallToolRequest, params SpecificResourceParams) (*mcp.CallToolResult, any, error) {
	zap.L().Debug("inspectPod called")

	podResource, err := t.getResource(ctx, GetParams{
		Cluster:   params.Cluster,
		Kind:      "pod",
		Namespace: params.Namespace,
		Name:      params.Name,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get Pod", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, err
	}

	var pod corev1.Pod
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podResource.Object, &pod); err != nil {
		zap.L().Error("failed to convert unstructured object to Pod", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to convert unstructured object to Pod: %w", err)
	}

	// find the parent of the pod
	var replicaSetName string
	for _, or := range pod.OwnerReferences {
		if or.Kind == "ReplicaSet" {
			replicaSetName = or.Name
			break
		}
	}
	replicaSetResource, err := t.getResource(ctx, GetParams{
		Cluster:   params.Cluster,
		Kind:      "replicaset",
		Namespace: params.Namespace,
		Name:      replicaSetName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get ReplicaSet", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, err
	}

	var replicaSet appsv1.ReplicaSet
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(replicaSetResource.Object, &replicaSet); err != nil {
		zap.L().Error("failed to convert unstructured object to ReplicaSet", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to convert unstructured object to Pod: %w", err)
	}

	var parentName, parentKind string
	for _, or := range replicaSet.OwnerReferences {
		if or.Kind == "Deployment" {
			parentName = or.Name
			parentKind = or.Kind
			break
		}
		if or.Kind == "StatefulSet" {
			parentName = or.Name
			parentKind = or.Kind
			break
		}
		if or.Kind == "DaemonSet" {
			parentName = or.Name
			parentKind = or.Kind
			break
		}
	}
	parentResource, err := t.getResource(ctx, GetParams{
		Cluster:   params.Cluster,
		Kind:      parentKind,
		Namespace: params.Namespace,
		Name:      parentName,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get parent resource", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, err
	}

	// ignore error as Metrics Server might not be installed in the cluster
	podMetrics, _ := t.getResource(ctx, GetParams{
		Cluster:   params.Cluster,
		Kind:      "pod.metrics.k8s.io",
		Namespace: params.Namespace,
		Name:      params.Name,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})

	logs, err := t.getPodLogs(ctx, toolReq.Extra.Header.Get(urlHeader), params.Cluster, toolReq.Extra.Header.Get(tokenHeader), pod)
	if err != nil {
		zap.L().Error("failed to get pod logs", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, err
	}

	resources := []*unstructured.Unstructured{podResource, parentResource, logs}
	if podMetrics != nil {
		resources = append(resources, podMetrics)
	}

	mcpResponse, err := response.CreateMcpResponse(resources, params.Cluster)
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "inspectPod"), zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mcpResponse}},
	}, nil, nil
}

// GetDeploymentDetails retrieves details about a deployment and its associated pods.
func (t *Tools) GetDeploymentDetails(ctx context.Context, toolReq *mcp.CallToolRequest, params SpecificResourceParams) (*mcp.CallToolResult, any, error) {
	zap.L().Debug("getDeploymentDetails called")

	deploymentResource, err := t.getResource(ctx, GetParams{
		Cluster:   params.Cluster,
		Kind:      "deployment",
		Namespace: params.Namespace,
		Name:      params.Name,
		URL:       toolReq.Extra.Header.Get(urlHeader),
		Token:     toolReq.Extra.Header.Get(tokenHeader),
	})
	if err != nil {
		zap.L().Error("failed to get deployment", zap.String("tool", "getDeploymentDetails"), zap.Error(err))
		return nil, nil, err
	}

	var deployment appsv1.Deployment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(deploymentResource.Object, &deployment); err != nil {
		zap.L().Error("failed convert unstructured object to Deployment", zap.String("tool", "getDeploymentDetails"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to convert unstructured object to Pod: %w", err)
	}

	// find all pods for this deployment
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		zap.L().Error("failed create label selector", zap.String("tool", "getDeploymentDetails"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to convert label selector: %w", err)
	}
	pods, err := t.getResources(ctx, ListParams{
		Cluster:       params.Cluster,
		Kind:          "pod",
		Namespace:     params.Namespace,
		Name:          params.Name,
		URL:           toolReq.Extra.Header.Get(urlHeader),
		Token:         toolReq.Extra.Header.Get(tokenHeader),
		LabelSelector: selector.String(),
	})
	if err != nil {
		zap.L().Error("failed to get pods", zap.String("tool", "getDeploymentDetails"), zap.Error(err))
		return nil, nil, fmt.Errorf("failed to get pods: %w", err)
	}

	mcpResponse, err := response.CreateMcpResponse(append([]*unstructured.Unstructured{deploymentResource}, pods...), params.Cluster)
	if err != nil {
		zap.L().Error("failed to create mcp response", zap.String("tool", "getDeploymentDetails"), zap.Error(err))
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
			Cluster: LocalCluster,
			Kind:    converter.ManagementClusterResourceKind,
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
