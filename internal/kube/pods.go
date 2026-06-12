package kube

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type PodService interface {
	List(ctx context.Context, namespace string) ([]PodSummary, error)
	Describe(ctx context.Context, namespace, name string) (PodDetails, error)
	Containers(ctx context.Context, namespace, name string) ([]string, error)
	Ports(ctx context.Context, namespace, name string) ([]PodPort, error)
	Logs(ctx context.Context, options PodLogsOptions) (io.ReadCloser, error)
	ActionInfo(ctx context.Context, namespace, name string) (PodActionInfo, error)
	Delete(ctx context.Context, namespace, name string) error
}

type PodLogsOptions struct {
	Namespace  string
	Pod        string
	Container  string
	TailLines  int64
	Follow     bool
	Previous   bool
	Timestamps bool
}

type PodSummary struct {
	Name          string
	Ready         string
	Status        string
	Restarts      int32
	CreatedAt     time.Time
	Node          string
	LastRestartAt time.Time
	OwnerKind     string
	OwnerName     string
}

type PodActionInfo struct {
	Owners          []string
	ControllerOwner string
}

type PodPort struct {
	Container string
	Name      string
	Port      int32
	Protocol  string
}

type PodDetails struct {
	PodSummary
	Namespace      string
	ServiceAccount string
	PodIP          string
	HostIP         string
	QoSClass       string
	Owners         []string
	Conditions     []ConditionSummary
	Containers     []ContainerSummary
	Labels         []string
	Annotations    []string
	Volumes        []string
	Events         []EventSummary
	EventsWarning  string
}

type ContainerSummary struct {
	Name             string
	Image            string
	Ready            bool
	Restarts         int32
	State            string
	LastState        string
	Ports            []string
	Requests         []string
	Limits           []string
	Mounts           []string
	Probes           []string
	EnvironmentNames []string
}

type ConditionSummary struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

type EventSummary struct {
	Type      string
	Reason    string
	Message   string
	Count     int32
	FirstSeen time.Time
	LastSeen  time.Time
}

type Pods struct {
	factory   ClientFactory
	logStream podLogStream
}

func NewPodService() *Pods {
	return &Pods{factory: NewKubeconfigClientFactory(), logStream: streamPodLogs}
}

func NewPodServiceWithFactory(factory ClientFactory) *Pods {
	return &Pods{factory: factory, logStream: streamPodLogs}
}

func NewPodServiceWithFactoryAndLogStream(factory ClientFactory, stream podLogStream) *Pods {
	return &Pods{factory: factory, logStream: stream}
}

func (s *Pods) List(ctx context.Context, namespace string) ([]PodSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}

	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("list pods in namespace %q", namespace), err)
	}

	pods := make([]PodSummary, 0, len(list.Items))
	for index := range list.Items {
		pods = append(pods, summarizePod(&list.Items[index]))
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
	return pods, nil
}

func (s *Pods) Describe(ctx context.Context, namespace, name string) (PodDetails, error) {
	client, err := s.client()
	if err != nil {
		return PodDetails{}, err
	}

	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PodDetails{}, diagnose(fmt.Sprintf("get pod %q in namespace %q", name, namespace), err)
	}
	fieldSelector := fmt.Sprintf("involvedObject.kind=Pod,involvedObject.name=%s", name)
	if pod.UID != "" {
		fieldSelector += ",involvedObject.uid=" + string(pod.UID)
	}
	events, eventErr := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	details := describePod(pod, events.Items)
	if eventErr != nil {
		details.EventsWarning = diagnose(
			fmt.Sprintf("list events for pod %q in namespace %q", name, namespace),
			eventErr,
		).Error()
	}
	return details, nil
}

func (s *Pods) Containers(ctx context.Context, namespace, name string) ([]string, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}

	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("get pod %q in namespace %q", name, namespace), err)
	}

	names := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		names = append(names, container.Name)
	}
	return names, nil
}

func (s *Pods) Ports(ctx context.Context, namespace, name string) ([]PodPort, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}

	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("get pod %q in namespace %q", name, namespace), err)
	}

	var ports []PodPort
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			protocol := string(port.Protocol)
			if protocol == "" {
				protocol = string(corev1.ProtocolTCP)
			}
			ports = append(ports, PodPort{
				Container: container.Name,
				Name:      port.Name,
				Port:      port.ContainerPort,
				Protocol:  protocol,
			})
		}
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port == ports[j].Port {
			return ports[i].Container < ports[j].Container
		}
		return ports[i].Port < ports[j].Port
	})
	return ports, nil
}

func (s *Pods) Logs(ctx context.Context, options PodLogsOptions) (io.ReadCloser, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	stream, err := s.logStream(ctx, client, options)
	if err != nil {
		return nil, diagnose(
			fmt.Sprintf("stream logs for pod %q in namespace %q", options.Pod, options.Namespace),
			err,
		)
	}
	return stream, nil
}

