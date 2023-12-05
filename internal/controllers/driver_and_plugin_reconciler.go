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

package controllers

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	amdv1alpha1 "github.com/yevgeny-shnaidman/amd-gpu-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DriverAndPluginReconcilerName  = "DriverAndPluginReconciler"
	kubeletDevicePluginsVolumeName = "kubelet-device-plugins"
	kubeletDevicePluginsPath       = "/var/lib/kubelet/device-plugins"
	nodeVarLibFirmwarePath         = "/var/lib/firmware"
	gpuDriverModuleName            = "amdgpu"
	imageFirmwarePath              = "firmwareDir/updates"
	defaultDevicePluginImage       = "rocm/k8s-device-plugin"
	defaultDriversImage            = "image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amd_gpu_kmm_modules:$KERNEL_VERSION"
	deviceConfigFinalizer          = "amd.node.kubernetes.io/deviceconfig-finalizer"
)

const buildDockerfile = `
FROM quay.io/yshnaidm/amd_gpu_sources:el9 as sources
ARG DTK_AUTO
FROM ${DTK_AUTO} as builder
ARG KERNEL_VERSION
COPY --from=sources /amdgpu-drivers-source /amdgpu-drivers-source
WORKDIR /amdgpu-drivers-source
RUN ./amd/dkms/pre-build.sh ${KERNEL_VERSION}
RUN make TTM_NAME=amdttm SCHED_NAME=amd-sched -C /usr/src/kernels/${KERNEL_VERSION} M=/amdgpu-drivers-source
RUN ./amd/dkms/post-build.sh ${KERNEL_VERSION}

RUN mkdir -p /lib/modules/${KERNEL_VERSION}/amd/amdgpu
RUN mkdir -p /lib/modules/${KERNEL_VERSION}/amd/amdkcl
RUN mkdir -p /lib/modules/${KERNEL_VERSION}/amd/amdxcp
RUN mkdir -p /lib/modules/${KERNEL_VERSION}/scheduler
RUN mkdir -p /lib/modules/${KERNEL_VERSION}/ttm
RUN rm -f /lib/modules/${KERNEL_VERSION}/kernel/drivers/gpu/drm/amd/amdgpu/amdgpu.ko.xz

RUN cp /amdgpu-drivers-source/amd/amdgpu/amdgpu.ko /lib/modules/${KERNEL_VERSION}/amd/amdgpu/amdgpu.ko
RUN cp /amdgpu-drivers-source/amd/amdkcl/amdkcl.ko /lib/modules/${KERNEL_VERSION}/amd/amdkcl/amdkcl.ko
RUN cp /amdgpu-drivers-source/amd/amdxcp/amdxcp.ko /lib/modules/${KERNEL_VERSION}/amd/amdxcp/amdxcp.ko
RUN cp /amdgpu-drivers-source/scheduler/amd-sched.ko /lib/modules/${KERNEL_VERSION}/scheduler/amd-sched.ko
RUN cp /amdgpu-drivers-source/ttm/amdttm.ko /lib/modules/${KERNEL_VERSION}/ttm/amdttm.ko
RUN cp /amdgpu-drivers-source/amddrm_buddy.ko /lib/modules/${KERNEL_VERSION}/amddrm_buddy.ko
RUN cp /amdgpu-drivers-source/amddrm_ttm_helper.ko /lib/modules/${KERNEL_VERSION}/amddrm_ttm_helper.ko

RUN depmod ${KERNEL_VERSION}

RUN mkdir /modules_files
RUN cp /lib/modules/${KERNEL_VERSION}/modules.* /modules_files

FROM registry.redhat.io/ubi9/ubi-minimal

ARG KERNEL_VERSION

RUN ["microdnf", "install", "-y", "kmod"]

COPY --from=builder /amdgpu-drivers-source/amd/amdgpu/amdgpu.ko /opt/lib/modules/${KERNEL_VERSION}/amd/amdgpu/amdgpu.ko
COPY --from=builder /amdgpu-drivers-source/amd/amdkcl/amdkcl.ko /opt/lib/modules/${KERNEL_VERSION}/amd/amdkcl/amdkcl.ko
COPY --from=builder /amdgpu-drivers-source/amd/amdxcp/amdxcp.ko /opt/lib/modules/${KERNEL_VERSION}/amd/amdxcp/amdxcp.ko
COPY --from=builder /amdgpu-drivers-source/scheduler/amd-sched.ko /opt/lib/modules/${KERNEL_VERSION}/scheduler/amd-sched.ko
COPY --from=builder /amdgpu-drivers-source/ttm/amdttm.ko /opt/lib/modules/${KERNEL_VERSION}/ttm/amdttm.ko
COPY --from=builder /amdgpu-drivers-source/amddrm_buddy.ko /opt/lib/modules/${KERNEL_VERSION}/amddrm_buddy.ko
COPY --from=builder /amdgpu-drivers-source/amddrm_ttm_helper.ko /opt/lib/modules/${KERNEL_VERSION}/amddrm_ttm_helper.ko
COPY --from=builder /modules_files /opt/lib/modules/${KERNEL_VERSION}/
RUN ln -s /lib/modules/${KERNEL_VERSION}/kernel /opt/lib/modules/${KERNEL_VERSION}/kernel

# copy firmware
RUN mkdir -p /firmwareDir/updates/amdgpu
COPY --from=sources /firmwareDir/updates/amdgpu /firmwareDir/updates/amdgpu
`

