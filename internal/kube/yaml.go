package kube

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

type ResourceYAMLService interface {
	Get(ctx context.Context, namespace, kind, name string) (string, error)
}

type ResourceYAML struct {
	factory ClientFactory
}

func NewResourceYAMLService() *ResourceYAML {
	return &ResourceYAML{factory: NewKubeconfigClientFactory()}
}

func NewResourceYAMLServiceWithFactory(factory ClientFactory) *ResourceYAML {
	return &ResourceYAML{factory: factory}
}

func (s *ResourceYAML) Get(ctx context.Context, namespace, kind, name string) (string, error) {
	client, err := s.client()
	if err != nil {
		return "", err
	}
	object, err := getResourceObject(ctx, client, namespace, kind, name)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	serializer := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)
	if err := serializer.Encode(object, &output); err != nil {
		return "", fmt.Errorf("encode %s/%s as YAML: %w", kind, name, err)
	}
	return output.String(), nil
}

func getResourceObject(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, kind, name string,
) (runtime.Object, error) {
	switch strings.ToLower(kind) {
	case "pod", "pods":
		item, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get pod %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "v1", "Pod")
		return item, nil
	case "deployment", "deployments":
		item, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get deployment %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "apps/v1", "Deployment")
		return item, nil
	case "statefulset", "statefulsets":
		item, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get statefulset %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "apps/v1", "StatefulSet")
		return item, nil
	case "daemonset", "daemonsets":
		item, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get daemonset %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "apps/v1", "DaemonSet")
		return item, nil
	case "cronjob", "cronjobs":
		item, err := client.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get cronjob %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "batch/v1", "CronJob")
		return item, nil
	case "service", "services":
		item, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get service %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "v1", "Service")
		return item, nil
	case "ingress", "ingresses":
		item, err := client.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get ingress %q in namespace %q", name, namespace), err)
		}
		ensureTypeMeta(item, "networking.k8s.io/v1", "Ingress")
		return item, nil
	default:
		return nil, fmt.Errorf("resource kind %q does not support YAML preview", kind)
	}
}

func ensureTypeMeta(object runtime.Object, apiVersion, kind string) {
	switch item := object.(type) {
	case *corev1.Pod:
		item.APIVersion, item.Kind = apiVersion, kind
	case *corev1.Service:
		item.APIVersion, item.Kind = apiVersion, kind
	case *appsv1.Deployment:
		item.APIVersion, item.Kind = apiVersion, kind
	case *appsv1.StatefulSet:
		item.APIVersion, item.Kind = apiVersion, kind
	case *appsv1.DaemonSet:
		item.APIVersion, item.Kind = apiVersion, kind
	case *batchv1.CronJob:
		item.APIVersion, item.Kind = apiVersion, kind
	case *networkingv1.Ingress:
		item.APIVersion, item.Kind = apiVersion, kind
	}
}

func (s *ResourceYAML) client() (kubernetes.Interface, error) {
	return s.factory.Client()
}
