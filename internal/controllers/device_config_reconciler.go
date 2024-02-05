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
	"github.com/yevgeny-shnaidman/amd-gpu-operator/internal/kmmmodule"
	"github.com/yevgeny-shnaidman/amd-gpu-operator/internal/nodelabeller"
	"github.com/yevgeny-shnaidman/amd-gpu-operator/internal/nodemetrics"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DeviceConfigReconcilerName = "DriverAndPluginReconciler"
	deviceConfigFinalizer      = "amd.node.kubernetes.io/deviceconfig-finalizer"
)

// ModuleReconciler reconciles a Module object
type DeviceConfigReconciler struct {
	helper deviceConfigReconcilerHelperAPI
}

func NewDeviceConfigReconciler(
	client client.Client,
	kmmHandler kmmmodule.KMMModuleAPI,
	nlHandler nodelabeller.NodeLabeller,
	nmHandler nodemetrics.NodeMetrics) *DeviceConfigReconciler {
	helper := newDeviceConfigReconcilerHelper(client, kmmHandler, nlHandler, nmHandler)
	return &DeviceConfigReconciler{
		helper: helper,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeviceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&amdv1alpha1.DeviceConfig{}).
		Owns(&kmmv1beta1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		Named(DeviceConfigReconcilerName).
		Complete(r)
}

//+kubebuilder:rbac:groups=amd.io,resources=deviceconfigs,verbs=get;list;watch;create;patch;update
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch;create;patch;update;delete
//+kubebuilder:rbac:groups=amd.io,resources=deviceconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=create;delete;get;list;patch;watch;create
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch

func (r *DeviceConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	devConfig, err := r.helper.getRequestedDeviceConfig(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Module deleted")
			return ctrl.Result{}, nil
		}

		return res, fmt.Errorf("failed to get the requested %s KMMO CR: %w", req.NamespacedName, err)
	}

	if devConfig.GetDeletionTimestamp() != nil {
		// DeviceConfig is being deleted
		err = r.helper.finalizeDeviceConfig(ctx, devConfig)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to finalize DeviceConfig %s: %v", req.NamespacedName, err)
		}
		return ctrl.Result{}, nil
	}

	err = r.helper.setFinalizer(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to set finalizer for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start build configmap reconciliation")
	err = r.helper.handleBuildConfigMap(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to handle build ConfigMap for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	logger.Info("start KMM reconciliation")
	err = r.helper.handleKMMModule(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to handle KMM module for DeviceConfig %s: %v", req.NamespacedName, err)
	}

	/*
		logger.Info("start node labeller reconciliation")
		err = r.helper.handleNodeLabeller(ctx, devConfig)
		if err != nil {
			return res, fmt.Errorf("failed to handle node labeller for DeviceConfig %s: %v", req.NamespacedName, err)
		}
	*/

	logger.Info("start metrics reconciliation")
	err = r.helper.handleNodeMetrics(ctx, devConfig)
	if err != nil {
		return res, fmt.Errorf("failed to handle node metrics for DeviceConfig %s: %v", req.NamespacedName, err)
	}
	// [TODO] add status handling for DeviceConfig
	return res, nil
}

//go:generate mockgen -source=device_config_reconciler.go -package=controllers -destination=mock_device_config_reconciler.go deviceConfigReconcilerHelperAPI
type deviceConfigReconcilerHelperAPI interface {
	getRequestedDeviceConfig(ctx context.Context, namespacedName types.NamespacedName) (*amdv1alpha1.DeviceConfig, error)
	finalizeDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	setFinalizer(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	handleKMMModule(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	handleBuildConfigMap(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	handleNodeLabeller(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	handleNodeMetrics(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
}

type deviceConfigReconcilerHelper struct {
	client     client.Client
	kmmHandler kmmmodule.KMMModuleAPI
	nlHandler  nodelabeller.NodeLabeller
	nmHandler  nodemetrics.NodeMetrics
}

func newDeviceConfigReconcilerHelper(client client.Client,
	kmmHandler kmmmodule.KMMModuleAPI,
	nlHandler nodelabeller.NodeLabeller,
	nmHandler nodemetrics.NodeMetrics) deviceConfigReconcilerHelperAPI {
	return &deviceConfigReconcilerHelper{
		client:     client,
		kmmHandler: kmmHandler,
		nlHandler:  nlHandler,
		nmHandler:  nmHandler,
	}
}

func (dcrh *deviceConfigReconcilerHelper) getRequestedDeviceConfig(ctx context.Context, namespacedName types.NamespacedName) (*amdv1alpha1.DeviceConfig, error) {
	devConfig := amdv1alpha1.DeviceConfig{}

	if err := dcrh.client.Get(ctx, namespacedName, &devConfig); err != nil {
		return nil, fmt.Errorf("failed to get DeviceConfig %s: %v", namespacedName, err)
	}
	return &devConfig, nil
}

func (dcrh *deviceConfigReconcilerHelper) setFinalizer(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	if controllerutil.ContainsFinalizer(devConfig, deviceConfigFinalizer) {
		return nil
	}

	devConfigCopy := devConfig.DeepCopy()
	controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)
	return dcrh.client.Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
}

func (dcrh *deviceConfigReconcilerHelper) finalizeDeviceConfig(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	logger := log.FromContext(ctx)
	/*
		nlDS := appsv1.DaemonSet{}
		namespacedName := types.NamespacedName{
			Namespace: devConfig.Namespace,
			Name:      devConfig.Name + "node-labeller",
		}

		err := dcrh.client.Get(ctx, namespacedName, &nlDS)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return fmt.Errorf("failed to get nodelabeller daemonset %s: %v", namespacedName, err)
			}
		} else {
			logger.Info("deleting nodelabeller daemonset", "daemonset", namespacedName)
			return dcrh.client.Delete(ctx, &nlDS)
		}
	*/
	nmDS := appsv1.DaemonSet{}
	namespacedName := types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name + "node-metrics",
	}

	err := dcrh.client.Get(ctx, namespacedName, &nmDS)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get nodemetrics daemonset %s: %v", namespacedName, err)
		}
	} else {
		logger.Info("deleting nodemetrics daemonset", "daemonset", namespacedName)
		return dcrh.client.Delete(ctx, &nmDS)
	}

	mod := kmmv1beta1.Module{}
	namespacedName = types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      devConfig.Name,
	}
	err = dcrh.client.Get(ctx, namespacedName, &mod)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("module already deleted, removing finalizer", "module", namespacedName)
			devConfigCopy := devConfig.DeepCopy()
			controllerutil.RemoveFinalizer(devConfig, deviceConfigFinalizer)
			return dcrh.client.Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
		}
		return fmt.Errorf("failed to get the requested Module %s: %v", namespacedName, err)
	}
	logger.Info("deleting KMM Module", "module", namespacedName)
	return dcrh.client.Delete(ctx, &mod)
}

