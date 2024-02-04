package nodelabeller

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

type NodeLabeller interface {
	SetNodeLabellerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type nodeLabeller struct {
	scheme *runtime.Scheme
}

func NewNodeLabeller(scheme *runtime.Scheme) NodeLabeller {
	return &nodeLabeller{
		scheme: scheme,
	}
}

func (nl *nodeLabeller) SetNodeLabellerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}
	containerVolumeMounts := []v1.VolumeMount{
		{
			Name:      "dev-volume",
			MountPath: "/dev",
		},
		{
			Name:      "sys-volume",
			MountPath: "/sys",
		},
	}

	hostPathDirectory := v1.HostPathDirectory

	volumes := []v1.Volume{
		{
			Name: "dev-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/dev",
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

	matchLabels := map[string]string{"daemonset-name": devConfig.Name}
	nodeSelector := map[string]string{labels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name): ""}
	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: matchLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: matchLabels,
				//Finalizers: []string{constants.NodeLabelerFinalizer},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Args:    []string{"-vram", "-cu-count", "-simd-count", "-device-id", "-family"},
						Command: []string{"./k8s-node-labeller"},
						Env: []v1.EnvVar{
							{
								Name: "DS_NODE_NAME",
								ValueFrom: &v1.EnvVarSource{
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: "spec.nodeName",
									},
								},
							},
						},
						Name:            "node-labeller-container",
						WorkingDir:      "/root",
						Image:           "rocm/k8s-device-plugin:labeller-latest",
						ImagePullPolicy: v1.PullAlways,
						SecurityContext: &v1.SecurityContext{Privileged: pointer.Bool(true)},
						VolumeMounts:    containerVolumeMounts,
					},
				},
				PriorityClassName:  "system-node-critical",
				NodeSelector:       nodeSelector,
				ServiceAccountName: "amd-gpu-operator-node-labeller",
				Volumes:            volumes,
			},
		},
	}

	return controllerutil.SetControllerReference(devConfig, ds, nl.scheme)
}
