package kmmmodule

import (
	_ "embed"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	defaultDriversImageTemplate    = "image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amd_gpu_kmm_modules:%s-$KERNEL_VERSION"
	defaultDriversVersion          = "el9-6.0"
)

var (
	//go:embed dockerfiles/driversDockerfile.txt
	buildDockerfile string
)

//go:generate mockgen -source=kmmmodule.go -package=kmmmodule -destination=mock_kmmmodule.go KMMModuleAPI
type KMMModuleAPI interface {
	SetBuildConfigMapAsDesired(buildCM *v1.ConfigMap, devConfig *amdv1alpha1.DeviceConfig) error
	SetKMMModuleAsDesired(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) error
}

type kmmModule struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewKMMModule(client client.Client, scheme *runtime.Scheme) KMMModuleAPI {
	return &kmmModule{
		client: client,
		scheme: scheme,
	}
}

func (km *kmmModule) SetBuildConfigMapAsDesired(buildCM *v1.ConfigMap, devConfig *amdv1alpha1.DeviceConfig) error {
	if buildCM.Data == nil {
		buildCM.Data = make(map[string]string)
	}

	buildCM.Data["dockerfile"] = buildDockerfile
	return controllerutil.SetControllerReference(devConfig, buildCM, km.scheme)
}

func (km *kmmModule) SetKMMModuleAsDesired(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) error {
	err := setKMMModuleLoader(mod, devConfig)
	if err != nil {
		return fmt.Errorf("failed to set KMM Module: %v", err)
	}
	setKMMDevicePlugin(mod, devConfig)
	return controllerutil.SetControllerReference(devConfig, mod, km.scheme)
}

func setKMMModuleLoader(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) error {
	driversVersion := devConfig.Spec.DriversVersion
	if driversVersion == "" {
		driversVersion = defaultDriversVersion
	}

	driversImage := devConfig.Spec.DriversImage
	if driversImage == "" {
		driversImage = fmt.Sprintf(defaultDriversImageTemplate, driversVersion)
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
					BuildArgs: []kmmv1beta1.BuildArg{
						{
							Name:  "DRIVERS_VERSION",
							Value: driversVersion,
						},
					},
				},
			},
		},
	}
	mod.Spec.ModuleLoader.ServiceAccountName = "amd-gpu-operator-kmm-module-loader"
	mod.Spec.ImageRepoSecret = devConfig.Spec.ImageRepoSecret
	mod.Spec.Selector = getNodeSelector(devConfig)
	return nil
}

func setKMMDevicePlugin(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) {
	devicePluginImage := devConfig.Spec.DevicePluginImage
	if devicePluginImage == "" {
		devicePluginImage = defaultDevicePluginImage
	}
	hostPathDirectory := v1.HostPathDirectory
	mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{
		ServiceAccountName: "amd-gpu-operator-kmm-device-plugin",
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

func getNodeSelector(devConfig *amdv1alpha1.DeviceConfig) map[string]string {
	if devConfig.Spec.Selector != nil {
		return devConfig.Spec.Selector
	}

	ns := make(map[string]string, 0)
	ns[fmt.Sprintf("feature.node.kubernetes.io/pci-%s.present", amdv1alpha1.AMDPCIVendorID)] = "true"
	return ns
}
