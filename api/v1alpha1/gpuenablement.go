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

package v1alpha1

import (
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GPUEnablementSpec describes how the AMD GPU operator should enable AMD GPU device for customer's use.
type GPUEnablementSpec struct {
	// if the in-tree driver should be used instead of OOT drivers
	UseInTreeDrivers bool `json:"useInTreeDrivers,omitempty"`

	// defines configuration for kernel modules/drivers need by the operator
	DriversConfig kmmv1beta1.ModuleLoaderContainerSpec `json:"driversConfig"`

	// pull secrets used for pull/setting images used by operator
	ImageRepoSecret *v1.LocalObjectReference `json:"imageRepoSecret,omitempty"`

	// device plugin image
	DevicePluginImage string `json:"devicePluginImage"`

	// Selector describes on which nodes the GPU Operator should enable the GPU device.
	Selector map[string]string `json:"selector"`
}

// DaemonSetStatus contains the status for a daemonset deployed during
// reconciliation loop
type DeploymentStatus struct {
	// number of nodes that are targeted by the module selector
	NodesMatchingSelectorNumber int32 `json:"nodesMatchingSelectorNumber,omitempty"`
	// number of the pods that should be deployed for daemonset
	DesiredNumber int32 `json:"desiredNumber,omitempty"`
	// number of the actually deployed and running pods
	AvailableNumber int32 `json:"availableNumber,omitempty"`
}

// ModuleStatus defines the observed state of Module.
type GPUEnablementStatus struct {
	// DevicePlugin contains the status of the GPU Device Plugin deployment
	DevicePlugin DeploymentStatus `json:"devicePlugin,omitempty"`
	// Driver contains the status of the GPU Driver deployment
	Driver DeploymentStatus `json:"driver"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Namespaced,shortName=gpue
//+kubebuilder:subresource:status

// GPUConfig describes how to enable AMD GPU device
// +operator-sdk:csv:customresourcedefinitions:displayName="GPUEnablement"
type GPUEnablement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUEnablementSpec   `json:"spec,omitempty"`
	Status GPUEnablementStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ModuleList contains a list of Module
type GPUEnablementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUEnablement `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GPUEnablement{}, &GPUEnablementList{})
}
