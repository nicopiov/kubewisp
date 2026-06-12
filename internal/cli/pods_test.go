package cli

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
	"github.com/nicopiov/kubewisp/internal/selector"
)

type fakePods struct {
	list        []kube.PodSummary
	details     kube.PodDetails
	listErr     error
	describeErr error
	namespace   string
	name        string
	containers  []string
	ports       []kube.PodPort
	logs        string
	logOptions  kube.PodLogsOptions
}

func (f *fakePods) List(_ context.Context, namespace string) ([]kube.PodSummary, error) {
	f.namespace = namespace
	return f.list, f.listErr
}

func (f *fakePods) Describe(_ context.Context, namespace, name string) (kube.PodDetails, error) {
	f.namespace = namespace
	f.name = name
	return f.details, f.describeErr
}

func (f *fakePods) Containers(_ context.Context, namespace, name string) ([]string, error) {
	f.namespace = namespace
	f.name = name
	return f.containers, nil
}

func (f *fakePods) Logs(_ context.Context, options kube.PodLogsOptions) (io.ReadCloser, error) {
	f.logOptions = options
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func (f *fakePods) Ports(_ context.Context, namespace, name string) ([]kube.PodPort, error) {
	f.namespace = namespace
	f.name = name
	return f.ports, nil
}

type fakePortForwarder struct {
	options kubectl.PortForwardOptions
}

func (f *fakePortForwarder) PortForward(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.PortForwardOptions,
) error {
	f.options = options
	return nil
}

func TestPodsListUsesSelectedNamespace(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{list: []kube.PodSummary{{
		Name:      "api-abc",
		Ready:     "1/1",
		Status:    "Running",
		CreatedAt: time.Now().Add(-time.Hour),
		Node:      "node-a",
	}}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Pods: pods})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "pods", "list"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if pods.namespace != "api" {
		t.Fatalf("namespace = %q, want api", pods.namespace)
	}
	for _, expected := range []string{"NAME", "api-abc", "Running", "node-a"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestPodsDescribeDoesNotPrintEnvironmentValues(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{details: kube.PodDetails{
		PodSummary: kube.PodSummary{Name: "api-abc", Ready: "1/1", Status: "Running"},
		Namespace:  "api",
		PodIP:      "10.0.0.4",
		HostIP:     "10.0.0.1",
		QoSClass:   "Burstable",
		Conditions: []kube.ConditionSummary{{Type: "Ready", Status: "False", Reason: "ContainersNotReady"}},
		Containers: []kube.ContainerSummary{{
			Name:             "app",
			Image:            "example/api:v1",
			Ready:            true,
			State:            "waiting | CrashLoopBackOff",
			LastState:        "terminated | Error | exitCode=1",
			Requests:         []string{"cpu=100m"},
			Mounts:           []string{"/var/credentials from credentials (ro)"},
			EnvironmentNames: []string{"API_TOKEN"},
		}},
		Annotations: []string{"example.com/note"},
		Events: []kube.EventSummary{{
			Type:     "Warning",
			Reason:   "BackOff",
			Message:  "Back-off restarting failed container",
			Count:    4,
			LastSeen: time.Now().Add(-time.Minute),
		}},
		EventsWarning: "events access is limited",
	}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Pods: pods})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "pods", "describe", "api-abc"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, expected := range []string{
		"Name: api-abc",
		"Pod IP: 10.0.0.4",
		"Conditions:",
		"ContainersNotReady",
		"State: waiting | CrashLoopBackOff",
		"Last state: terminated | Error | exitCode=1",
		"Requests: cpu=100m",
		"Environment names: API_TOKEN",
		"Annotation names:",
		"Recent Events:",
		"events access is limited",
		"BackOff",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
	for _, forbidden := range []string{"must-not-appear", "private annotation value"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("output contains forbidden value %q:\n%s", forbidden, output.String())
		}
	}
}

func TestPodsDescribeInteractiveSelectsPod(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{
		list: []kube.PodSummary{{
			Name:     "api-abc",
			Ready:    "1/1",
			Status:   "Running",
			Restarts: 2,
		}},
		details: kube.PodDetails{
			PodSummary: kube.PodSummary{Name: "api-abc", Ready: "1/1", Status: "Running"},
			Namespace:  "api",
		},
	}
	terminalSelector := &fakeSelector{selected: "api-abc | Running | ready 1/1 | restarts 2"}
	command := NewRootCommand(Dependencies{
		Runner:   fakeRunner{},
		Pods:     pods,
		Selector: terminalSelector,
	})
	command.SetArgs([]string{"--config", path, "pods", "describe"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if pods.name != "api-abc" {
		t.Fatalf("described pod = %q, want api-abc", pods.name)
	}
	if terminalSelector.initial != terminalSelector.selected {
		t.Fatalf("initial selection = %q, want %q", terminalSelector.initial, terminalSelector.selected)
	}
}

func TestPodsLogsUsesSingleContainerAndDefaults(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{containers: []string{"app"}, logs: "hello\n"}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Pods: pods})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "pods", "logs", "api-abc"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := kube.PodLogsOptions{
		Namespace: "api",
		Pod:       "api-abc",
		Container: "app",
		TailLines: 200,
	}
	if !reflect.DeepEqual(pods.logOptions, want) {
		t.Fatalf("log options = %#v, want %#v", pods.logOptions, want)
	}
	if output.String() != "hello\n" {
		t.Fatalf("output = %q, want hello", output.String())
	}
}

func TestPodsLogsInteractiveSelectsPod(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{
		list: []kube.PodSummary{{
			Name:   "api-abc",
			Ready:  "1/1",
			Status: "Running",
		}},
		containers: []string{"app"},
		logs:       "hello\n",
	}
	terminalSelector := &fakeSelector{selected: "api-abc | Running | ready 1/1 | restarts 0"}
	command := NewRootCommand(Dependencies{
		Runner:   fakeRunner{},
		Pods:     pods,
		Selector: terminalSelector,
	})
	command.SetArgs([]string{"--config", path, "pods", "logs"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if pods.logOptions.Pod != "api-abc" {
		t.Fatalf("logs pod = %q, want api-abc", pods.logOptions.Pod)
	}
}

func TestPodsDescribeInteractiveCancellation(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{list: []kube.PodSummary{{Name: "api-abc"}}}
	command := NewRootCommand(Dependencies{
		Runner:   fakeRunner{},
		Pods:     pods,
		Selector: &fakeSelector{err: selector.ErrCancelled},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "pods", "describe"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Pod selection cancelled.") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestPodsLogsSelectsContainerAndPassesFlags(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{containers: []string{"app", "sidecar"}, logs: "sidecar\n"}
	terminalSelector := &fakeSelector{selected: "sidecar"}
	command := NewRootCommand(Dependencies{
		Runner:   fakeRunner{},
		Pods:     pods,
		Selector: terminalSelector,
	})
	command.SetArgs([]string{
		"--config", path,
		"pods", "logs", "api-abc",
		"--tail", "50",
		"--follow",
		"--previous",
		"--timestamps",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if terminalSelector.initial != "app" {
		t.Fatalf("initial selection = %q, want app", terminalSelector.initial)
	}
	want := kube.PodLogsOptions{
		Namespace:  "api",
		Pod:        "api-abc",
		Container:  "sidecar",
		TailLines:  50,
		Follow:     true,
		Previous:   true,
		Timestamps: true,
	}
	if !reflect.DeepEqual(pods.logOptions, want) {
		t.Fatalf("log options = %#v, want %#v", pods.logOptions, want)
	}
}

func TestPodsLogsExplicitContainerSkipsDiscovery(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{logs: "hello\n"}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Pods: pods})
	command.SetArgs([]string{"--config", path, "pods", "logs", "api-abc", "-c", "app"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if pods.name != "" {
		t.Fatalf("container discovery pod = %q, want none", pods.name)
	}
	if pods.logOptions.Container != "app" {
		t.Fatalf("container = %q, want app", pods.logOptions.Container)
	}
}

func TestPodsPortForwardSelectsDeclaredPort(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{ports: []kube.PodPort{
		{Container: "app", Name: "http", Port: 8080, Protocol: "TCP"},
		{Container: "metrics", Name: "metrics", Port: 9090, Protocol: "TCP"},
	}}
	forwarder := &fakePortForwarder{}
	terminalSelector := &fakeSelector{selected: "metrics | 9090/TCP | container metrics"}
	command := NewRootCommand(Dependencies{
		Runner:      fakeRunner{},
		Pods:        pods,
		PortForward: forwarder,
		Selector:    terminalSelector,
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "pods", "port-forward", "api-abc"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := kubectl.PortForwardOptions{
		Namespace:  "api",
		Pod:        "api-abc",
		LocalPort:  9090,
		RemotePort: 9090,
	}
	if !reflect.DeepEqual(forwarder.options, want) {
		t.Fatalf("options = %#v, want %#v", forwarder.options, want)
	}
	if !strings.Contains(output.String(), "Forwarding localhost:9090") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestPodsPortForwardDirectPortsSkipDiscovery(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	pods := &fakePods{}
	forwarder := &fakePortForwarder{}
	command := NewRootCommand(Dependencies{
		Runner:      fakeRunner{},
		Pods:        pods,
		PortForward: forwarder,
	})
	command.SetArgs([]string{
		"--config", path,
		"pods", "port-forward", "api-abc",
		"--port", "8080",
		"--local-port", "18080",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if pods.name != "" {
		t.Fatalf("port discovery pod = %q, want none", pods.name)
	}
	if forwarder.options.LocalPort != 18080 || forwarder.options.RemotePort != 8080 {
		t.Fatalf("options = %#v", forwarder.options)
	}
}

func TestPodsPortForwardRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{
		Runner:      fakeRunner{},
		Pods:        &fakePods{},
		PortForward: &fakePortForwarder{},
	})
	command.SetArgs([]string{"--config", path, "pods", "port-forward", "api-abc", "--port", "70000"})

	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "--port must be between") {
		t.Fatalf("Execute() error = %v, want invalid port error", err)
	}
}

func TestFormatAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"30s": now.Add(-30 * time.Second),
		"12m": now.Add(-12 * time.Minute),
		"2h":  now.Add(-2 * time.Hour),
		"3d":  now.Add(-72 * time.Hour),
	}
	for want, createdAt := range cases {
		if got := formatAge(now, createdAt); got != want {
			t.Errorf("formatAge() = %q, want %q", got, want)
		}
	}
}
