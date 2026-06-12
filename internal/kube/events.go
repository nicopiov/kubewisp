package kube

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type EventService interface {
	ListWarnings(ctx context.Context, namespace string) ([]NamespaceEventSummary, error)
}

type NamespaceEventSummary struct {
	ObjectKind string
	ObjectName string
	Reason     string
	Message    string
	Count      int32
	FirstSeen  time.Time
	LastSeen   time.Time
}

type Events struct {
	factory ClientFactory
}

func NewEventService() *Events {
	return &Events{factory: NewKubeconfigClientFactory()}
}

func NewEventServiceWithFactory(factory ClientFactory) *Events {
	return &Events{factory: factory}
}

func (s *Events) ListWarnings(ctx context.Context, namespace string) ([]NamespaceEventSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	list, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "type=" + corev1.EventTypeWarning,
	})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("list warning events in namespace %q", namespace), err)
	}
	return aggregateWarningEvents(list.Items), nil
}

func (s *Events) client() (kubernetes.Interface, error) {
	client, err := s.factory.Client()
	if err != nil {
		return nil, err
	}
	return client, nil
}

func aggregateWarningEvents(events []corev1.Event) []NamespaceEventSummary {
	aggregated := make(map[string]NamespaceEventSummary)
	for _, event := range events {
		if event.Type != corev1.EventTypeWarning {
			continue
		}
		firstSeen, lastSeen := namespaceEventTimes(event)
		key := event.InvolvedObject.Kind + "\x00" + event.InvolvedObject.Name + "\x00" + event.Reason + "\x00" + event.Message
		summary, exists := aggregated[key]
		if !exists {
			summary = NamespaceEventSummary{
				ObjectKind: event.InvolvedObject.Kind,
				ObjectName: event.InvolvedObject.Name,
				Reason:     event.Reason,
				Message:    event.Message,
				FirstSeen:  firstSeen,
				LastSeen:   lastSeen,
			}
		}
		count := event.Count
		if event.Series != nil && event.Series.Count > count {
			count = event.Series.Count
		}
		if count == 0 {
			count = 1
		}
		summary.Count += count
		if summary.FirstSeen.IsZero() || (!firstSeen.IsZero() && firstSeen.Before(summary.FirstSeen)) {
			summary.FirstSeen = firstSeen
		}
		if lastSeen.After(summary.LastSeen) {
			summary.LastSeen = lastSeen
		}
		aggregated[key] = summary
	}

	summaries := make([]NamespaceEventSummary, 0, len(aggregated))
	for _, summary := range aggregated {
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].LastSeen.Equal(summaries[j].LastSeen) {
			if summaries[i].ObjectKind == summaries[j].ObjectKind {
				return summaries[i].ObjectName < summaries[j].ObjectName
			}
			return summaries[i].ObjectKind < summaries[j].ObjectKind
		}
		return summaries[i].LastSeen.After(summaries[j].LastSeen)
	})
	return summaries
}

func namespaceEventTimes(event corev1.Event) (time.Time, time.Time) {
	firstSeen := event.FirstTimestamp.Time
	if firstSeen.IsZero() {
		firstSeen = event.EventTime.Time
	}
	if firstSeen.IsZero() {
		firstSeen = event.CreationTimestamp.Time
	}

	lastSeen := event.LastTimestamp.Time
	if event.Series != nil && event.Series.LastObservedTime.Time.After(lastSeen) {
		lastSeen = event.Series.LastObservedTime.Time
	}
	if event.EventTime.Time.After(lastSeen) {
		lastSeen = event.EventTime.Time
	}
	if lastSeen.IsZero() {
		lastSeen = event.CreationTimestamp.Time
	}
	if lastSeen.IsZero() {
		lastSeen = firstSeen
	}
	return firstSeen, lastSeen
}
