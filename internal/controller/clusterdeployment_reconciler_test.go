/*
Copyright 2024.

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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sakhoury/siteconfig/api/v1alpha1"
	"github.com/sakhoury/siteconfig/internal/controller/conditions"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Reconcile", func() {
	var (
		c                client.Client
		r                *ClusterDeploymentReconciler
		ctx              = context.Background()
		clusterName      = "test-cluster"
		clusterNamespace = "test-namespace"
		siteConfig       *v1alpha1.SiteConfig
	)
	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.SiteConfig{}).
			Build()
		testLogger := ctrl.Log.WithName("ClusterDeploymentReconciler")
		r = &ClusterDeploymentReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    testLogger,
		}

		siteConfig = &v1alpha1.SiteConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterName,
				Namespace:  clusterNamespace,
				Finalizers: []string{siteConfigFinalizer},
			},
			Spec: v1alpha1.SiteConfigSpec{
				ClusterName:            clusterName,
				PullSecretRef:          &corev1.LocalObjectReference{Name: "pull-secret"},
				ClusterImageSetNameRef: "testimage:foobar",
				SSHPublicKey:           "test-ssh",
				BaseDomain:             "abcd",
				ClusterType:            v1alpha1.ClusterTypeSNO,
				TemplateRefs: []v1alpha1.TemplateRef{
					{Name: "test-cluster-template", Namespace: "default"}},
				Nodes: []v1alpha1.NodeSpec{{
					BmcAddress:         "1:2:3:4",
					BmcCredentialsName: v1alpha1.BmcCredentialsName{Name: "bmc"},
					TemplateRefs: []v1alpha1.TemplateRef{
						{Name: "test-node-template", Namespace: "default"}}}}},
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterNamespace,
			},
		}
		Expect(c.Create(ctx, ns)).To(Succeed())
		Expect(c.Create(ctx, siteConfig)).To(Succeed())
	})

	It("doesn't error for a missing ClusterDeployment", func() {
		key := types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("doesn't reconcile a ClusterDeployment that is not owned by SiteConfig", func() {
		key := types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}
		clusterDeployment := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: clusterNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "foobar.group.io",
						Kind:       "foobar",
						Name:       clusterName,
					},
				},
			},
			Status: hivev1.ClusterDeploymentStatus{
				Conditions: []hivev1.ClusterDeploymentCondition{
					{
						Type:    hivev1.ClusterInstallFailedClusterDeploymentCondition,
						Status:  corev1.ConditionStatus(metav1.ConditionFalse),
						Reason:  "InstallationNotFailed",
						Message: "The installation has not failed"},
				},
			},
		}
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		// Fetch SiteConfig and verify that the status is unchanged
		sc := &v1alpha1.SiteConfig{}
		Expect(c.Get(ctx, key, sc)).To(Succeed())
		Expect(sc.Status).To(Equal(siteConfig.Status))
	})

	It("tests that ClusterDeploymentReconciler initializes SiteConfig ClusterDeployment correctly", func() {
		key := types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}
		clusterDeployment := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: clusterNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "metaclusterinstall.openshift.io/v1alpha1",
						Kind:       v1alpha1.SiteConfigKind,
						Name:       clusterName,
					},
				},
			},
		}
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(doNotRequeue()))

		expectedConditions := []hivev1.ClusterDeploymentCondition{
			{
				Type:    hivev1.ClusterInstallRequirementsMetClusterDeploymentCondition,
				Status:  corev1.ConditionStatus(metav1.ConditionUnknown),
				Message: "Unknown",
			},
			{
				Type:    hivev1.ClusterInstallStoppedClusterDeploymentCondition,
				Status:  corev1.ConditionStatus(metav1.ConditionUnknown),
				Message: "Unknown",
			},
			{
				Type:    hivev1.ClusterInstallCompletedClusterDeploymentCondition,
				Status:  corev1.ConditionStatus(metav1.ConditionUnknown),
				Message: "Unknown",
			},
			{
				Type:    hivev1.ClusterInstallFailedClusterDeploymentCondition,
				Status:  corev1.ConditionStatus(metav1.ConditionUnknown),
				Message: "Unknown",
			},
		}

		sc := &v1alpha1.SiteConfig{}
		Expect(c.Get(ctx, key, sc)).To(Succeed())
		Expect(sc.Status.ClusterDeploymentRef.Name).To(Equal(clusterName))
		Expect(len(sc.Status.DeploymentConditions)).To(Equal(len(expectedConditions)))

		for _, cond := range expectedConditions {
			matched := false
			for i := range sc.Status.DeploymentConditions {
				if sc.Status.DeploymentConditions[i].Type == cond.Type &&
					sc.Status.DeploymentConditions[i].Status == cond.Status {
					matched = true
				}
			}
			Expect(matched).To(Equal(true), "Condition %s was not found", cond.Type)
		}
	})

	It("tests that ClusterDeploymentReconciler updates SiteConfig deploymentConditions", func() {
		key := types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}
		clusterDeployment := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: clusterNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "metaclusterinstall.openshift.io/v1alpha1",
						Kind:       v1alpha1.SiteConfigKind,
						Name:       clusterName,
					},
				},
			},
		}
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		Expect(c.Get(ctx, key, siteConfig)).To(Succeed())
		conditions.SetStatusCondition(&siteConfig.Status.Conditions,
			conditions.Provisioned,
			conditions.InProgress,
			metav1.ConditionTrue,
			"Provisioning cluster")
		err := conditions.UpdateStatus(ctx, c, siteConfig)
		Expect(err).ToNot(HaveOccurred())

		sc := &v1alpha1.SiteConfig{}
		Expect(c.Get(ctx, key, sc)).To(Succeed())

		DeploymentConditions := [][]hivev1.ClusterDeploymentCondition{
			{
				{
					Type:    hivev1.ClusterInstallRequirementsMetClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionFalse),
					Reason:  "ClusterNotReady",
					Message: "The cluster is not ready to begin the installation",
				},
				{
					Type:    hivev1.ClusterInstallStoppedClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionFalse),
					Reason:  "InstallationNotStopped",
					Message: "The installation is waiting to start or in progress",
				},
				{
					Type:    hivev1.ClusterInstallCompletedClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionTrue),
					Reason:  "InstallationNotFailed",
					Message: "The installation has not started",
				},
				{
					Type:    hivev1.ClusterInstallFailedClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionFalse),
					Reason:  "InstallationNotFailed",
					Message: "The installation has not started",
				},
			},

			{
				{
					Type:    hivev1.ClusterInstallRequirementsMetClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionTrue),
					Reason:  "ClusterInstallationStopped",
					Message: "The cluster installation stopped",
				},
				{
					Type:    hivev1.ClusterInstallStoppedClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionTrue),
					Reason:  "ClusterInstallStopped",
					Message: "The installation has stopped because it completed successfully",
				},
				{
					Type:    hivev1.ClusterInstallCompletedClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionTrue),
					Reason:  "InstallationCompleted",
					Message: "The installation has completed: Cluster is installed",
				},
				{
					Type:    hivev1.ClusterInstallFailedClusterDeploymentCondition,
					Status:  corev1.ConditionStatus(metav1.ConditionFalse),
					Reason:  "InstallationNotFailed",
					Message: "The installation has not failed",
				},
			},
		}

		for _, deploymentCondition := range DeploymentConditions {
			clusterDeployment.Status.Conditions = deploymentCondition
			Expect(c.Update(ctx, clusterDeployment)).To(Succeed())

			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(doNotRequeue()))

			sc := &v1alpha1.SiteConfig{}
			Expect(c.Get(ctx, key, sc)).To(Succeed())

			for _, cond := range deploymentCondition {
				matched := false
				for i := range sc.Status.DeploymentConditions {
					if sc.Status.DeploymentConditions[i].Type == cond.Type &&
						sc.Status.DeploymentConditions[i].Status == cond.Status &&
						sc.Status.DeploymentConditions[i].Message == cond.Message &&
						sc.Status.DeploymentConditions[i].Reason == cond.Reason {
						matched = true
					}
				}
				Expect(matched).To(Equal(true), "Condition %s was not found", cond.Type)
			}
		}
	})

	It("tests that SiteConfig status condition is set to provisioned when cluster is installed", func() {
		key := types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}
		clusterDeployment := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: clusterNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "metaclusterinstall.openshift.io/v1alpha1",
						Kind:       v1alpha1.SiteConfigKind,
						Name:       clusterName,
					},
				},
			},
		}
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		// Set cluster installed -> true
		clusterDeployment.Spec.Installed = true
		Expect(c.Update(ctx, clusterDeployment)).To(Succeed())

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		sc := &v1alpha1.SiteConfig{}
		Expect(c.Get(ctx, key, sc)).To(Succeed())

		found := false
		for i := range sc.Status.Conditions {
			if sc.Status.Conditions[i].Type == string(conditions.Provisioned) &&
				sc.Status.Conditions[i].Status == metav1.ConditionTrue {
				found = true
			}
		}
		Expect(found).To(Equal(true))
	})
})
