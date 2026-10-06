package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rancher/rancher-ai-mcp/internal/middleware"
	"github.com/rancher/rancher-ai-mcp/pkg/converter"
	"github.com/rancher/rancher-ai-mcp/pkg/response"
	nodeUtils "github.com/rancher/rancher-ai-mcp/pkg/toolsets/provisioning/utils"
	"github.com/rancher/rancher-ai-mcp/pkg/utils"
	"github.com/rancher/rancher/pkg/plan"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type investigateFailedPlanApplicationParams struct {
	Cluster  string `json:"cluster" jsonschema:"the name of the Kubernetes cluster"`
	NodeName string `json:"nodeName" jsonschema:"the name of the node which is failing to execute a plan"`
}

func (params *investigateFailedPlanApplicationParams) validate(ctx context.Context, t *Tools, toolReq *mcp.CallToolRequest) (*zap.Logger, error) {
	log := utils.NewChildLogger(toolReq, map[string]string{
		"cluster":  params.Cluster,
		"nodeName": params.NodeName,
	})

	if params.Cluster == "local" {
		return nil, fmt.Errorf("plans are not used to control the local cluster")
	}

	if strings.TrimSpace(params.NodeName) == "" {
		return nil, fmt.Errorf("node name is required")
	}

	// ensure that the provided node name is actually valid for the specified cluster and
	// that it is managed by Rancher machine plans (i.e. CAPI compliant)
	machines, _, _, err := t.getAllCAPIMachineResources(ctx, log, getCAPIMachineResourcesParams{
		namespace:     "fleet-default",
		targetCluster: params.Cluster,
	})
	if err != nil && !errors.IsNotFound(err) {
		log.Error("failed to lookup CAPI machine resources", zap.Error(err))
		return nil, fmt.Errorf("did not find any CAPI machine resources for cluster %s, this cluster is likely not managed by Rancher machine plans or the system agent. this tool cannot be run against clusters which do not utilize CAPI (aks,eks,gke,k3k,etc.): %w", params.Cluster, err)
	}

	if len(machines) == 0 {
		return nil, fmt.Errorf("did not find any CAPI machine resources for cluster %s, this cluster is likely not managed by Rancher machine plans or the system agent. this tool cannot be run against clusters which do not utilize CAPI (aks,eks,gke,k3k,etc.)", params.Cluster)
	}

	found := false
	for _, machine := range machines {
		if machine.GetName() == params.NodeName {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("failed to find node %s", params.NodeName)
	}

	return log, nil
}

func (t *Tools) investigateFailedPlanApplication(ctx context.Context, toolReq *mcp.CallToolRequest, params investigateFailedPlanApplicationParams) (*mcp.CallToolResult, any, error) {
	log, err := params.validate(ctx, t, toolReq)
	if err != nil {
		return nil, nil, err
	}

	// get select fields from the machine plan for the requested node and the overall plan hash
	machinePlanFields, planHash, err := getMachinePlanFields(ctx, params.NodeName, t.client, log)
	if err != nil {
		log.Error("failed to get machine plan", zap.Error(err))
		return nil, nil, err
	}

	// get system-agent journald logs from the downstream node, filtering on the current machine
	// plan hash.
	logs, err := createJobAndPollPod(ctx, params, t.client, t.toolboxImage, planHash, log)
	if err != nil {
		log.Error("failed to create job", zap.Error(err))
		return nil, nil, err
	}

	ulogs, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&logs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert logs: %w", err)
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

func createJobAndPollPod(ctx context.Context, params investigateFailedPlanApplicationParams, client toolsClient, image, planHash string, log *zap.Logger) (utils.ContainerLogs, error) {
	// whatever image we use for the pod needs to have these three commands
	// installed.
	journald := "journalctl -eu rancher-system-agent.service --no-pager -o cat"
	grep := fmt.Sprintf("grep %s -A 15 -B 10", planHash)
	systemctl := "systemctl status rancher-system-agent.service -n 0"
	journaldCommand := []string{
		"chroot",
		"/host",
		"bash",
		"-c",
		fmt.Sprintf("%s | %s && %s", journald, grep, systemctl),
	}

	job, err := nodeUtils.CreateSystemdLogGathererJob(fmt.Sprintf("systemd-log-gatherer-%s", params.NodeName), params.NodeName, "rancher-system-agent", image, log, journaldCommand...)
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
		log.Error("failed to create job resource", zap.Error(err))
		return utils.ContainerLogs{}, err
	}

	pi, err := client.GetResourceInterface(ctx, middleware.Token(ctx), "cattle-system", params.Cluster, converter.K8sKindsToGVRs["pod"])
	if err != nil {
		return utils.ContainerLogs{}, err
	}

	// Wait for the jobs pod to roll out
	pod, err := nodeUtils.PollForPod(ctx, fmt.Sprintf("%s=%s", "job-name", jj.GetName()), pi, log)
	if err != nil {
		return utils.ContainerLogs{}, err
	}

	clientset, err := client.CreateClientSet(ctx, middleware.Token(ctx), params.Cluster)
	if err != nil {
		return utils.ContainerLogs{}, fmt.Errorf("failed to create clientset: %w", err)
	}

	// Read the logs from stdout
	logs, err := utils.GetPodLogs(ctx, clientset, pod, 500)
	if err != nil {
		return utils.ContainerLogs{}, fmt.Errorf("failed to get pod logs: %w", err)
	}

	return logs, nil
}

func getMachinePlanFields(ctx context.Context, nodeName string, client toolsClient, log *zap.Logger) (map[string]interface{}, string, error) {
	si, err := client.GetResourceInterface(ctx, middleware.Token(ctx), "fleet-default", "local", converter.K8sKindsToGVRs["secret"])
	if err != nil {
		log.Error("failed to get secret resource interface", zap.Error(err))
		return nil, "", err
	}

	machinePlanName := fmt.Sprintf("%s-machine-plan", nodeName)

	machinePlanUnstruct, err := si.Get(ctx, machinePlanName, metav1.GetOptions{})
	if err != nil {
		log.Error("failed to get machine plan resource interface", zap.Error(err))
		return nil, "", err
	}

	machinePlan := &corev1.Secret{}
	j, err := machinePlanUnstruct.MarshalJSON()
	if err != nil {
		log.Error("failed to marshal machine plan resource interface", zap.Error(err))
		return nil, "", err
	}

	err = json.Unmarshal(j, &machinePlan)
	if err != nil {
		log.Error("failed to unmarshal machine plan resource interface", zap.Error(err))
		return nil, "", err
	}

	if machinePlan.Data == nil || machinePlan.Data["plan"] == nil {
		return nil, "", fmt.Errorf("no plan found in machine plan resource")
	}

	currentPlanChecksum := plan.Checksum(machinePlan.Data["plan"])
	if currentPlanChecksum == "" {
		log.Error("failed to calculate checksum of plan resource", zap.Error(err))
	}

	out := map[string]string{
		"checksum": currentPlanChecksum,
	}
	for _, field := range []string{
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
		return nil, "", fmt.Errorf("failed to convert logs: %w", err)
	}

	return uFields, currentPlanChecksum, nil
}
