/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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

package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	testutil "github.com/submariner-io/admiral/pkg/test"
	"github.com/submariner-io/lighthouse/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	mcsv1a1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
)

var _ = Describe("ClusterIP Service export", func() {
	Describe("Single cluster", testClusterIPServiceInOneCluster)
})

func testClusterIPServiceInOneCluster() {
	var t *testDriver

	BeforeEach(func() {
		t = newTestDiver()

		t.cluster1.createEndpoints()
	})

	JustBeforeEach(func() {
		t.justBeforeEach()
	})

	AfterEach(func() {
		t.afterEach()
	})

	When("a ServiceExport is created", func() {
		Context("and the Service already exists", func() {
			It("should export the service and update the ServiceExport status", func() {
				t.cluster1.createService()
				t.cluster1.createServiceExport()
				t.awaitNonHeadlessServiceExported(&t.cluster1)
			})
		})

		Context("and the Service doesn't initially exist", func() {
			It("should eventually export the service", func() {
				t.cluster1.createServiceExport()
				t.cluster1.awaitServiceUnavailableStatus()

				t.cluster1.createService()
				t.awaitNonHeadlessServiceExported(&t.cluster1)
			})
		})
	})

	When("a ServiceExport is deleted after the service is exported", func() {
		It("should unexport the service", func() {
			t.cluster1.createService()
			t.cluster1.createServiceExport()
			t.awaitNonHeadlessServiceExported(&t.cluster1)

			t.cluster1.deleteServiceExport()
			t.awaitServiceUnexported(&t.cluster1)
		})
	})

	When("an exported Service is deleted and recreated while the ServiceExport still exists", func() {
		It("should unexport and re-export the service", func() {
			t.cluster1.createService()
			t.cluster1.createServiceExport()
			t.awaitNonHeadlessServiceExported(&t.cluster1)
			t.cluster1.localDynClient.Fake.ClearActions()

			t.cluster1.deleteService()
			t.cluster1.awaitServiceUnavailableStatus()
			t.cluster1.awaitServiceExportCondition(newServiceExportSyncedCondition(corev1.ConditionFalse, "NoServiceImport"))
			t.awaitServiceUnexported(&t.cluster1)

			t.cluster1.createService()
			t.awaitNonHeadlessServiceExported(&t.cluster1)
		})
	})

	When("the type of an exported Service is updated to an unsupported type", func() {
		It("should unexport the ServiceImport and update the ServiceExport status appropriately", func() {
			t.cluster1.createService()
			t.cluster1.createServiceExport()
			t.awaitNonHeadlessServiceExported(&t.cluster1)

			t.cluster1.service.Spec.Type = corev1.ServiceTypeNodePort
			t.cluster1.updateService()

			t.cluster1.awaitServiceExportCondition(newServiceExportValidCondition(corev1.ConditionFalse, "UnsupportedServiceType"))
			t.cluster1.awaitServiceExportCondition(newServiceExportSyncedCondition(corev1.ConditionFalse, "NoServiceImport"))
			t.awaitServiceUnexported(&t.cluster1)
		})
	})

	When("a ServiceExport is created for a Service whose type is unsupported", func() {
		BeforeEach(func() {
			t.cluster1.service.Spec.Type = corev1.ServiceTypeNodePort
		})

		JustBeforeEach(func() {
			t.cluster1.createService()
			t.cluster1.createServiceExport()
		})

		It("should update the ServiceExport status appropriately and not export the serviceImport", func() {
			t.cluster1.awaitServiceExportCondition(newServiceExportValidCondition(corev1.ConditionFalse, "UnsupportedServiceType"))
			t.cluster1.ensureNoServiceExportCondition(constants.ServiceExportSynced)
		})

		Context("and is subsequently updated to a supported type", func() {
			It("should eventually export the service and update the ServiceExport status appropriately", func() {
				t.cluster1.awaitServiceExportCondition(newServiceExportValidCondition(corev1.ConditionFalse, "UnsupportedServiceType"))

				t.cluster1.service.Spec.Type = corev1.ServiceTypeClusterIP
				t.cluster1.updateService()

				t.awaitNonHeadlessServiceExported(&t.cluster1)
			})
		})
	})

	When("the backend Endpoints has no ready addresses", func() {
		JustBeforeEach(func() {
			t.cluster1.createService()
			t.cluster1.createServiceExport()
			t.awaitNonHeadlessServiceExported(&t.cluster1)
		})

		Specify("the EndpointSlice's service IP address should indicate not ready", func() {
			t.cluster1.endpoints.Subsets[0].Addresses = nil

			t.cluster1.updateEndpoints()
			t.awaitEndpointSlice(&t.cluster1)
		})
	})

	When("two Services with the same name in different namespaces are exported", func() {
		It("should correctly export both services", func() {
			t.cluster1.createService()
			t.cluster1.createServiceExport()
			t.awaitNonHeadlessServiceExported(&t.cluster1)

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      t.cluster1.service.Name,
					Namespace: "other-service-ns",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.253.9.2",
				},
			}

			serviceExport := &mcsv1a1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service.Name,
					Namespace: service.Namespace,
				},
			}

			endpoints := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service.Name,
					Namespace: service.Namespace,
				},
			}

			expServiceImport := &mcsv1a1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service.Name,
					Namespace: service.Namespace,
				},
				Spec: mcsv1a1.ServiceImportSpec{
					Type:  mcsv1a1.ClusterSetIP,
					Ports: []mcsv1a1.ServicePort{},
				},
				Status: mcsv1a1.ServiceImportStatus{
					Clusters: []mcsv1a1.ClusterStatus{
						{
							Cluster: t.cluster1.clusterID,
						},
					},
				},
			}

			expEndpointSlice := &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service.Name,
					Namespace: service.Namespace,
					Labels: map[string]string{
						discovery.LabelManagedBy:        constants.LabelValueManagedBy,
						constants.MCSLabelSourceCluster: t.cluster1.clusterID,
						mcsv1a1.LabelServiceName:        service.Name,
						constants.LabelSourceNamespace:  service.Namespace,
					},
				},
				AddressType: discovery.AddressTypeIPv4,
				Endpoints: []discovery.Endpoint{
					{
						Addresses:  []string{service.Spec.ClusterIP},
						Conditions: discovery.EndpointConditions{Ready: pointer.Bool(false)},
					},
				},
			}

			test.CreateResource(endpointsClientFor(t.cluster1.localDynClient, endpoints.Namespace), endpoints)
			test.CreateResource(t.cluster1.dynamicServiceClientFor().Namespace(service.Namespace), service)
			test.CreateResource(serviceExportClientFor(t.cluster1.localDynClient, service.Namespace), serviceExport)

			awaitServiceImport(t.cluster2.localServiceImportClient, expServiceImport)
			awaitEndpointSlice(endpointSliceClientFor(t.cluster2.localDynClient, endpoints.Namespace), expEndpointSlice)

			// Ensure the resources for the first Service weren't overwritten
			t.awaitAggregatedServiceImport(mcsv1a1.ClusterSetIP, t.cluster1.service.Name, t.cluster1.service.Namespace, &t.cluster1)
		})
	})

	Specify("an EndpointSlice not managed by Lighthouse should not be synced to the broker", func() {
		test.CreateResource(endpointSliceClientFor(t.cluster1.localDynClient, t.cluster1.service.Namespace),
			&discovery.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
				Name:   "other-eps",
				Labels: map[string]string{discovery.LabelManagedBy: "other"},
			}})

		testutil.EnsureNoResource(resource.ForDynamic(endpointSliceClientFor(t.syncerConfig.BrokerClient, test.RemoteNamespace)), "other-eps")
	})
}