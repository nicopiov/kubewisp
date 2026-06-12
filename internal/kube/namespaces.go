package kube

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type NamespaceService interface {
	List(ctx context.Context) ([]string, error)
	Exists(ctx context.Context, name string) error
}

type Namespaces struct {
	factory ClientFactory
}

func NewNamespaceService() *Namespaces {
	return &Namespaces{factory: KubeconfigClientFactory{}}
}

func NewNamespaceServiceWithFactory(factory ClientFactory) *Namespaces {
	return &Namespaces{factory: factory}
}

func (s *Namespaces) List(ctx context.Context) ([]string, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}

	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, diagnose("list namespaces", err)
	}

	names := make([]string, 0, len(list.Items))
	for _, namespace := range list.Items {
		names = append(names, namespace.Name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *Namespaces) Exists(ctx context.Context, name string) error {
	client, err := s.client()
	if err != nil {
		return err
	}

	if _, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{}); err != nil {
		return diagnose(fmt.Sprintf("access namespace %q", name), err)
	}
	return nil
}

func (s *Namespaces) client() (kubernetes.Interface, error) {
	client, err := s.factory.Client()
	if err != nil {
		return nil, err
	}
	return client, nil
}
