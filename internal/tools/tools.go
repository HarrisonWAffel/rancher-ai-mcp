package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"mcp/internal/tools/converter"
	"mcp/internal/tools/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	tokenHeader      = "R_token"
	urlHeader        = "R_url"
	podLogsTailLines = 50
)

// GetParams holds the parameters required to get a resource from k8s.
type GetParams struct {
	Cluster   string // The Cluster ID.
	Kind      string // The Kind of the Kubernetes resource (e.g., "pod", "deployment").
	Namespace string // The Namespace of the resource (optional).
	Name      string // The Name of the resource (optional).
	URL       string // The base URL of the Rancher server.
	Token     string // The authentication Token for Steve.
}

// ListParams holds the parameters required to list resources from k8s.
type ListParams struct {
	Cluster       string // The Cluster ID.
	Kind          string // The Kind of the Kubernetes resource (e.g., "pod", "deployment").
	Namespace     string // The Namespace of the resource (optional).
	Name          string // The Name of the resource (optional).
	URL           string // The base URL of the Rancher server.
	Token         string // The authentication Token for Steve.
	LabelSelector string // Optional LabelSelector string for the request.
}

// ResourceParams uniquely identifies a specific named resource within a cluster.
type ResourceParams struct {
	Name      string `json:"name" jsonschema:"the name of k8s resource"`
	Namespace string `json:"namespace" jsonschema:"the namespace of the resource"`
	Kind      string `json:"kind" jsonschema:"the kind of the resource"`
	Cluster   string `json:"cluster" jsonschema:"the cluster of the resource"`
}

// GetNodesParams specifies the parameters needed to retrieve node metrics.
type GetNodesParams struct {
	Cluster string `json:"cluster" jsonschema:"the cluster of the resource"`
}

type JSONPatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// UpdateKubernetesResourceParams defines the structure for updating a general Kubernetes resource.
// It includes fields required to uniquely identify a resource within a cluster.
type UpdateKubernetesResourceParams struct {
	Name      string      `json:"name" jsonschema:"the name of k8s resource"`
	Namespace string      `json:"namespace" jsonschema:"the namespace of the resource"`
	Kind      string      `json:"kind" jsonschema:"the kind of the resource"`
	Cluster   string      `json:"cluster" jsonschema:"the cluster of the resource"`
	Patch     []JSONPatch `json:"patch" jsonschema:"the patch of the request"`
}

// CreateKubernetesResourceParams defines the structure for creating a general Kubernetes resource.
type CreateKubernetesResourceParams struct {
	Name      string `json:"name" jsonschema:"the name of k8s resource"`
	Namespace string `json:"namespace" jsonschema:"the namespace of the resource"`
	Kind      string `json:"kind" jsonschema:"the kind of the resource"`
	Cluster   string `json:"cluster" jsonschema:"the cluster of the resource"`
	Resource  any    `json:"resource" jsonschema:"the resource to be created"`
}

// ListKubernetesResourcesParams specifies the parameters needed to list kubernetes resources.
type ListKubernetesResourcesParams struct {
	Namespace string `json:"namespace" jsonschema:"the namespace of the resource"`
	Kind      string `json:"kind" jsonschema:"the kind of the resource"`
	Cluster   string `json:"cluster" jsonschema:"the cluster of the resource"`
}

// SpecificResourceParams uniquely identifies a resource with a known kind within a cluster.
type SpecificResourceParams struct {
	Name      string `json:"name" jsonschema:"the name of k8s resource"`
	Namespace string `json:"namespace" jsonschema:"the namespace of the resource"`
	Cluster   string `json:"cluster" jsonschema:"the cluster of the resource"`
}

type GetClusterImagesParams struct {
	Clusters []string `json:"clusters" jsonschema:"the clusters where images are returned"`
}

// ContainerLogs holds logs for multiple containers.
type ContainerLogs struct {
	Logs map[string]any `json:"logs"`
}

// K8sClient defines an interface for a Kubernetes client.
type K8sClient interface {
	GetResourceInterface(token string, url string, namespace string, cluster string, gvr schema.GroupVersionResource) (dynamic.ResourceInterface, error)
	CreateClientSet(token string, url string, cluster string) (kubernetes.Interface, error)
}

// Tools contains all tools for the MCP server
type Tools struct {
	client K8sClient
}

// NewTools creates and returns a new Tools instance.
func NewTools() *Tools {
	return &Tools{
		client: k8s.NewClient(),
	}
}

func (t *Tools) getPodLogs(ctx context.Context, url string, cluster string, token string, pod corev1.Pod) (*unstructured.Unstructured, error) {
	clientset, err := t.client.CreateClientSet(token, url, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	logs := ContainerLogs{
		Logs: make(map[string]any),
	}
	for _, container := range pod.Spec.Containers {
		podLogOptions := corev1.PodLogOptions{
			TailLines: ptr.To[int64](podLogsTailLines),
			Container: container.Name,
		}
		req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOptions)
		podLogs, err := req.Stream(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to open log stream: %v", err)
		}
		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			return nil, fmt.Errorf("failed to copy log stream to buffer: %v", err)
		}
		logs.Logs[container.Name] = buf.String()
		if err := podLogs.Close(); err != nil {
			return nil, fmt.Errorf("failed to close pod logs stream: %v", err)
		}
	}

	return &unstructured.Unstructured{Object: map[string]interface{}{"pod-logs": logs.Logs}}, nil
}

func (t *Tools) getResource(ctx context.Context, params GetParams) (*unstructured.Unstructured, error) {
	resourceInterface, err := t.client.GetResourceInterface(params.Token, params.URL, params.Namespace, params.Cluster, converter.K8sKindsToGVRs[strings.ToLower(params.Kind)])
	if err != nil {
		return nil, err
	}

	obj, err := resourceInterface.Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return obj, err
}

func (t *Tools) getResources(ctx context.Context, params ListParams) ([]*unstructured.Unstructured, error) {
	resourceInterface, err := t.client.GetResourceInterface(params.Token, params.URL, params.Namespace, params.Cluster, converter.K8sKindsToGVRs[strings.ToLower(params.Kind)])
	if err != nil {
		return nil, err
	}

	opts := metav1.ListOptions{}
	if params.LabelSelector != "" {
		opts.LabelSelector = params.LabelSelector
	}
	list, err := resourceInterface.List(ctx, opts)
	if err != nil {
		return nil, err
	}

	objs := make([]*unstructured.Unstructured, len(list.Items))
	for i := range list.Items {
		objs[i] = &list.Items[i]
	}

	return objs, err
}
