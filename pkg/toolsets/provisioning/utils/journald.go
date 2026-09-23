package utils

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const nodeHostnameLabel = "kubernetes.io/hostname"

func CreateSystemdLogGathererJob(jobName, nodeName, unit string, log *zap.Logger) (*unstructured.Unstructured, error) {
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName,
			// as this job retrieves sensitive information it should only ever be
			// deployed into privileged namespaces.
			Namespace: "cattle-system",
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32(1)),
			Completions:             new(int32(1)),
			TTLSecondsAfterFinished: new(int32(60)),
			ActiveDeadlineSeconds:   new(int64(10)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name: "journal-reader",
							// TODO: Get a real image together for this
							Image: "debian:stable-slim",
							Command: []string{
								"/bin/sh",
								"-c",
								fmt.Sprintf("apt-get update >/dev/null && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd >/dev/null && rm -rf /var/lib/apt/lists/* && journalctl -u %s.service --no-pager -n 100 -o cat", unit),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "journal-logs",
									MountPath: "/var/log/journal",
									ReadOnly:  true,
								},
								{
									Name:      "machine-id",
									MountPath: "/etc/machine-id",
									ReadOnly:  true,
								},
							},
						},
					},
					NodeSelector: map[string]string{
						nodeHostnameLabel: nodeName,
					},
					Volumes: []corev1.Volume{
						{
							Name: "journal-logs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/log/journal",
									Type: new(corev1.HostPathDirectoryOrCreate),
								},
							},
						},
						{
							Name: "machine-id",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/machine-id",
									Type: new(corev1.HostPathFile),
								},
							},
						},
					},
				},
			},
		},
	}

	objBytes, err := json.Marshal(job)
	if err != nil {
		log.Error("failed to marshal resource", zap.Error(err))
		return nil, fmt.Errorf("failed to marshal resource: %w", err)
	}

	unstructuredObj := &unstructured.Unstructured{}
	if err := json.Unmarshal(objBytes, unstructuredObj); err != nil {
		log.Error("failed to create unstructured resource", zap.Error(err))
		return nil, fmt.Errorf("failed to create unstructured object: %w", err)
	}

	return unstructuredObj, nil
}
