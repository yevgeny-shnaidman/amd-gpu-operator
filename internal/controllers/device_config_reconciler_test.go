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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	amdv1alpha1 "github.com/yevgeny-shnaidman/amd-gpu-operator/api/v1alpha1"
	mock_client "github.com/yevgeny-shnaidman/amd-gpu-operator/internal/client"
	"github.com/yevgeny-shnaidman/amd-gpu-operator/internal/kmmmodule"
	"github.com/yevgeny-shnaidman/amd-gpu-operator/internal/nodemetrics"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	devConfigName      = "devConfigName"
	devConfigNamespace = "devConfigNamespace"
)

var _ = Describe("Reconcile", func() {
	var (
		mockHelper *MockdeviceConfigReconcilerHelperAPI
		dcr        *DeviceConfigReconciler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockHelper = NewMockdeviceConfigReconcilerHelperAPI(ctrl)
		dcr = &DeviceConfigReconciler{
			helper: mockHelper,
		}
	})

	ctx := context.Background()
	nn := types.NamespacedName{
		Name:      devConfigName,
		Namespace: devConfigNamespace,
	}
	req := ctrl.Request{NamespacedName: nn}

	DescribeTable("reconciler error flow", func(getDeviceError, setFinalizerError, buildConfigMapError, handleKMMModuleError, handleMetricsError bool) {
		devConfig := &amdv1alpha1.DeviceConfig{}
		if getDeviceError {
			mockHelper.EXPECT().getRequestedDeviceConfig(ctx, nn).Return(nil, fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().getRequestedDeviceConfig(ctx, req.NamespacedName).Return(devConfig, nil)
		if setFinalizerError {
			mockHelper.EXPECT().setFinalizer(ctx, devConfig).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().setFinalizer(ctx, devConfig).Return(nil)
		if buildConfigMapError {
			mockHelper.EXPECT().handleBuildConfigMap(ctx, devConfig).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().handleBuildConfigMap(ctx, devConfig).Return(nil)
		if handleKMMModuleError {
			mockHelper.EXPECT().handleKMMModule(ctx, devConfig).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().handleKMMModule(ctx, devConfig).Return(nil)
		if handleMetricsError {
			mockHelper.EXPECT().handleNodeMetrics(ctx, devConfig).Return(fmt.Errorf("some error"))
			goto executeTestFunction
		}
		mockHelper.EXPECT().handleNodeMetrics(ctx, devConfig).Return(nil)

	executeTestFunction:

		res, err := dcr.Reconcile(ctx, req)
		if getDeviceError || setFinalizerError || buildConfigMapError || handleKMMModuleError || handleMetricsError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))
		}
	},
		Entry("good flow, no requeue", false, false, false, false, false),
		Entry("getDeviceConfigFailed", true, false, false, false, false),
		Entry("setFinalizer failed", false, true, false, false, false),
		Entry("buildConfigMap failed", false, false, true, false, false),
		Entry("handleKMMModule failed", false, false, false, true, false),
		Entry("handleMetrics failed", false, false, false, false, true),
	)

	It("device config finalization", func() {
		devConfig := &amdv1alpha1.DeviceConfig{}
		devConfig.SetDeletionTimestamp(&metav1.Time{})

		mockHelper.EXPECT().getRequestedDeviceConfig(ctx, req.NamespacedName).Return(devConfig, nil)
		mockHelper.EXPECT().finalizeDeviceConfig(ctx, devConfig).Return(nil)

		res, err := dcr.Reconcile(ctx, req)

		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		mockHelper.EXPECT().getRequestedDeviceConfig(ctx, req.NamespacedName).Return(devConfig, nil)
		mockHelper.EXPECT().finalizeDeviceConfig(ctx, devConfig).Return(fmt.Errorf("some error"))

		res, err = dcr.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})
})

var _ = Describe("getLabelsPerModules", func() {
	var (
		kubeClient *mock_client.MockClient
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nil)
	})

	ctx := context.Background()
	nn := types.NamespacedName{
		Name:      devConfigName,
		Namespace: devConfigNamespace,
	}

	It("good flow", func() {
		expectedDevConfig := amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
		}
		kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Do(
			func(_ interface{}, _ interface{}, devConfig *amdv1alpha1.DeviceConfig, _ ...client.GetOption) {
				devConfig.Name = nn.Name
				devConfig.Namespace = nn.Namespace
			},
		)
		res, err := dcrh.getRequestedDeviceConfig(ctx, nn)
		Expect(err).ToNot(HaveOccurred())
		Expect(*res).To(Equal(expectedDevConfig))
	})

	It("error flow", func() {
		kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(fmt.Errorf("some error"))

		res, err := dcrh.getRequestedDeviceConfig(ctx, nn)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})
})

