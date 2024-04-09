/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/


package nodemetrics

import (
	"fmt"

	"github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"
	amdv1alpha1 "github.com/yevgeny-shnaidman/amd-gpu-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	metricsPortName       = "node-metrics"
	metricsPort           = 9110
	metricsServiceAccount = "amd-gpu-operator-node-metrics"
	metricsImage          = "quay.io/yshnaidm/node-exporter:latest"
)

//go:generate mockgen -source=nodemetrics.go -package=nodemetrics -destination=mock_nodemetrics.go NodeMetrics
type NodeMetrics interface {
	SetNodeMetricsAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type nodeMetrics struct {
	scheme *runtime.Scheme
}

func NewNodeMetrcis(scheme *runtime.Scheme) NodeMetrics {
	return &nodeMetrics{
		scheme: scheme,
	}
}

func (nm *nodeMetrics) SetNodeMetricsAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}

	volumes, volumesMounts := getVolumesAndMount()
	ports := getPorts()

	matchLabels := map[string]string{
		"app.kubernetes.io/component": "amd-gpu",
		"app.kubernetes.io/name":      "amd-gpu",
		"app.kubernetes.io/part-of":   "amd-gpu",
		"app.kubernetes.io/role":      "amd-gpu-metrics",
	}
	nodeSelector := map[string]string{labels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name): ""}
	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: matchLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: matchLabels,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:            "node-metrics-container",
						Image:           metricsImage,
						ImagePullPolicy: v1.PullAlways,
						SecurityContext: &v1.SecurityContext{
							Privileged: pointer.Bool(true),
							RunAsUser:  pointer.Int64(0),
						},
						VolumeMounts: volumesMounts,
						Ports:        ports,
					},
				},
				NodeSelector:       nodeSelector,
				ServiceAccountName: metricsServiceAccount,
				Volumes:            volumes,
			},
		},
	}

	return controllerutil.SetControllerReference(devConfig, ds, nm.scheme)
}

func getVolumesAndMount() ([]v1.Volume, []v1.VolumeMount) {
	containerVolumeMounts := []v1.VolumeMount{
		{
			Name:      "root-volume",
			MountPath: "/host/root",
		},
		{
			Name:      "sys-volume",
			MountPath: "/host/sys",
		},
	}

	hostPathDirectory := v1.HostPathDirectory
	volumes := []v1.Volume{
		{
			Name: "root-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: "sys-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/sys",
					Type: &hostPathDirectory,
				},
			},
		},
	}

	return volumes, containerVolumeMounts
}

func getPorts() []v1.ContainerPort {
	return []v1.ContainerPort{
		{
			Name:          metricsPortName,
			HostPort:      metricsPort,
			ContainerPort: metricsPort,
			Protocol:      v1.ProtocolTCP,
		},
	}
}
