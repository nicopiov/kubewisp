package kube

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResourceYAMLGetSupportedResources(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/api:v1"}}},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "apps"},
		},
	)
	service := NewResourceYAMLServiceWithFactory(fakeFactory{client: client})

	pod, err := service.Get(context.Background(), "apps", "Pod", "api")
	if err != nil {
		t.Fatalf("Get(Pod) error = %v", err)
	}
	for _, want := range []string{"apiVersion: v1", "kind: Pod", "name: api", "image: example/api:v1"} {
		if !strings.Contains(pod, want) {
			t.Fatalf("pod YAML missing %q:\n%s", want, pod)
		}
	}

	deployment, err := service.Get(context.Background(), "apps", "Deployment", "web")
	if err != nil {
		t.Fatalf("Get(Deployment) error = %v", err)
	}
	for _, want := range []string{"apiVersion: apps/v1", "kind: Deployment", "name: web"} {
		if !strings.Contains(deployment, want) {
			t.Fatalf("deployment YAML missing %q:\n%s", want, deployment)
		}
	}
}

func TestResourceYAMLRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	service := NewResourceYAMLServiceWithFactory(fakeFactory{client: fake.NewSimpleClientset()})
	_, err := service.Get(context.Background(), "apps", "Secret", "token")
	if err == nil || !strings.Contains(err.Error(), "does not support YAML preview") {
		t.Fatalf("Get(Secret) error = %v, want unsupported kind", err)
	}
}
