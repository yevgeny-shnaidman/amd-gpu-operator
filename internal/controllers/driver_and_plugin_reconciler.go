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
	gpuev1alpha1 "github.com/yevgeny-shnaidman/amd-gpu-operator/api/v1alpha"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
)

// ModuleReconciler reconciles a Module object
type DriverAndPluginReconciler struct {
	client        client.Client
	eventRecorder record.EventRecorder
	scheme        *runtime.Scheme
}

func NewDriverAndPluginReconciler(
	client client.Client,
	eventRecorder record.EventRecorder,
	scheme *runtime.Scheme,
) *DriverAndPluginReconciler {
	return &DriverAndPluginReconciler{
		client:        client,
		eventRecorder: eventRecorder,
		scheme:        scheme,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DriverAndPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&kmmv1beta1.Module{}).
		Named(DriverAndPluginReconcilerName).
		Complete(r)
}

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch;
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch

func (r *DriverAndPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	gpue, err := r.getRequestedGPUEnablement(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Module deleted")
			return ctrl.Result{}, nil
		}

		return res, fmt.Errorf("failed to get the requested %s KMMO CR: %w", req.NamespacedName, err)
	}

	logger.Info("start KMM reconciliation")
	err = r.handleKMM(ctx, gpue)
	if err != nil {
		return res, fmt.Errorf("failed to handle KMM module for gpue %s: %v", req.NamespacedName, err)
	}

	logger.Info("start DevicePlugin reconciliation")
	err = r.handleDevicePlugin(ctx, gpue)
	if err != nil {
		return res, fmt.Errorf("failed to handle DevicePlugin for gpue %s: %v", req.NamespacedName, err)
	}

	// [TODO] add status handling for GPUE
	return res, nil
}

func (r *DriverAndPluginReconciler) getRequestedGPUEnablement(ctx context.Context, namespacedName types.NamespacedName) (*gpuev1alpha1.GPUEnablement, error) {
	gpue := gpuev1alpha1.GPUEnablement{}

	if err := r.client.Get(ctx, namespacedName, &gpue); err != nil {
		return nil, fmt.Errorf("failed to get GPUEnablement %s: %v", namespacedName, err)
	}
	return &gpue, nil
}

func (r *DriverAndPluginReconciler) handleKMM(ctx context.Context, gpue *gpuev1alpha1.GPUEnablement) error {
	kmmMod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gpue.Namespace,
			Name:      gpue.Name + "-kmm-module",
		},
	}
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, r.client, kmmMod, func() error {
		return r.setKMMAsDesired(ctx, kmmMod, gpue)
	})

	if err == nil {
		logger.Info("Reconciled KMM Module", "name", kmmMod.Name, "result", opRes)
	}

	return err

}
func (r *DriverAndPluginReconciler) setKMMAsDesired(ctx context.Context, mod *kmmv1beta1.Module, gpue *gpuev1alpha1.GPUEnablement) error {
	mod.Spec.ModuleLoader.Container = gpue.Spec.DriversConfig
	mod.Spec.ImageRepoSecret = gpue.Spec.ImageRepoSecret
	mod.Spec.Selector = gpue.Spec.Selector
	return controllerutil.SetControllerReference(gpue, mod, r.scheme)
}

func (r *DriverAndPluginReconciler) handleDevicePlugin(ctx context.Context, gpue *gpuev1alpha1.GPUEnablement) error {
	devicePluginDS := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gpue.Namespace,
			Name:      gpue.Name + "device-plugin",
		},
	}
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, r.client, devicePluginDS, func() error {
		return r.setDevicePluginAsDesired(ctx, devicePluginDS, gpue)
	})
	if err == nil {
		logger.Info("Reconciled Device Plugin daemonset", "name", devicePluginDS.Name, "result", opRes)
	}
	return err
}

func (r *DriverAndPluginReconciler) setDevicePluginAsDesired(ctx context.Context, ds *appsv1.DaemonSet, gpue *gpuev1alpha1.GPUEnablement) error {
	return controllerutil.SetControllerReference(gpue, ds, r.scheme)
}
