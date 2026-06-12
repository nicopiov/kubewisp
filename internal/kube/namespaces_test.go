package kube

import (
	"context"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNamespacesListSorted(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workers"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "api"}},
	)
	service := NewNamespaceServiceWithFactory(fakeFactory{client: client})

	names, err := service.List(context.Background())

	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []string{"api", "workers"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("List() = %#v, want %#v", names, want)
	}
}

func TestNamespacesExistsReportsMissingNamespace(t *testing.T) {
	t.Parallel()

	service := NewNamespaceServiceWithFactory(fakeFactory{client: fake.NewSimpleClientset()})

	err := service.Exists(context.Background(), "missing")

	if err == nil || !strings.Contains(err.Error(), `access namespace "missing"`) {
		t.Fatalf("Exists() error = %v, want missing namespace diagnostic", err)
	}
}
