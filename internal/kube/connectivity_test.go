package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

type fakeFactory struct {
	client kubernetes.Interface
	err    error
}

func (f fakeFactory) Client() (kubernetes.Interface, error) {
	return f.client, f.err
}

func TestConnectivityCheck(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "api"},
	})
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.32.1"}

	report, err := NewConnectivityCheckerWithFactory(fakeFactory{client: client}).Check(context.Background(), "api")

	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if report.ServerVersion != "v1.32.1" || report.Namespace != "api" {
		t.Fatalf("report = %#v", report)
	}
}

func TestConnectivityCheckClassifiesForbidden(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.32.1"}
	client.PrependReactor("get", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("namespaces \"api\" is forbidden")
	})

	_, err := NewConnectivityCheckerWithFactory(fakeFactory{client: client}).Check(context.Background(), "api")

	if err == nil || !strings.Contains(err.Error(), "Kubernetes RBAC denied access") {
		t.Fatalf("Check() error = %v, want RBAC diagnostic", err)
	}
}

func TestDiagnoseMissingGKEAuthPlugin(t *testing.T) {
	t.Parallel()

	err := diagnose("reach Kubernetes API", errors.New("exec: executable gke-gcloud-auth-plugin not found"))

	if !strings.Contains(err.Error(), "gke-gcloud-auth-plugin is required") {
		t.Fatalf("diagnose() error = %v, want missing plugin diagnostic", err)
	}
}