func (dcrh *deviceConfigReconcilerHelper) handleBuildConfigMap(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	buildDockerfileCM := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: devConfig.Namespace,
			Name:      getDockerfileCMName(devConfig),
		},
	}

	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, buildDockerfileCM, func() error {
		return dcrh.kmmHandler.SetBuildConfigMapAsDesired(buildDockerfileCM, devConfig)
	})

	if err == nil {
		logger.Info("Reconciled KMM build dockerfile ConfigMap", "name", buildDockerfileCM.Name, "result", opRes)
	}

	return err
}

func (dcrh *deviceConfigReconcilerHelper) handleKMMModule(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	kmmMod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: devConfig.Namespace,
			Name:      devConfig.Name,
		},
	}
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, kmmMod, func() error {
		return dcrh.kmmHandler.SetKMMModuleAsDesired(kmmMod, devConfig)
	})

	if err == nil {
		logger.Info("Reconciled KMM Module", "name", kmmMod.Name, "result", opRes)
	}

	return err

}

func (dcrh *deviceConfigReconcilerHelper) handleNodeLabeller(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "node-labeller"},
	}
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.nlHandler.SetNodeLabellerAsDesired(ds, devConfig)
	})

	if err == nil {
		logger.Info("Reconciled node labeller", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)
	}

	return err
}

func (dcrh *deviceConfigReconcilerHelper) handleNodeMetrics(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "node-metrics"},
	}
	logger := log.FromContext(ctx)
	opRes, err := controllerutil.CreateOrPatch(ctx, dcrh.client, ds, func() error {
		return dcrh.nmHandler.SetNodeMetricsAsDesired(ds, devConfig)
	})

	if err == nil {
		logger.Info("Reconciled node metrics", "namespace", ds.Namespace, "name", ds.Name, "result", opRes)
	}

	return err
}

func getDockerfileCMName(devConfig *amdv1alpha1.DeviceConfig) string {
	return "dockerfile-" + devConfig.Name
}
