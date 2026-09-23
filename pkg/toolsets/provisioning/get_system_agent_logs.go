package provisioning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rancher/rancher-ai-mcp/internal/middleware"
	"github.com/rancher/rancher-ai-mcp/pkg/converter"
	"github.com/rancher/rancher-ai-mcp/pkg/response"
	nodeUtils "github.com/rancher/rancher-ai-mcp/pkg/toolsets/provisioning/utils"
	"github.com/rancher/rancher-ai-mcp/pkg/utils"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type investigateFailedPlanApplicationParams struct {
	Cluster   string `json:"cluster" jsonschema:"the name of the Kubernetes cluster"`
	Namespace string `json:"namespace" jsonschema:"the namespace where the resource is located. The default namespace will be used if not provided"`
	NodeName  string `json:"nodeName" jsonschema:"the name of the node to which is failing to execute a plan"`
	// logsStartAt: The time we filter the most recent journald log from, so we can look far in the past. length is scoped by the expected line count of two subsequent plans.
	//
}

func (t *Tools) investigateFailedPlanApplication(ctx context.Context, toolReq *mcp.CallToolRequest, params investigateFailedPlanApplicationParams) (*mcp.CallToolResult, any, error) {
	log := utils.NewChildLogger(toolReq, map[string]string{
		"cluster_id": params.Cluster,
		"namespace":  params.Namespace,
		"nodeName":   params.NodeName,
	})

	if params.Cluster == "local" {
		return nil, nil, fmt.Errorf("cannot investigate failed plan for local cluster")
	}

	// get system-agent journald logs from the downstream node.
	logs, err := createJobAndPollPod(ctx, params, t.client, log)
	// todo: parse out the journald logs
	// get the last three executions?

	ulogs, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&logs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert logs: %w", err)
	}

	// get select fields from the machine plan for the requested node
	machinePlanFields, err := getMachinePlanFields(ctx, params.NodeName, t.client, log)
	if err != nil {
		log.Error("failed to get machine plan", zap.Error(err))
		return nil, nil, err
	}

	mcpResponse, err := response.CreateMcpResponse([]*unstructured.Unstructured{
		{Object: ulogs},
		{Object: machinePlanFields}}, params.Cluster)
	if err != nil {
		log.Error("failed to create mcp response", zap.Error(err))
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: mcpResponse,
		}},
	}, nil, nil
}

func createJobAndPollPod(ctx context.Context, params investigateFailedPlanApplicationParams, client toolsClient, log *zap.Logger) (utils.ContainerLogs, error) {
	job, err := nodeUtils.CreateSystemdLogGathererJob(fmt.Sprintf("systemd-log-gatherer-%s", params.NodeName), params.NodeName, "rancher-system-agent", log)
	if err != nil {
		log.Error("failed to create systemd job", zap.Error(err))
		return utils.ContainerLogs{}, err
	}

	ji, err := client.GetResourceInterface(ctx, middleware.Token(ctx), "cattle-system", params.Cluster, converter.K8sKindsToGVRs["job"])
	if err != nil {
		log.Error("failed to get job resource")
		return utils.ContainerLogs{}, err
	}

	jj, err := ji.Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		log.Error("failed to create job resource")
		return utils.ContainerLogs{}, err
	}

	pi, err := client.GetResourceInterface(ctx, middleware.Token(ctx), "cattle-system", params.Cluster, converter.K8sKindsToGVRs["pod"])
	if err != nil {
		return utils.ContainerLogs{}, err
	}

	ls := fmt.Sprintf("%s=%s", "job-name", jj.GetName())
	log.Info("polling for pod with label selector", zap.String("selector", ls))

	// Wait for the jobs pod to roll out
	pod, err := nodeUtils.PollForPod(ctx, ls, pi, log)
	if err != nil {
		return utils.ContainerLogs{}, err
	}

	clientset, err := client.CreateClientSet(ctx, middleware.Token(ctx), params.Cluster)
	if err != nil {
		return utils.ContainerLogs{}, fmt.Errorf("failed to create clientset: %w", err)
	}

	// Read the logs from stdout
	logs, err := utils.GetPodLogs(ctx, clientset, pod, 50)
	if err != nil {
		return utils.ContainerLogs{}, fmt.Errorf("failed to get pod logs: %w", err)
	}

	return logs, nil
}

func getMachinePlanFields(ctx context.Context, nodeName string, client toolsClient, log *zap.Logger) (map[string]interface{}, error) {
	si, err := client.GetResourceInterface(ctx, middleware.Token(ctx), "fleet-default", "local", converter.K8sKindsToGVRs["secret"])
	if err != nil {
		log.Error("failed to get secret resource interface", zap.Error(err))
		return nil, err
	}

	machinePlanName := fmt.Sprintf("%s-machine-plan", nodeName)

	machinePlanUnstruct, err := si.Get(ctx, machinePlanName, metav1.GetOptions{})
	if err != nil {
		log.Error("failed to get machine plan resource interface", zap.Error(err))
		return nil, err
	}

	machinePlan := &corev1.Secret{}
	j, err := machinePlanUnstruct.MarshalJSON()
	if err != nil {
		log.Error("failed to marshal machine plan resource interface", zap.Error(err))
		return nil, err
	}

	err = json.Unmarshal(j, &machinePlan)
	if err != nil {
		log.Error("failed to unmarshal machine plan resource interface", zap.Error(err))
		return nil, err
	}

	out := make(map[string]string)
	for _, field := range []string{
		"failed-output",
		"failure-count",
		"last-apply-time",
		"max-failures",
		"plan-revision",
		"plan-state",
		"probe-statuses",
	} {
		out[field] = string(machinePlan.Data[field])
	}

	uFields, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&out)
	if err != nil {
		return nil, fmt.Errorf("failed to convert logs: %w", err)
	}

	return uFields, nil
}
