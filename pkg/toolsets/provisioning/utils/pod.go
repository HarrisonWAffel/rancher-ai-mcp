package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	v2 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

func PollForPod(ctx context.Context, labelSelector string, podResourceInterface dynamic.ResourceInterface, log *zap.Logger) (v2.Pod, error) {
	var pods *unstructured.UnstructuredList
	err := wait.PollUntilContextTimeout(ctx, time.Second*1, time.Second*20, true, func(ctx context.Context) (done bool, err error) {
		pods, err = podResourceInterface.List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			log.Error("failed to list pods", zap.Error(err))
			return false, nil
		}
		log.Info("got pod list", zap.Any("pods", pods.Items))
		if len(pods.Items) == 0 {
			return false, nil
		}

		pod := &v2.Pod{}
		j, err := pods.Items[0].MarshalJSON()
		if err != nil {
			log.Error("failed to marshal pod", zap.Error(err))
			return false, nil
		}

		err = json.Unmarshal(j, &pod)
		if err != nil {
			log.Error("failed to unmarshal pod", zap.Error(err))
			return false, nil
		}

		return pod.Status.Phase == v2.PodSucceeded || pod.Status.Phase == v2.PodFailed, nil
	})
	if err != nil {
		return v2.Pod{}, err
	}
	log.Info("finished polling for pod", zap.String("selector", labelSelector))
	if pods.Items == nil || len(pods.Items) == 0 {
		log.Error("failed to find pod", zap.String("selector", labelSelector))
		return v2.Pod{}, fmt.Errorf("failed to find pod")
	}

	b, err := pods.Items[0].MarshalJSON()
	if err != nil {
		log.Error("failed to marshal pod", zap.String("selector", labelSelector), zap.Error(err))
		return v2.Pod{}, err
	}
	var pod v2.Pod
	if err := json.Unmarshal(b, &pod); err != nil {
		log.Error("failed to unmarshal pod", zap.String("selector", labelSelector), zap.Error(err))
		return v2.Pod{}, err
	}
	log.Info("finished polling for pod, found it", zap.String("selector", labelSelector))
	return pod, nil
}
