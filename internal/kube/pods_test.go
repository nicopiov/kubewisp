package kube

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestPodsListSummarizesAndSorts(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "api"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker"}},
				NodeName:   "node-b",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:         "worker",
					RestartCount: 3,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "api"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  "app",
					Ready: true,
				}},
			},
		},
	)

	pods, err := NewPodServiceWithFactory(fakeFactory{client: client}).List(context.Background(), "api")

	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got := []string{pods[0].Name, pods[1].Name}; !reflect.DeepEqual(got, []string{"api", "worker"}) {
		t.Fatalf("pod names = %#v", got)
	}
	if pods[0].Ready != "1/1" || pods[0].Status != "Running" {
		t.Fatalf("api summary = %#v", pods[0])
	}
	if pods[1].Status != "CrashLoopBackOff" || pods[1].Restarts != 3 {
		t.Fatalf("worker summary = %#v", pods[1])
	}
}

func TestPodsDescribeKeepsDetailsWhenEventsAreForbidden(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "api"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	})
	client.PrependReactor("list", "events", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("events is forbidden")
	})

	details, err := NewPodServiceWithFactory(fakeFactory{client: client}).Describe(context.Background(), "api", "api")

	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if details.Name != "api" {
		t.Fatalf("Name = %q, want api", details.Name)
	}
	if !strings.Contains(details.EventsWarning, "Kubernetes RBAC denied access") {
		t.Fatalf("EventsWarning = %q, want RBAC warning", details.EventsWarning)
	}
}

func TestPodsContainers(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "api"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "app"},
			{Name: "sidecar"},
		}},
	})

	names, err := NewPodServiceWithFactory(fakeFactory{client: client}).Containers(context.Background(), "api", "api")

	if err != nil {
		t.Fatalf("Containers() error = %v", err)
	}
	if !reflect.DeepEqual(names, []string{"app", "sidecar"}) {
		t.Fatalf("Containers() = %#v", names)
	}
}

func TestPodsPortsReturnsStructuredSortedPorts(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "api"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "metrics", Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9090}}},
			{Name: "app", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}},
		}},
	})

	ports, err := NewPodServiceWithFactory(fakeFactory{client: client}).Ports(context.Background(), "api", "api")
	if err != nil {
		t.Fatalf("Ports() error = %v", err)
	}
	want := []PodPort{
		{Container: "app", Name: "http", Port: 8080, Protocol: "TCP"},
		{Container: "metrics", Name: "metrics", Port: 9090, Protocol: "TCP"},
	}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("Ports() = %#v, want %#v", ports, want)
	}
}

func TestPodsActionInfoAndDelete(t *testing.T) {
	t.Parallel()

	controller := true
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "api",
			OwnerReferences: []metav1.OwnerReference{{
				Kind:       "ReplicaSet",
				Name:       "api-abc",
				Controller: &controller,
			}},
		},
	})
	service := NewPodServiceWithFactory(fakeFactory{client: client})

	info, err := service.ActionInfo(context.Background(), "api", "api")
	if err != nil {
		t.Fatalf("ActionInfo() error = %v", err)
	}
	if info.ControllerOwner != "ReplicaSet/api-abc" {
		t.Fatalf("ControllerOwner = %q", info.ControllerOwner)
	}
	if err := service.Delete(context.Background(), "api", "api"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := client.CoreV1().Pods("api").Get(context.Background(), "api", metav1.GetOptions{}); err == nil {
		t.Fatal("pod still exists after Delete()")
	}
}

func TestSummarizePodTracksLastRestart(t *testing.T) {
	t.Parallel()

	finished := metav1.NewTime(time.Now().Add(-time.Minute))
	summary := summarizePod(&corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:         "app",
			RestartCount: 3,
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				FinishedAt: finished,
			}},
		}}},
	})
	if !summary.LastRestartAt.Equal(finished.Time) {
		t.Fatalf("LastRestartAt = %v, want %v", summary.LastRestartAt, finished.Time)
	}
}

