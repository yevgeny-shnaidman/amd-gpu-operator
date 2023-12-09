package kmmmodule

import (
	_ "embed"

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

var (
        //go:embed dockerfiles/driversDockerfile.txt
	buildDockerfile string
)

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
