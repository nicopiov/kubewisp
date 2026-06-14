package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type EventService interface {
	ListWarnings(ctx context.Context, namespace string) ([]NamespaceEventSummary, error)
	Diagnose(ctx context.Context, namespace, kind, name string) (ResourceDiagnostics, error)
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

type ResourceDiagnostics struct {
	ResourceKind string
	ResourceName string
	Summary      string
	Causes       []string
	Events       []NamespaceEventSummary
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

func (s *Events) Diagnose(ctx context.Context, namespace, kind, name string) (ResourceDiagnostics, error) {
	client, err := s.client()
	if err != nil {
		return ResourceDiagnostics{}, err
	}
	report := ResourceDiagnostics{ResourceKind: kind, ResourceName: name}
	relatedPods := make(map[string]struct{})
	if strings.EqualFold(kind, "Pod") {
		relatedPods[name] = struct{}{}
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return ResourceDiagnostics{}, diagnose(fmt.Sprintf("get pod %q in namespace %q", name, namespace), err)
		}
		report.Causes = append(report.Causes, podDiagnosticCauses(pod)...)
	} else {
		selector, err := diagnosticWorkloadSelector(ctx, client, namespace, kind, name)
		if err != nil {
			return ResourceDiagnostics{}, err
		}
		if selector != nil {
			labelSelector, err := metav1.LabelSelectorAsSelector(selector)
			if err != nil {
				return ResourceDiagnostics{}, fmt.Errorf("build diagnostic selector for %s/%s: %w", kind, name, err)
			}
			pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector.String()})
			if err != nil {
				return ResourceDiagnostics{}, diagnose(fmt.Sprintf("list pods for %s/%s in namespace %q", kind, name, namespace), err)
			}
			for index := range pods.Items {
				pod := &pods.Items[index]
				relatedPods[pod.Name] = struct{}{}
				report.Causes = append(report.Causes, podDiagnosticCauses(pod)...)
			}
		}
	}
	events, err := s.ListWarnings(ctx, namespace)
	if err != nil {
		return ResourceDiagnostics{}, err
	}
	for _, event := range events {
		_, relatedPod := relatedPods[event.ObjectName]
		if (strings.EqualFold(event.ObjectKind, kind) && event.ObjectName == name) ||
			(strings.EqualFold(event.ObjectKind, "Pod") && relatedPod) {
			report.Events = append(report.Events, event)
			report.Causes = append(report.Causes, eventDiagnosticCause(event))
		}
	}
	report.Causes = uniqueStrings(report.Causes)
	switch {
	case len(report.Causes) > 0:
		report.Summary = report.Causes[0]
	case len(report.Events) > 0:
		report.Summary = "Kubernetes reported warning events for this resource."
	default:
		report.Summary = "No active warning signals were found."
	}
	return report, nil
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

func warningCountByObject(events []corev1.Event) map[string]int32 {
	counts := make(map[string]int32)
	for _, event := range events {
		if event.Type != corev1.EventTypeWarning {
			continue
		}
		count := event.Count
		if event.Series != nil && event.Series.Count > count {
			count = event.Series.Count
		}
		if count == 0 {
			count = 1
		}
		key := event.InvolvedObject.Kind + "\x00" + event.InvolvedObject.Name
		counts[key] += count
	}
	return counts
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

func diagnosticWorkloadSelector(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, kind, name string,
) (*metav1.LabelSelector, error) {
	switch strings.ToLower(kind) {
	case "deployment":
		item, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get deployment %q in namespace %q", name, namespace), err)
		}
		return item.Spec.Selector, nil
	case "statefulset":
		item, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get statefulset %q in namespace %q", name, namespace), err)
		}
		return item.Spec.Selector, nil
	case "daemonset":
		item, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get daemonset %q in namespace %q", name, namespace), err)
		}
		return item.Spec.Selector, nil
	default:
		return nil, fmt.Errorf("diagnostics for resource kind %q are not supported", kind)
	}
}

func podDiagnosticCauses(pod *corev1.Pod) []string {
	var causes []string
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			causes = append(causes, fmt.Sprintf("Container %s is waiting: %s.", status.Name, status.State.Waiting.Reason))
		}
		if status.LastTerminationState.Terminated != nil {
			terminated := status.LastTerminationState.Terminated
			causes = append(causes, fmt.Sprintf(
				"Container %s previously terminated with exit code %d (%s).",
				status.Name, terminated.ExitCode, valueOrUnknown(terminated.Reason),
			))
		}
		if status.RestartCount > 0 {
			causes = append(causes, fmt.Sprintf("Container %s has restarted %d times.", status.Name, status.RestartCount))
		}
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Status == corev1.ConditionFalse && condition.Reason != "" {
			causes = append(causes, fmt.Sprintf("%s condition is false: %s.", condition.Type, condition.Reason))
		}
	}
	return causes
}

func eventDiagnosticCause(event NamespaceEventSummary) string {
	switch strings.ToLower(event.Reason) {
	case "backoff":
		return "A container is repeatedly failing and Kubernetes is delaying restarts."
	case "unhealthy":
		return "A readiness, liveness, or startup probe is failing."
	case "failedscheduling":
		return "The scheduler cannot place a pod on an eligible node."
	case "failed", "failedcreate":
		return "Kubernetes could not create or run the requested resource."
	case "failedmount":
		return "A required volume could not be mounted."
	case "errimagepull", "imagepullbackoff":
		return "A container image could not be pulled."
	default:
		return fmt.Sprintf("%s: %s", event.Reason, event.Message)
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown reason"
	}
	return value
}