func TestSummarizePodTracksControllerOwner(t *testing.T) {
	t.Parallel()

	controller := true
	summary := summarizePod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{
			Kind:       "Job",
			Name:       "cleanup-123",
			Controller: &controller,
		}}},
	})

	if summary.OwnerKind != "Job" || summary.OwnerName != "cleanup-123" {
		t.Fatalf("owner = %s/%s, want Job/cleanup-123", summary.OwnerKind, summary.OwnerName)
	}
}

func TestPodsLogsPassesOptionsToStream(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	var got PodLogsOptions
	service := NewPodServiceWithFactoryAndLogStream(
		fakeFactory{client: client},
		func(_ context.Context, _ kubernetes.Interface, options PodLogsOptions) (io.ReadCloser, error) {
			got = options
			return io.NopCloser(strings.NewReader("hello\n")), nil
		},
	)
	want := PodLogsOptions{
		Namespace:  "api",
		Pod:        "api-abc",
		Container:  "app",
		TailLines:  200,
		Follow:     true,
		Previous:   true,
		Timestamps: true,
	}

	stream, err := service.Logs(context.Background(), want)

	if err != nil {
		t.Fatalf("Logs() error = %v", err)
	}
	stream.Close()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}
}

func TestPodsDescribeHidesEnvironmentValues(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api",
			Namespace:         "api",
			CreationTimestamp: metav1.NewTime(time.Now()),
			Labels:            map[string]string{"app": "api"},
			Annotations:       map[string]string{"example.com/note": "private annotation value"},
			OwnerReferences:   []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-abc"}},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "api-service-account",
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "example/api:v1",
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "credentials", MountPath: "/var/credentials", ReadOnly: true}},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt32(8080)},
					},
					PeriodSeconds: 5,
				},
				Env: []corev1.EnvVar{
					{Name: "PLAIN_SECRET", Value: "must-not-appear"},
					{Name: "SECRET_REF", ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"},
							Key:                  "token",
						},
					}},
				},
			}},
			Volumes: []corev1.Volume{{
				Name: "credentials",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "api-secret"},
				},
			}},
		},
		Status: corev1.PodStatus{
			PodIP:    "10.0.0.4",
			HostIP:   "10.0.0.1",
			QOSClass: corev1.PodQOSBurstable,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
				Reason: "ContainersNotReady",
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "app",
				RestartCount: 2,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason:   "Error",
					ExitCode: 1,
				}},
			}},
		},
	}, &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "api-failed", Namespace: "api"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "api",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "BackOff",
		Message:       "Back-off restarting failed container",
		Count:         4,
		LastTimestamp: metav1.NewTime(time.Now()),
	})

	details, err := NewPodServiceWithFactory(fakeFactory{client: client}).Describe(context.Background(), "api", "api")

	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if got := details.Containers[0].EnvironmentNames; !reflect.DeepEqual(got, []string{"PLAIN_SECRET", "SECRET_REF"}) {
		t.Fatalf("environment names = %#v", got)
	}
	if got := details.Annotations; !reflect.DeepEqual(got, []string{"example.com/note"}) {
		t.Fatalf("annotations = %#v", got)
	}
	if got := details.Volumes; !reflect.DeepEqual(got, []string{"credentials (secret)"}) {
		t.Fatalf("volumes = %#v", got)
	}
	if details.PodIP != "10.0.0.4" || details.ServiceAccount != "api-service-account" {
		t.Fatalf("details = %#v", details)
	}
	container := details.Containers[0]
	for _, expected := range []string{"CrashLoopBackOff", "exitCode=1"} {
		if !strings.Contains(container.State+" "+container.LastState, expected) {
			t.Fatalf("container states = %q / %q, want %q", container.State, container.LastState, expected)
		}
	}
	if !reflect.DeepEqual(container.Requests, []string{"cpu=100m"}) {
		t.Fatalf("requests = %#v", container.Requests)
	}
	if got := details.Events[0].Reason; got != "BackOff" {
		t.Fatalf("event reason = %q, want BackOff", got)
	}
}
