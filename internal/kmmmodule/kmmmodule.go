package kmmmodule

import (
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

        kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
        amdv1alpha1 "github.com/yevgeny-shnaidman/amd-gpu-operator/api/v1alpha1"

)

const (
        kubeletDevicePluginsVolumeName = "kubelet-device-plugins"
        kubeletDevicePluginsPath       = "/var/lib/kubelet/device-plugins"
        nodeVarLibFirmwarePath         = "/var/lib/firmware"
        gpuDriverModuleName            = "amdgpu"
        imageFirmwarePath              = "firmwareDir/updates"
        defaultDevicePluginImage       = "rocm/k8s-device-plugin"
        defaultDriversImage            = "image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amd_gpu_kmm_modules:$KERNEL_VERSION"
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


type KMMModuleAPI interface
{
	SetBuildConfigMapAsDesired(buildCM *v1.ConfigMap, devConfig *amdv1alpha1.DeviceConfig) error
        SetKMMModuleAsDesired(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) error
}

type kmmModule struct {
	client          client.Client
	scheme *runtime.Scheme
}

func NewKMMModule(client client.Client, scheme *runtime.Scheme) KMMModuleAPI {
        return &kmmModule{
                client:          client,
		scheme: scheme,
        }
}

func(km *kmmModule) SetBuildConfigMapAsDesired(buildCM *v1.ConfigMap, devConfig *amdv1alpha1.DeviceConfig) error {
        if buildCM.Data == nil {
                 buildCM.Data = make(map[string]string)
        }
        buildCM.Data["dockerfile"] = buildDockerfile
        return controllerutil.SetControllerReference(devConfig, buildCM, km.scheme)
}

func(km *kmmModule) SetKMMModuleAsDesired(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) error {
        setKMMModuleLoader(mod, devConfig)
        setKMMDevicePlugin(mod, devConfig)
        return controllerutil.SetControllerReference(devConfig, mod, km.scheme)
}

func setKMMModuleLoader(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) {
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

func setKMMDevicePlugin(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) {
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

func getDockerfileCMName(devConfig *amdv1alpha1.DeviceConfig) string {
        return "dockerfile-" + devConfig.Name
}