func (s *Pods) ActionInfo(ctx context.Context, namespace, name string) (PodActionInfo, error) {
	client, err := s.client()
	if err != nil {
		return PodActionInfo{}, err
	}
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PodActionInfo{}, diagnose(fmt.Sprintf("get pod %q in namespace %q", name, namespace), err)
	}
	info := PodActionInfo{Owners: ownerNames(pod.OwnerReferences)}
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			info.ControllerOwner = owner.Kind + "/" + owner.Name
			break
		}
	}
	return info, nil
}

func (s *Pods) Delete(ctx context.Context, namespace, name string) error {
	client, err := s.client()
	if err != nil {
		return err
	}
	if err := client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return diagnose(fmt.Sprintf("delete pod %q in namespace %q", name, namespace), err)
	}
	return nil
}

type podLogStream func(context.Context, kubernetes.Interface, PodLogsOptions) (io.ReadCloser, error)

func streamPodLogs(
	ctx context.Context,
	client kubernetes.Interface,
	options PodLogsOptions,
) (io.ReadCloser, error) {
	tailLines := options.TailLines
	return client.CoreV1().Pods(options.Namespace).GetLogs(options.Pod, &corev1.PodLogOptions{
		Container:  options.Container,
		Follow:     options.Follow,
		Previous:   options.Previous,
		Timestamps: options.Timestamps,
		TailLines:  &tailLines,
	}).Stream(ctx)
}

func (s *Pods) client() (kubernetes.Interface, error) {
	client, err := s.factory.Client()
	if err != nil {
		return nil, err
	}
	return client, nil
}

func summarizePod(pod *corev1.Pod) PodSummary {
	ready := 0
	var restarts int32
	var lastRestartAt time.Time
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
		if terminated := status.LastTerminationState.Terminated; terminated != nil &&
			terminated.FinishedAt.Time.After(lastRestartAt) {
			lastRestartAt = terminated.FinishedAt.Time
		}
	}

	ownerKind, ownerName := controllerOwner(pod.OwnerReferences)
	return PodSummary{
		Name:          pod.Name,
		Ready:         fmt.Sprintf("%d/%d", ready, len(pod.Spec.Containers)),
		Status:        podStatus(pod),
		Restarts:      restarts,
		CreatedAt:     pod.CreationTimestamp.Time,
		Node:          pod.Spec.NodeName,
		LastRestartAt: lastRestartAt,
		OwnerKind:     ownerKind,
		OwnerName:     ownerName,
	}
}

func describePod(pod *corev1.Pod, events []corev1.Event) PodDetails {
	details := PodDetails{
		PodSummary:     summarizePod(pod),
		Namespace:      pod.Namespace,
		ServiceAccount: pod.Spec.ServiceAccountName,
		PodIP:          pod.Status.PodIP,
		HostIP:         pod.Status.HostIP,
		QoSClass:       string(pod.Status.QOSClass),
		Owners:         ownerNames(pod.OwnerReferences),
		Labels:         sortedPairs(pod.Labels),
		Annotations:    sortedKeys(pod.Annotations),
		Conditions:     conditionSummaries(pod.Status.Conditions),
		Events:         eventSummaries(events),
	}

	statuses := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for _, status := range pod.Status.ContainerStatuses {
		statuses[status.Name] = status
	}
	for _, container := range pod.Spec.Containers {
		status := statuses[container.Name]
		details.Containers = append(details.Containers, ContainerSummary{
			Name:             container.Name,
			Image:            container.Image,
			Ready:            status.Ready,
			Restarts:         status.RestartCount,
			State:            containerState(status.State),
			LastState:        containerState(status.LastTerminationState),
			Ports:            containerPorts(container.Ports),
			Requests:         resourceList(container.Resources.Requests),
			Limits:           resourceList(container.Resources.Limits),
			Mounts:           volumeMounts(container.VolumeMounts),
			Probes:           probeSummaries(container),
			EnvironmentNames: environmentNames(container.Env),
		})
	}
	for _, volume := range pod.Spec.Volumes {
		details.Volumes = append(details.Volumes, fmt.Sprintf("%s (%s)", volume.Name, volumeType(volume)))
	}
	return details
}

