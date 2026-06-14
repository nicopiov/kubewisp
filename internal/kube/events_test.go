package kube

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEventsListWarningsAggregatesAndSorts(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	client := fake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "backoff-1", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-123"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			Message:        "Back-off restarting failed container",
			Count:          2,
			FirstTimestamp: metav1.NewTime(older),
			LastTimestamp:  metav1.NewTime(older),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "backoff-2", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-123"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			Message:        "Back-off restarting failed container",
			Count:          3,
			FirstTimestamp: metav1.NewTime(newer),
			LastTimestamp:  metav1.NewTime(newer),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "failed", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "worker"},
			Type:           corev1.EventTypeWarning,
			Reason:         "FailedCreate",
			Message:        "quota exceeded",
			Count:          1,
			LastTimestamp:  metav1.NewTime(newer.Add(time.Minute)),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "normal", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "healthy"},
			Type:           corev1.EventTypeNormal,
			Reason:         "Pulled",
			Message:        "image pulled",
		},
	)

	events, err := NewEventServiceWithFactory(fakeFactory{client: client}).ListWarnings(context.Background(), "apps")
	if err != nil {
		t.Fatalf("ListWarnings() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want 2 aggregated warnings", events)
	}
	if events[0].ObjectKind != "Deployment" || events[0].ObjectName != "worker" {
		t.Fatalf("first event = %#v, want newest deployment event", events[0])
	}
	if events[1].Count != 5 || !events[1].FirstSeen.Equal(older) || !events[1].LastSeen.Equal(newer) {
		t.Fatalf("aggregated event = %#v", events[1])
	}
}

func TestEventsDiagnosePodFindsLikelyCauses(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-123", Namespace: "apps"},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app", RestartCount: 7,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "Error", ExitCode: 1,
				}},
			}}},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "backoff", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-123"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			Message:        "Back-off restarting failed container",
		},
	)

	report, err := NewEventServiceWithFactory(fakeFactory{client: client}).Diagnose(
		context.Background(), "apps", "Pod", "api-123",
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	joined := strings.Join(report.Causes, " ")
	for _, want := range []string{"CrashLoopBackOff", "exit code 1", "restarted 7 times", "repeatedly failing"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("causes missing %q: %#v", want, report.Causes)
		}
	}
	if len(report.Events) != 1 || report.Summary == "" {
		t.Fatalf("report = %#v", report)
	}
}

func TestEventsDiagnoseWorkloadIncludesSelectedPodWarnings(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "api-123", Namespace: "apps", Labels: map[string]string{"app": "api"},
		}},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "unhealthy", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-123"},
			Type:           corev1.EventTypeWarning,
			Reason:         "Unhealthy",
			Message:        "Readiness probe failed",
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "unrelated", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "worker-123"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
		},
	)

	report, err := NewEventServiceWithFactory(fakeFactory{client: client}).Diagnose(
		context.Background(), "apps", "Deployment", "api",
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if len(report.Events) != 1 || report.Events[0].ObjectName != "api-123" ||
		!strings.Contains(strings.Join(report.Causes, " "), "probe is failing") {
		t.Fatalf("report = %#v", report)
	}
}
