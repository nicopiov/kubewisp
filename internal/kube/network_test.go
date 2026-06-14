package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNetworkListAndDescribe(t *testing.T) {
	t.Parallel()

	className := "gce"
	pathType := networkingv1.PathTypePrefix
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "10.0.0.1",
				Selector:  map[string]string{"app": "api"},
				Ports: []corev1.ServicePort{{
					Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(8080),
				}},
			},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-abc", Namespace: "apps", Labels: map[string]string{discoveryv1.LabelServiceName: "api"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.2.0.5"}}},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					Host: "api.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path: "/", PathType: &pathType,
							Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
								Name: "api", Port: networkingv1.ServiceBackendPort{Number: 80},
							}},
						}},
					}},
				}},
			},
		},
	)
	service := NewNetworkServiceWithFactory(fakeFactory{client: client})

	resources, err := service.List(context.Background(), "apps")
	if err != nil || len(resources) != 2 || resources[0].Kind != "Ingress" || resources[1].Kind != "Service" {
		t.Fatalf("List() = %#v, error = %v", resources, err)
	}
	details, err := service.Describe(context.Background(), "apps", "Service", "api")
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if details.Address != "10.0.0.1" || len(details.Endpoints) != 1 ||
		!strings.Contains(details.Ports[0], "8080") || details.Selector[0] != "app=api" {
		t.Fatalf("service details = %#v", details)
	}
	ingress, err := service.Describe(context.Background(), "apps", "Ingress", "api")
	if err != nil || len(ingress.Routes) != 1 || !strings.Contains(ingress.Routes[0], "Service/api:80") {
		t.Fatalf("ingress details = %#v, error = %v", ingress, err)
	}
}

func TestNetworkRelationships(t *testing.T) {
	t.Parallel()

	pathType := networkingv1.PathTypePrefix
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "api"},
				Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
			},
		},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "fallback", Namespace: "apps"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-b", Namespace: "apps", Labels: map[string]string{"app": "api"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-a", Namespace: "apps", Labels: map[string]string{"app": "api"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "apps", Labels: map[string]string{"app": "worker"}}},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "public", Namespace: "apps"},
			Spec: networkingv1.IngressSpec{
				DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "fallback"}},
				Rules: []networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{Path: "/", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}},
							{Path: "/v2", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}},
						},
					}},
				}},
			},
		},
	)
	service := NewNetworkServiceWithFactory(fakeFactory{client: client})

	pods, err := service.PodsForService(context.Background(), "apps", "api")
	if err != nil || len(pods) != 2 || pods[0].Name != "api-a" || pods[1].Name != "api-b" {
		t.Fatalf("PodsForService() = %#v, error = %v", pods, err)
	}
	services, err := service.ServicesForIngress(context.Background(), "apps", "public")
	if err != nil || len(services) != 2 || services[0].Name != "api" || services[1].Name != "fallback" {
		t.Fatalf("ServicesForIngress() = %#v, error = %v", services, err)
	}
}