// ModuleReconciler reconciles a Module object
type DriverAndPluginReconciler struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewDriverAndPluginReconciler(
	client client.Client,
	scheme *runtime.Scheme,
) *DriverAndPluginReconciler {
	return &DriverAndPluginReconciler{
		client: client,
		scheme: scheme,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DriverAndPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&amdv1alpha1.DeviceConfig{}).
		Owns(&kmmv1beta1.Module{}).
		Named(DriverAndPluginReconcilerName).
		Complete(r)
}

//+kubebuilder:rbac:groups=amd.io,resources=deviceconfigs,verbs=get;list;watch;create;patch;update
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch;create;patch;update;delete
//+kubebuilder:rbac:groups=amd.io,resources=deviceconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=create;delete;get;list;patch;watch;create

func (r *DriverAndPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	devConfig, err := r.getRequestedDeviceConfig(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Module deleted")
			return ctrl.Result{}, nil
		}

		return res, fmt.Errorf("failed to get the requested %s KMMO CR: %w", req.NamespacedName, err)
	}

	if devConfig.GetDeletionTimestamp() != nil {
		// DeviceConfig is being deleted
		err = r.finalizeDeviceConfig(ctx, devConfig)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize DeviceConfig %s: %v", req.NamespacedName, err)
		}
		return ctrl.Result{}, nil
	}

	err = r.setFinalizer(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to set finalizer for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start KMM reconciliation")
	err = r.handleKMM(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to handle KMM module for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	// [TODO] add status handling for DeviceConfig
	return res, nil
}

func (r *DriverAndPluginReconciler) getRequestedDeviceConfig(ctx context.Context, namespacedName types.NamespacedName) (*amdv1alpha1.DeviceConfig, error) {
	devConfig := amdv1alpha1.DeviceConfig{}

	if err := r.client.Get(ctx, namespacedName, &devConfig); err != nil {
		return nil, fmt.Errorf("failed to get DeviceConfig %s: %v", namespacedName, err)
	}
	return &devConfig, nil
}

func (r *DriverAndPluginReconciler) setFinalizer(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	if controllerutil.ContainsFinalizer(devConfig, deviceConfigFinalizer) {
		return nil
	}

	devConfigCopy := devConfig.DeepCopy()
	controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)
	return r.client.Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
}

func (r *DriverAndPluginReconciler) finalizeDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	mod := kmmv1beta1.Module{}

	logger := log.FromContext(ctx)
	namespacedName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name,
	}
	err := r.client.Get(ctx, namespacedName, &mod)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("module %s already deleted, removing finalizer", namespacedName)
			devConfigCopy := devConfig.DeepCopy()
			controllerutil.RemoveFinalizer(devConfig, deviceConfigFinalizer)
			return r.client.Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
		}
		return fmt.Errorf("failed to get the requested Module %s: %v", namespacedName, err)
	}
	logger.Info("deleting KMM Module %s", namespacedName)
	return r.client.Delete(ctx, &mod)
}

