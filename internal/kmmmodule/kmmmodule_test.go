package kmmmodule

import (
	"fmt"
	//"gopkg.in/yaml.v3"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	amdv1alpha1 "github.com/yevgeny-shnaidman/amd-gpu-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("setKMMModuleLoader", func() {
	It("KMM module creation - default input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}
		input := amdv1alpha1.DeviceConfig{}

		expectedYAMLFile, err := os.ReadFile("testdata/module_loader_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())
		fmt.Printf("<%s>\n", expectedMod.Name)
		fmt.Printf("<%s>\n", expectedMod.Spec.ModuleLoader.Container.Modprobe.ModuleName)
		Expect(len(expectedMod.Spec.ModuleLoader.Container.KernelMappings)).To(Equal(1))

		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].ContainerImage = fmt.Sprintf(defaultDriversImageTemplate, defaultDriversVersion)
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.DockerfileConfigMap.Name = "dockerfile-" + input.Name
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.BuildArgs[0].Value = defaultDriversVersion
		expectedMod.Spec.Selector = map[string]string{"feature.node.kubernetes.io/pci-1002.present": "true"}

		err = setKMMModuleLoader(&mod, &input)

		Expect(err).To(BeNil())
		Expect(mod).To(Equal(expectedMod))
	})

	It("KMM module creation - user input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}
		input := amdv1alpha1.DeviceConfig{
			Spec: amdv1alpha1.DeviceConfigSpec{
				UseInTreeDrivers: false,
				DriversImage:     "some driver image",
				DriversVersion:   "some driver version",
				Selector:         map[string]string{"some label": "some label value"},
				ImageRepoSecret:  &v1.LocalObjectReference{Name: "image repo secret name"},
			},
		}

		expectedYAMLFile, err := os.ReadFile("testdata/module_loader_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())
		fmt.Printf("<%s>\n", expectedMod.Name)
		fmt.Printf("<%s>\n", expectedMod.Spec.ModuleLoader.Container.Modprobe.ModuleName)
		Expect(len(expectedMod.Spec.ModuleLoader.Container.KernelMappings)).To(Equal(1))

		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].ContainerImage = "some driver image"
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.DockerfileConfigMap.Name = "dockerfile-" + input.Name
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.BuildArgs[0].Value = "some driver version"
		expectedMod.Spec.Selector = map[string]string{"some label": "some label value"}
		expectedMod.Spec.ImageRepoSecret = &v1.LocalObjectReference{Name: "image repo secret name"}

		err = setKMMModuleLoader(&mod, &input)

		Expect(err).To(BeNil())
		Expect(mod).To(Equal(expectedMod))
	})
})

var _ = Describe("setKMMDevicePlugin", func() {
	It("KMM module creation - default input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}

		input := amdv1alpha1.DeviceConfig{}

		expectedYAMLFile, err := os.ReadFile("testdata/device_plugin_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())

		setKMMDevicePlugin(&mod, &input)

		Expect(mod).To(Equal(expectedMod))
	})

	It("KMM module creation - user input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}

		input := amdv1alpha1.DeviceConfig{
			Spec: amdv1alpha1.DeviceConfigSpec{
				DevicePluginImage: "some device plugin image",
			},
		}

		expectedYAMLFile, err := os.ReadFile("testdata/device_plugin_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())

		expectedMod.Spec.DevicePlugin.Container.Image = "some device plugin image"

		setKMMDevicePlugin(&mod, &input)

		Expect(mod).To(Equal(expectedMod))
	})
})