var _ = Describe("setFinalizer", func() {
	var (
		kubeClient *mock_client.MockClient
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nil)
	})

	ctx := context.Background()

	It("good flow", func() {
		devConfig := &amdv1alpha1.DeviceConfig{}

		kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil)

		err := dcrh.setFinalizer(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())

		err = dcrh.setFinalizer(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	It("error flow", func() {
		devConfig := &amdv1alpha1.DeviceConfig{}

		kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		err := dcrh.setFinalizer(ctx, devConfig)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("finalizeDeviceConfig", func() {
	var (
		kubeClient *mock_client.MockClient
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nil)
	})

	ctx := context.Background()
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
	}

	metricsNN := types.NamespacedName{
		Name:      devConfigName + "-node-metrics",
		Namespace: devConfigNamespace,
	}

	nn := types.NamespacedName{
		Name:      devConfigName,
		Namespace: devConfigNamespace,
	}

	It("failed to get Metrics daemonset", func() {
		kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(fmt.Errorf("some error"))

		err := dcrh.finalizeDeviceConfig(ctx, devConfig)
		Expect(err).To(HaveOccurred())
	})

	It("node metrics daemonset exists", func() {
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(nil),
			kubeClient.EXPECT().Delete(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig)
		Expect(err).To(BeNil())
	})

	It("failed to get KMM Module", func() {
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(fmt.Errorf("some error")),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig)
		Expect(err).To(HaveOccurred())
	})

	It("KMM module not found, removing finalizer", func() {
		expectedDevConfig := devConfig.DeepCopy()
		expectedDevConfig.SetFinalizers([]string{})
		controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "moduleName")),
			kubeClient.EXPECT().Patch(ctx, expectedDevConfig, gomock.Any()).Return(nil),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	It("KMM module found, deleting it", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      devConfigName,
				Namespace: devConfigNamespace,
			},
		}

		expectedDevConfig := devConfig.DeepCopy()
		expectedDevConfig.SetFinalizers([]string{})
		controllerutil.AddFinalizer(devConfig, deviceConfigFinalizer)

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, metricsNN, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "dsName")),
			kubeClient.EXPECT().Get(ctx, nn, gomock.Any()).Do(
				func(_ interface{}, _ interface{}, mod *kmmv1beta1.Module, _ ...client.GetOption) {
					mod.Name = nn.Name
					mod.Namespace = nn.Namespace
				},
			),
			kubeClient.EXPECT().Delete(ctx, &mod).Return(nil),
		)

		err := dcrh.finalizeDeviceConfig(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("handleKMMModule", func() {
	var (
		kubeClient *mock_client.MockClient
		kmmHelper  *kmmmodule.MockKMMModuleAPI
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		kmmHelper = kmmmodule.NewMockKMMModuleAPI(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, kmmHelper, nil, nil)
	})

	ctx := context.Background()
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
	}

	It("KMM Module does not exist", func() {
		newMod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      devConfig.Name,
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "whatever")),
			kmmHelper.EXPECT().SetKMMModuleAsDesired(newMod, devConfig).Return(nil),
			kubeClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.handleKMMModule(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	It("KMM Module exists", func() {
		existingMod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      devConfig.Name,
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
				func(_ interface{}, _ interface{}, mod *kmmv1beta1.Module, _ ...client.GetOption) {
					mod.Name = devConfig.Name
					mod.Namespace = devConfig.Namespace
				},
			),
			kmmHelper.EXPECT().SetKMMModuleAsDesired(existingMod, devConfig).Return(nil),
		)

		err := dcrh.handleKMMModule(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("handleBuildConfigMap", func() {
	var (
		kubeClient *mock_client.MockClient
		kmmHelper  *kmmmodule.MockKMMModuleAPI
		dcrh       deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		kmmHelper = kmmmodule.NewMockKMMModuleAPI(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, kmmHelper, nil, nil)
	})

	ctx := context.Background()
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
	}

	It("BuildConfig does not exist", func() {
		newBuildCM := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      "dockerfile-" + devConfig.Name,
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "whatever")),
			kmmHelper.EXPECT().SetBuildConfigMapAsDesired(newBuildCM, devConfig).Return(nil),
			kubeClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.handleBuildConfigMap(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	It("BuildConfig exists", func() {
		existingBuildCM := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: devConfig.Namespace,
				Name:      "dockerfile-" + devConfig.Name,
			},
		}
		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
				func(_ interface{}, _ interface{}, buildCM *v1.ConfigMap, _ ...client.GetOption) {
					buildCM.Name = "dockerfile-" + devConfig.Name
					buildCM.Namespace = devConfig.Namespace
				},
			),
			kmmHelper.EXPECT().SetBuildConfigMapAsDesired(existingBuildCM, devConfig).Return(nil),
		)

		err := dcrh.handleBuildConfigMap(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("handleNodeMetrics", func() {
	var (
		kubeClient        *mock_client.MockClient
		nodeMetricsHelper *nodemetrics.MockNodeMetrics
		dcrh              deviceConfigReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		nodeMetricsHelper = nodemetrics.NewMockNodeMetrics(ctrl)
		dcrh = newDeviceConfigReconcilerHelper(kubeClient, nil, nil, nodeMetricsHelper)
	})

	ctx := context.Background()
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devConfigName,
			Namespace: devConfigNamespace,
		},
	}

	It("NodeMetrics DaemonSet does not exist", func() {
		newDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-node-metrics"},
		}

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, "whatever")),
			nodeMetricsHelper.EXPECT().SetNodeMetricsAsDesired(newDS, devConfig).Return(nil),
			kubeClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dcrh.handleNodeMetrics(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	It("NodeMetrcis DaemonSet exists", func() {
		existingDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: devConfig.Namespace, Name: devConfig.Name + "-node-metrics"},
		}

		gomock.InOrder(
			kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
				func(_ interface{}, _ interface{}, ds *appsv1.DaemonSet, _ ...client.GetOption) {
					ds.Name = devConfig.Name + "-node-metrics"
					ds.Namespace = devConfig.Namespace
				},
			),
			nodeMetricsHelper.EXPECT().SetNodeMetricsAsDesired(existingDS, devConfig).Return(nil),
		)

		err := dcrh.handleNodeMetrics(ctx, devConfig)
		Expect(err).ToNot(HaveOccurred())
	})
})