func (r *DriverAndPluginReconciler) handleKMM(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	err := r.prepareBuildConfigMap(ctx, devConfig)
	if err != nil {
		return fmt.Errorf("failed to prepare dockerfile config map for KMM: %v", err)
	}

	kmmMod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: devConfig.Namespace,
			Name:      devConfig.Name,
		},
	}
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, r.client, kmmMod, func() error {
		return r.setKMMAsDesired(ctx, kmmMod, devConfig)
	})

	if err == nil {
		logger.Info("Reconciled KMM Module", "name", kmmMod.Name, "result", opRes)
	}

	return err

}
func (r *DriverAndPluginReconciler) setKMMAsDesired(ctx context.Context, mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) error {
	r.setKMMModuleLoader(ctx, mod, devConfig)
	r.setKMMDevicePlugin(ctx, mod, devConfig)
	return controllerutil.SetControllerReference(devConfig, mod, r.scheme)
}

func (r *DriverAndPluginReconciler) setKMMModuleLoader(ctx context.Context, mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) {
	driversImage := devConfig.Spec.DriversImage
	if driversImage == "" {
		driversImage = defaultDriversImage
	}
	mod.Spec.ModuleLoader.Container = kmmv1beta1.ModuleLoaderContainerSpec{
		Modprobe: kmmv1beta1.ModprobeSpec{
			ModuleName:   gpuDriverModuleName,
			FirmwarePath: imageFirmwarePath,
		},
		KernelMappings: []kmmv1beta1.KernelMapping{
			{
				Regexp:               "^.+$",
				ContainerImage:       driversImage,
				InTreeModuleToRemove: gpuDriverModuleName,
				Build: &kmmv1beta1.Build{
					DockerfileConfigMap: &v1.LocalObjectReference{
						Name: getDockerfileCMName(devConfig),
					},
				},
			},
		},
	}
	mod.Spec.ImageRepoSecret = devConfig.Spec.ImageRepoSecret
	mod.Spec.Selector = devConfig.Spec.Selector
}

func (r *DriverAndPluginReconciler) setKMMDevicePlugin(ctx context.Context, mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) {
	devicePluginImage := devConfig.Spec.DevicePluginImage
	if devicePluginImage == "" {
		devicePluginImage = defaultDevicePluginImage
	}
	hostPathDirectory := v1.HostPathDirectory
	mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{
		Container: kmmv1beta1.DevicePluginContainerSpec{
			Image: devicePluginImage,
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "sys",
					MountPath: "/sys",
				},
			},
		},
		Volumes: []v1.Volume{
			{
				Name: "sys",
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: "/sys",
						Type: &hostPathDirectory,
					},
				},
			},
		},
	}
}

func (r *DriverAndPluginReconciler) prepareBuildConfigMap(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	buildDockerfileCM := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: devConfig.Namespace,
			Name:      getDockerfileCMName(devConfig),
		},
	}

	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, r.client, buildDockerfileCM, func() error {
		if buildDockerfileCM.Data == nil {
			buildDockerfileCM.Data = make(map[string]string)
		}
		buildDockerfileCM.Data["dockerfile"] = buildDockerfile
		return controllerutil.SetControllerReference(devConfig, buildDockerfileCM, r.scheme)
	})

	if err == nil {
		logger.Info("Reconciled KMM build dockerfile ConfigMap", "name", buildDockerfileCM.Name, "result", opRes)
	}

	return err
}

func getKMMModuleReadyNodeLabel(namespace, moduleName string) string {
	return fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", namespace, moduleName)
}

func getDockerfileCMName(devConfig *amdv1alpha1.DeviceConfig) string {
	return "dockerfile-" + devConfig.Name
}

func getDevicePluginName(devConfig *amdv1alpha1.DeviceConfig) string {
	return devConfig.Name + "-device-plugin"
}
