package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type NetworkService interface {
	List(ctx context.Context, namespace string) ([]NetworkSummary, error)
	Describe(ctx context.Context, namespace, kind, name string) (NetworkDetails, error)
	PodsForService(ctx context.Context, namespace, name string) ([]PodSummary, error)
	ServicesForIngress(ctx context.Context, namespace, name string) ([]NetworkSummary, error)
}

type NetworkSummary struct {
	Kind      string
	Name      string
	Type      string
	Address   string
	Ports     []string
	Hosts     []string
	CreatedAt time.Time
}

type NetworkDetails struct {
	NetworkSummary
	Selector  []string
	Endpoints []string
	Routes    []string
}

type Network struct {
	factory ClientFactory
}

func NewNetworkService() *Network {
	return &Network{factory: NewKubeconfigClientFactory()}
}

func NewNetworkServiceWithFactory(factory ClientFactory) *Network {
	return &Network{factory: factory}
}

func (s *Network) List(ctx context.Context, namespace string) ([]NetworkSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	var services *corev1.ServiceList
	var ingresses *networkingv1.IngressList
	var serviceErr, ingressErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		services, serviceErr = client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	}()
	go func() {
		defer wait.Done()
		ingresses, ingressErr = client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	}()
	wait.Wait()
	if serviceErr != nil {
		return nil, diagnose(fmt.Sprintf("list services in namespace %q", namespace), serviceErr)
	}
	if ingressErr != nil {
		return nil, diagnose(fmt.Sprintf("list ingresses in namespace %q", namespace), ingressErr)
	}
	resources := make([]NetworkSummary, 0, len(services.Items)+len(ingresses.Items))
	for index := range services.Items {
		resources = append(resources, summarizeService(&services.Items[index]))
	}
	for index := range ingresses.Items {
		ingress := &ingresses.Items[index]
		resources = append(resources, NetworkSummary{
			Kind: "Ingress", Name: ingress.Name, Type: valueOrDefault(ingress.Spec.IngressClassName, "default"),
			Address: ingressAddresses(ingress.Status.LoadBalancer.Ingress),
			Hosts:   ingressHosts(ingress.Spec.Rules), CreatedAt: ingress.CreationTimestamp.Time,
		})
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Kind == resources[j].Kind {
			return resources[i].Name < resources[j].Name
		}
		return resources[i].Kind < resources[j].Kind
	})
	return resources, nil
}

func (s *Network) Describe(ctx context.Context, namespace, kind, name string) (NetworkDetails, error) {
	client, err := s.client()
	if err != nil {
		return NetworkDetails{}, err
	}
	switch strings.ToLower(kind) {
	case "service", "services":
		service, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return NetworkDetails{}, diagnose(fmt.Sprintf("get service %q in namespace %q", name, namespace), err)
		}
		details := NetworkDetails{
			NetworkSummary: summarizeService(service),
			Selector:       sortedMap(service.Spec.Selector),
		}
		endpoints, endpointErr := client.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: discoveryv1.LabelServiceName + "=" + name,
		})
		if endpointErr == nil {
			seen := make(map[string]struct{})
			for _, slice := range endpoints.Items {
				for _, endpoint := range slice.Endpoints {
					if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
						continue
					}
					for _, address := range endpoint.Addresses {
						if _, exists := seen[address]; !exists {
							details.Endpoints = append(details.Endpoints, address)
							seen[address] = struct{}{}
						}
					}
				}
			}
			sort.Strings(details.Endpoints)
		}
		return details, nil
	case "ingress", "ingresses":
		ingress, err := client.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return NetworkDetails{}, diagnose(fmt.Sprintf("get ingress %q in namespace %q", name, namespace), err)
		}
		return NetworkDetails{NetworkSummary: NetworkSummary{
			Kind: "Ingress", Name: ingress.Name, Type: valueOrDefault(ingress.Spec.IngressClassName, "default"),
			Address: ingressAddresses(ingress.Status.LoadBalancer.Ingress),
			Hosts:   ingressHosts(ingress.Spec.Rules), CreatedAt: ingress.CreationTimestamp.Time,
		}, Routes: ingressRoutes(ingress.Spec.Rules)}, nil
	default:
		return NetworkDetails{}, fmt.Errorf("network resource kind %q is not supported", kind)
	}
}

func (s *Network) PodsForService(ctx context.Context, namespace, name string) ([]PodSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	service, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("get service %q in namespace %q", name, namespace), err)
	}
	if len(service.Spec.Selector) == 0 {
		return nil, nil
	}
	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(service.Spec.Selector).String(),
	})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("list pods selected by service %q in namespace %q", name, namespace), err)
	}
	pods := make([]PodSummary, 0, len(list.Items))
	for index := range list.Items {
		pods = append(pods, summarizePod(&list.Items[index]))
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
	return pods, nil
}

func (s *Network) ServicesForIngress(ctx context.Context, namespace, name string) ([]NetworkSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	ingress, err := client.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("get ingress %q in namespace %q", name, namespace), err)
	}
	names := ingressServiceNames(ingress)
	services := make([]NetworkSummary, 0, len(names))
	for _, serviceName := range names {
		service, err := client.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get service %q used by ingress %q in namespace %q", serviceName, name, namespace), err)
		}
		services = append(services, summarizeService(service))
	}
	return services, nil
}

func (s *Network) client() (kubernetes.Interface, error) {
	return s.factory.Client()
}

func summarizeService(service *corev1.Service) NetworkSummary {
	return NetworkSummary{
		Kind: "Service", Name: service.Name, Type: string(service.Spec.Type),
		Address: serviceAddress(service.Spec.ClusterIP, service.Status.LoadBalancer.Ingress),
		Ports:   servicePorts(service.Spec.Ports), CreatedAt: service.CreationTimestamp.Time,
	}
}

func ingressServiceNames(ingress *networkingv1.Ingress) []string {
	seen := make(map[string]struct{})
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		seen[ingress.Spec.DefaultBackend.Service.Name] = struct{}{}
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				seen[path.Backend.Service.Name] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func servicePorts(ports []corev1.ServicePort) []string {
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		name := port.Name
		if name == "" {
			name = "-"
		}
		values = append(values, fmt.Sprintf("%s:%d/%s -> %s", name, port.Port, port.Protocol, port.TargetPort.String()))
	}
	sort.Strings(values)
	return values
}

func serviceAddress(clusterIP string, ingresses []corev1.LoadBalancerIngress) string {
	addresses := []string{clusterIP}
	for _, ingress := range ingresses {
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
		}
	}
	return strings.Join(nonEmpty(addresses), ", ")
}

func ingressAddresses(ingresses []networkingv1.IngressLoadBalancerIngress) string {
	var addresses []string
	for _, ingress := range ingresses {
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
		}
	}
	return strings.Join(addresses, ", ")
}

func nonEmpty(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value != "" && value != corev1.ClusterIPNone {
			result = append(result, value)
		}
	}
	return result
}

func sortedMap(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

func ingressHosts(rules []networkingv1.IngressRule) []string {
	hosts := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func ingressRoutes(rules []networkingv1.IngressRule) []string {
	var routes []string
	for _, rule := range rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			backend := path.Backend.Service
			if backend == nil {
				continue
			}
			port := backend.Port.Name
			if port == "" {
				port = fmt.Sprint(backend.Port.Number)
			}
			routes = append(routes, fmt.Sprintf("%s%s -> Service/%s:%s", rule.Host, path.Path, backend.Name, port))
		}
	}
	sort.Strings(routes)
	return routes
}

func valueOrDefault(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}
