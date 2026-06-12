package kube

import (
	"context"
	"testing"
	"time"

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