func conditionSummaries(conditions []corev1.PodCondition) []ConditionSummary {
	summaries := make([]ConditionSummary, 0, len(conditions))
	for _, condition := range conditions {
		summaries = append(summaries, ConditionSummary{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return summaries
}

func eventSummaries(events []corev1.Event) []EventSummary {
	summaries := make([]EventSummary, 0, len(events))
	for _, event := range events {
		firstSeen := event.FirstTimestamp.Time
		lastSeen := event.LastTimestamp.Time
		if event.EventTime.Time.After(lastSeen) {
			lastSeen = event.EventTime.Time
		}
		summaries = append(summaries, EventSummary{
			Type:      event.Type,
			Reason:    event.Reason,
			Message:   event.Message,
			Count:     event.Count,
			FirstSeen: firstSeen,
			LastSeen:  lastSeen,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].LastSeen.Before(summaries[j].LastSeen)
	})
	return summaries
}

func containerState(state corev1.ContainerState) string {
	switch {
	case state.Waiting != nil:
		return joinDetails("waiting", state.Waiting.Reason)
	case state.Running != nil:
		return "running"
	case state.Terminated != nil:
		return joinDetails(
			"terminated",
			state.Terminated.Reason,
			fmt.Sprintf("exitCode=%d", state.Terminated.ExitCode),
		)
	default:
		return "-"
	}
}

func containerPorts(ports []corev1.ContainerPort) []string {
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		name := ""
		if port.Name != "" {
			name = port.Name + "="
		}
		values = append(values, fmt.Sprintf("%s%d/%s", name, port.ContainerPort, port.Protocol))
	}
	sort.Strings(values)
	return values
}

func resourceList(resources corev1.ResourceList) []string {
	values := make([]string, 0, len(resources))
	for name, quantity := range resources {
		values = append(values, fmt.Sprintf("%s=%s", name, quantity.String()))
	}
	sort.Strings(values)
	return values
}

func volumeMounts(mounts []corev1.VolumeMount) []string {
	values := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		mode := "rw"
		if mount.ReadOnly {
			mode = "ro"
		}
		values = append(values, fmt.Sprintf("%s from %s (%s)", mount.MountPath, mount.Name, mode))
	}
	sort.Strings(values)
	return values
}

func probeSummaries(container corev1.Container) []string {
	var probes []string
	if container.LivenessProbe != nil {
		probes = append(probes, "liveness: "+probeSummary(container.LivenessProbe))
	}
	if container.ReadinessProbe != nil {
		probes = append(probes, "readiness: "+probeSummary(container.ReadinessProbe))
	}
	if container.StartupProbe != nil {
		probes = append(probes, "startup: "+probeSummary(container.StartupProbe))
	}
	return probes
}

func probeSummary(probe *corev1.Probe) string {
	action := "unknown"
	switch {
	case probe.HTTPGet != nil:
		action = fmt.Sprintf("http-get %s:%s%s", probe.HTTPGet.Host, probe.HTTPGet.Port.String(), probe.HTTPGet.Path)
	case probe.TCPSocket != nil:
		action = fmt.Sprintf("tcp-socket %s:%s", probe.TCPSocket.Host, probe.TCPSocket.Port.String())
	case probe.Exec != nil:
		action = "exec"
	case probe.GRPC != nil:
		action = fmt.Sprintf("grpc port=%d", probe.GRPC.Port)
	}
	return fmt.Sprintf(
		"%s delay=%ds timeout=%ds period=%ds success=%d failure=%d",
		action,
		probe.InitialDelaySeconds,
		probe.TimeoutSeconds,
		probe.PeriodSeconds,
		probe.SuccessThreshold,
		probe.FailureThreshold,
	)
}

func joinDetails(values ...string) string {
	var nonEmpty []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return strings.Join(nonEmpty, " | ")
}

func podStatus(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
		if status.State.Terminated != nil && status.State.Terminated.Reason != "" {
			return status.State.Terminated.Reason
		}
	}
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	return string(pod.Status.Phase)
}

func ownerNames(references []metav1.OwnerReference) []string {
	owners := make([]string, 0, len(references))
	for _, reference := range references {
		owners = append(owners, reference.Kind+"/"+reference.Name)
	}
	sort.Strings(owners)
	return owners
}

func controllerOwner(references []metav1.OwnerReference) (string, string) {
	for _, reference := range references {
		if reference.Controller != nil && *reference.Controller {
			return reference.Kind, reference.Name
		}
	}
	return "", ""
}

func sortedPairs(values map[string]string) []string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)
	return pairs
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func environmentNames(values []corev1.EnvVar) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Name)
	}
	sort.Strings(names)
	return names
}

func volumeType(volume corev1.Volume) string {
	switch {
	case volume.Secret != nil:
		return "secret"
	case volume.ConfigMap != nil:
		return "configMap"
	case volume.PersistentVolumeClaim != nil:
		return "persistentVolumeClaim"
	case volume.EmptyDir != nil:
		return "emptyDir"
	case volume.Projected != nil:
		return "projected"
	case volume.HostPath != nil:
		return "hostPath"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%T", volume.VolumeSource), "VolumeSource")
	}
}
