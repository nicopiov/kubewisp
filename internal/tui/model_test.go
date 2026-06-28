package tui

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type fakeConnectivity struct {
	report kube.ConnectivityReport
	err    error
}

func (f fakeConnectivity) Check(context.Context, string) (kube.ConnectivityReport, error) {
	return f.report, f.err
}

type fakeNamespaces struct {
	names []string
}

type fakeDoctor struct {
	report doctor.Report
}

type fakeWorkloads struct {
	items           []kube.WorkloadSummary
	restarted       *string
	details         kube.CronJobDetails
	workloadDetails kube.WorkloadDetails
	rolloutProgress kube.RolloutProgress
	managedPods     []kube.PodSummary
	ownerWorkload   kube.WorkloadSummary
	suspended       *bool
}

type fakeEvents struct {
	items       []kube.NamespaceEventSummary
	listErr     error
	diagnostics kube.ResourceDiagnostics
}

type fakeResourceYAML struct {
	content string
	kind    string
	name    string
}

type fakeNetwork struct {
	items           []kube.NetworkSummary
	details         kube.NetworkDetails
	servicePods     []kube.PodSummary
	ingressServices []kube.NetworkSummary
}

func (f fakeNetwork) List(context.Context, string) ([]kube.NetworkSummary, error) {
	return f.items, nil
}

func (f fakeNetwork) Describe(context.Context, string, string, string) (kube.NetworkDetails, error) {
	return f.details, nil
}

func (f fakeNetwork) PodsForService(context.Context, string, string) ([]kube.PodSummary, error) {
	return f.servicePods, nil
}

func (f fakeNetwork) ServicesForIngress(context.Context, string, string) ([]kube.NetworkSummary, error) {
	return f.ingressServices, nil
}

func (f fakeEvents) ListWarnings(context.Context, string) ([]kube.NamespaceEventSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.items, nil
}

func (f fakeEvents) Diagnose(context.Context, string, string, string) (kube.ResourceDiagnostics, error) {
	return f.diagnostics, nil
}

func (f *fakeResourceYAML) Get(_ context.Context, _, kind, name string) (string, error) {
	f.kind = kind
	f.name = name
	return f.content, nil
}

func (f fakeWorkloads) List(context.Context, string) ([]kube.WorkloadSummary, error) {
	return f.items, nil
}

func (f fakeWorkloads) RolloutRestart(_ context.Context, _, kind, name string) error {
	if f.restarted != nil {
		*f.restarted = kind + "/" + name
	}
	return nil
}

func (f fakeWorkloads) RolloutProgress(context.Context, string, string, string) (kube.RolloutProgress, error) {
	return f.rolloutProgress, nil
}

func (f fakeWorkloads) DescribeCronJob(context.Context, string, string) (kube.CronJobDetails, error) {
	return f.details, nil
}

func (f fakeWorkloads) Describe(context.Context, string, string, string) (kube.WorkloadDetails, error) {
	return f.workloadDetails, nil
}

func (f fakeWorkloads) Pods(context.Context, string, string, string) ([]kube.PodSummary, error) {
	return f.managedPods, nil
}

func (f fakeWorkloads) OwnerForPod(context.Context, string, string) (kube.WorkloadSummary, error) {
	return f.ownerWorkload, nil
}

func (f fakeWorkloads) SetCronJobSuspended(_ context.Context, _, _ string, suspended bool) error {
	if f.suspended != nil {
		*f.suspended = suspended
	}
	return nil
}

func (f fakeDoctor) Run(context.Context) doctor.Report {
	return f.report
}

func (f fakeNamespaces) List(context.Context) ([]string, error) {
	return f.names, nil
}

func (f fakeNamespaces) Exists(context.Context, string) error {
	return nil
}

type fakePods struct {
	pods       []kube.PodSummary
	details    kube.PodDetails
	containers []string
	ports      []kube.PodPort
	logs       string
	actionInfo kube.PodActionInfo
	deleted    *bool
}

type fakePortForwarder struct {
	options kubectl.PortForwardOptions
	err     error
}

type fakeExecutor struct {
	options kubectl.ExecOptions
	err     error
}

type fakeProfileConnector struct {
	profile config.Profile
}

type fakeClipboard struct {
	copied string
	err    error
}

func (f *fakeClipboard) Copy(text string) error {
	f.copied = text
	return f.err
}

func (f *fakeProfileConnector) Connect(_ context.Context, profile config.Profile) error {
	f.profile = profile
	return nil
}

func (f *fakeExecutor) Exec(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.ExecOptions,
) error {
	f.options = options
	return f.err
}

func (f *fakePortForwarder) PortForward(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.PortForwardOptions,
) error {
	f.options = options
	return f.err
}

func (f fakePods) List(context.Context, string) ([]kube.PodSummary, error) {
	return f.pods, nil
}

func (f fakePods) Describe(context.Context, string, string) (kube.PodDetails, error) {
	return f.details, nil
}

func (f fakePods) Containers(context.Context, string, string) ([]string, error) {
	return f.containers, nil
}

func (f fakePods) Ports(context.Context, string, string) ([]kube.PodPort, error) {
	return f.ports, nil
}

func (f fakePods) Logs(context.Context, kube.PodLogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func (f fakePods) ActionInfo(context.Context, string, string) (kube.PodActionInfo, error) {
	return f.actionInfo, nil
}

func (f fakePods) Delete(context.Context, string, string) error {
	if f.deleted != nil {
		*f.deleted = true
	}
	return nil
}

func testDependencies(t *testing.T) Dependencies {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentProfile = "staging"
	cfg.Profiles["staging"] = config.Profile{
		Provider:         config.ProviderGKE,
		ProjectID:        "company-staging",
		ClusterName:      "staging-main",
		LocationType:     config.LocationRegion,
		Location:         "europe-west1",
		DefaultNamespace: "api",
	}
	cfg.Profiles["production"] = config.Profile{
		Provider: config.ProviderGKE, ProjectID: "company-production", ClusterName: "production-main",
		LocationType: config.LocationRegion, Location: "europe-west1", DefaultNamespace: "default", Production: true,
	}
	if err := (config.Store{Path: path}).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return Dependencies{
		ConfigPath:  path,
		ProfileName: "staging",
		Profile:     cfg.Profiles["staging"],
		Connectivity: fakeConnectivity{report: kube.ConnectivityReport{
			ServerVersion: "v1.32.1",
			Namespace:     "api",
		}},
		Namespaces: fakeNamespaces{names: []string{"api", "workers"}},
		Workloads: fakeWorkloads{items: []kube.WorkloadSummary{{
			Kind: "Deployment", Name: "api", Ready: 2, Desired: 3, Updated: 3, Available: 2,
		}, {
			Kind: "StatefulSet", Name: "db", Ready: 3, Desired: 3, Updated: 3, Available: 3,
		}, {
			Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *", Active: 1, Suspended: true,
		}}, details: kube.CronJobDetails{
			WorkloadSummary: kube.WorkloadSummary{
				Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *", Active: 1, Suspended: true,
			},
			ConcurrencyPolicy: "Forbid",
			Jobs: []kube.JobSummary{{
				Name: "cleanup-123", Status: "Completed", Succeeded: 1,
			}},
		}, workloadDetails: kube.WorkloadDetails{
			WorkloadSummary: kube.WorkloadSummary{
				Kind: "Deployment", Name: "api", Ready: 2, Desired: 3, Updated: 3, Available: 2,
			},
			Strategy:       "RollingUpdate",
			Selector:       "app=api",
			ServiceAccount: "api",
			Containers:     []string{"app | image=example/api:v1"},
			Conditions: []kube.WorkloadCondition{{
				Type: "Available", Status: "True", Reason: "MinimumReplicasAvailable",
			}},
		}, rolloutProgress: kube.RolloutProgress{
			WorkloadSummary: kube.WorkloadSummary{
				Kind: "Deployment", Name: "api", Ready: 2, Desired: 3, Updated: 2, Available: 2,
			},
			Generation: 4, ObservedGeneration: 4, Revision: "7",
			Status: "Progressing", Reason: "ReplicaSetUpdated",
			Message: "ReplicaSet api-new is progressing.",
			Pods:    []kube.PodSummary{{Name: "api-new", Ready: "1/1", Status: "Running"}},
		}, managedPods: []kube.PodSummary{{
			Name: "api-abc", Ready: "1/1", Status: "Running",
		}}},
		Network: fakeNetwork{items: []kube.NetworkSummary{{
			Kind: "Service", Name: "api", Type: "ClusterIP", Address: "10.0.0.1",
			Ports: []string{"http:80/TCP -> 8080"},
		}, {
			Kind: "Ingress", Name: "api", Type: "gce", Hosts: []string{"api.example.com"},
		}}, details: kube.NetworkDetails{
			NetworkSummary: kube.NetworkSummary{
				Kind: "Service", Name: "api", Type: "ClusterIP", Address: "10.0.0.1",
				Ports: []string{"http:80/TCP -> 8080"},
			},
			Selector:  []string{"app=api"},
			Endpoints: []string{"10.2.0.5"},
		}},
		Events: fakeEvents{items: []kube.NamespaceEventSummary{{
			ObjectKind: "Pod",
			ObjectName: "worker-abc",
			Reason:     "BackOff",
			Message:    "restarting failed container",
			Count:      4,
			LastSeen:   time.Now().Add(-time.Minute),
		}, {
			ObjectKind: "Deployment",
			ObjectName: "api",
			Reason:     "FailedCreate",
			Message:    "quota exceeded",
			Count:      2,
			LastSeen:   time.Now().Add(-time.Hour),
		}}},
		YAML: &fakeResourceYAML{content: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: api-abc\n"},
		Doctor: fakeDoctor{report: doctor.Report{Checks: []doctor.Check{{
			Dependency: doctor.Dependency{Name: "gcloud", Description: "Google Cloud CLI"},
			Path:       "/usr/local/bin/gcloud",
		}}}},
		PortForward: &fakePortForwarder{},
		Exec:        &fakeExecutor{},
		Profiles:    &fakeProfileConnector{},
		Clipboard:   &fakeClipboard{},
		Pods: fakePods{pods: []kube.PodSummary{{
			Name:      "api-abc",
			Ready:     "1/1",
			Status:    "Running",
			CreatedAt: time.Now().Add(-time.Hour),
		}, {
			Name:     "worker-abc",
			Ready:    "0/1",
			Status:   "CrashLoopBackOff",
			Restarts: 4,
		}}, details: kube.PodDetails{
			PodSummary: kube.PodSummary{Name: "api-abc", Ready: "1/1", Status: "Running"},
			Namespace:  "api",
			Owners:     []string{"ReplicaSet/api-123"},
			Conditions: []kube.ConditionSummary{{Type: "Ready", Status: "True"}},
			Volumes:    []string{"config (configMap)"},
			Labels:     []string{"app=api"},
			Containers: []kube.ContainerSummary{{
				Name:             "app",
				Image:            "example/api:v1",
				Ready:            true,
				State:            "running",
				Requests:         []string{"cpu=100m"},
				EnvironmentNames: []string{"API_TOKEN"},
			}},
		}, containers: []string{"app"}, ports: []kube.PodPort{{
			Container: "app",
			Name:      "http",
			Port:      8080,
			Protocol:  "TCP",
		}}, logs: "hello from app\n"},
	}
}

func TestProfileScreenRenamesDeletesAndSwitches(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	model := NewModel(dependencies)
	model.loading = false

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	updated, _ = updated.(Model).Update(command())
	profiles := updated.(Model)
	if profiles.screen != profileScreen || !strings.Contains(profiles.View(), "staging (active)") {
		t.Fatalf("unexpected profile screen:\n%s", profiles.View())
	}

	profiles.cursor = 0
	updated, _ = profiles.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	rename := updated.(Model)
	updated, _ = rename.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("prod")})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	renamed := updated.(Model)
	if !strings.Contains(renamed.status, "renamed to prod") {
		t.Fatalf("rename status = %q", renamed.status)
	}

	renamed.cursor = 0
	updated, _ = renamed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, _ = updated.(Model).Update(command())
	deleted := updated.(Model)
	if strings.Contains(deleted.View(), "\n> prod") || strings.Contains(deleted.View(), "\n  prod") {
		t.Fatalf("deleted profile remains:\n%s", deleted.View())
	}

}

func TestProfileScreenSwitchesLiveAndClearsClusterData(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false
	model.pods = []kube.PodSummary{{Name: "old-cluster-pod"}}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	updated, _ = updated.(Model).Update(command())
	profiles := updated.(Model)
	profiles.cursor = 0

	updated, command = profiles.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, command = updated.(Model).Update(command())
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	switched := updated.(Model)
	if switched.dependencies.ProfileName != "production" || switched.screen != dashboardScreen ||
		len(switched.pods) != 2 {
		t.Fatalf("switched model = %#v", switched)
	}
	connector := dependenciesProfileConnector(switched.dependencies)
	if connector.profile.ClusterName != "production-main" {
		t.Fatalf("connected profile = %#v", connector.profile)
	}
}

func dependenciesProfileConnector(dependencies Dependencies) *fakeProfileConnector {
	return dependencies.Profiles.(*fakeProfileConnector)
}

func TestPodEnterLoadsDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != podDetailsScreen {
		t.Fatalf("screen = %d, want podDetailsScreen", final.screen)
	}
	if !strings.Contains(final.View(), "Pod Details: api-abc") {
		t.Fatalf("view does not contain pod details:\n%s", final.View())
	}
}

func TestPodLogsLoadsSingleContainer(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	message := command()
	updated, command = updated.(Model).Update(message)
	message = command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != podLogsScreen {
		t.Fatalf("screen = %d, want podLogsScreen", final.screen)
	}
	if !strings.Contains(final.View(), "hello from app") {
		t.Fatalf("view does not contain logs:\n%s", final.View())
	}
}

func TestPodPortForwardSelectsDeclaredPort(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	chooser := updated.(Model)

	if chooser.screen != portForwardScreen || !strings.Contains(chooser.View(), "http | 8080/TCP") {
		t.Fatalf("unexpected port-forward chooser:\n%s", chooser.View())
	}
	updated, command = chooser.Update(tea.KeyMsg{Type: tea.KeyEnter})
	forwarding := updated.(Model)
	if command == nil {
		t.Fatal("port-forward selection did not start an external command")
	}

	updated, _ = forwarding.Update(portForwardFinishedMsg{err: errors.New("signal: interrupt")})
	final := updated.(Model)
	if final.screen != podScreen || final.status != "Port-forward stopped" || final.err != nil {
		t.Fatalf("final model = %#v", final)
	}
}

func TestPodPortForwardFailureReturnsToPodsWithError(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = portForwardScreen

	updated, _ := model.Update(portForwardFinishedMsg{err: errors.New("address already in use")})
	final := updated.(Model)

	if final.screen != podScreen || final.err == nil || !strings.Contains(final.View(), "address already in use") {
		t.Fatalf("unexpected model after port-forward failure:\n%s", final.View())
	}
}

func TestPodExecShowsContextAndReturnsToPods(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	confirm := updated.(Model)

	if confirm.screen != execConfirmScreen {
		t.Fatalf("screen = %d, want execConfirmScreen", confirm.screen)
	}
	for _, expected := range []string{"Exec target:", "Project: company-staging", "Pod: api-abc", "Container: app"} {
		if !strings.Contains(confirm.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, confirm.View())
		}
	}

	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not start non-production exec")
	}
	updated, _ = updated.(Model).Update(execFinishedMsg{})
	final := updated.(Model)
	if final.screen != podScreen || final.status != "Exec session ended" {
		t.Fatalf("final model = %#v", final)
	}
}

func TestProductionPodExecRequiresY(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	model := NewModel(dependencies)
	model.screen = execConfirmScreen
	model.selectedPod = "api-abc"
	model.selectedContainer = "app"

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || updated.(Model).screen != execConfirmScreen {
		t.Fatal("Enter started production exec")
	}
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("y did not start production exec")
	}
}

func TestPodRestartRequiresControllerAndDeletesAfterConfirmation(t *testing.T) {
	t.Parallel()

	deleted := false
	dependencies := testDependencies(t)
	pods := dependencies.Pods.(fakePods)
	pods.actionInfo = kube.PodActionInfo{ControllerOwner: "ReplicaSet/api-123"}
	pods.deleted = &deleted
	dependencies.Pods = pods
	model := NewModel(dependencies)
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	updated, _ = updated.(Model).Update(command())
	confirm := updated.(Model)
	if confirm.screen != podActionConfirmScreen || !strings.Contains(confirm.View(), "controller recreates it") {
		t.Fatalf("unexpected restart confirmation:\n%s", confirm.View())
	}
	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, _ = updated.(Model).Update(command())
	if !deleted {
		t.Fatal("confirmed restart did not delete pod")
	}
}

func TestPodRestartBlocksUnmanagedPodInTUI(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)
	if final.screen != podScreen || final.err == nil || !strings.Contains(final.View(), "no controller owner") {
		t.Fatalf("unexpected unmanaged restart result:\n%s", final.View())
	}
}

func TestProductionPodDeleteRequiresExactNameInTUI(t *testing.T) {
	t.Parallel()

	deleted := false
	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	pods := dependencies.Pods.(fakePods)
	pods.deleted = &deleted
	dependencies.Pods = pods
	model := NewModel(dependencies)
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, _ = updated.(Model).Update(command())
	confirm := updated.(Model)
	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("wrong")})
	if command != nil || deleted {
		t.Fatal("wrong production confirmation deleted pod")
	}
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	confirm = updated.(Model)
	confirm.confirmationInput = ""
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("api-abc")})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exact production confirmation did not create delete command")
	}
	updated, _ = updated.(Model).Update(command())
	if !deleted {
		t.Fatal("exact production confirmation did not delete pod")
	}
}

func TestEscapeReturnsFromPodDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podDetailsScreen

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	final := updated.(Model)

	if final.screen != podScreen {
		t.Fatalf("screen = %d, want podScreen", final.screen)
	}
}

func TestPodLogsScrolls(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podLogsScreen
	model.height = 12
	model.loading = false
	model.logs = "one\ntwo\nthree\nfour\nfive\nsix"

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	final := updated.(Model)

	if final.scroll != 1 {
		t.Fatalf("scroll = %d, want 1", final.scroll)
	}
	if !strings.Contains(final.View(), "lines 2-") {
		t.Fatalf("view does not contain scroll position:\n%s", final.View())
	}
}

func TestDashboardLoadsConnectivity(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	message := model.Init()()
	updated, command := model.Update(message)
	message = command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.connectivity.ServerVersion != "v1.32.1" {
		t.Fatalf("ServerVersion = %q, want v1.32.1", final.connectivity.ServerVersion)
	}
	for _, expected := range []string{
		"Profile: staging",
		"Cluster: staging-main",
		"Connection: connected",
		"Project: company-staging",
		"Location: europe-west1",
		"Local Dependencies",
		"pass gcloud",
		"/usr/local/bin/gcloud",
		"healthy 1",
		"unhealthy 1",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestDashboardCardsRespondToTerminalWidth(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false
	model.connectivity = kube.ConnectivityReport{ServerVersion: "v1.32.1"}
	model.pods = []kube.PodSummary{{Ready: "1/1", Status: "Running"}}
	model.doctorReport = testDependencies(t).Doctor.(fakeDoctor).report

	model.width = 120
	wide := model.dashboardView()
	if strings.Count(wide, "╭") != 3 || strings.Count(strings.Split(wide, "\n")[0], "╭") != 3 {
		t.Fatalf("wide dashboard cards are not side by side:\n%s", wide)
	}

	model.width = 80
	narrow := model.dashboardView()
	if strings.Count(strings.Split(narrow, "\n")[0], "╭") != 1 {
		t.Fatalf("narrow dashboard cards are not stacked:\n%s", narrow)
	}
}

func TestContextHelpCompactsAndWrapsAtTerminalWidth(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.width = 55

	help := model.responsiveHelpView()
	if !strings.Contains(help, "\n") || strings.Contains(help, "up/down navigate") {
		t.Fatalf("help was not compacted and wrapped:\n%s", help)
	}
	for _, line := range strings.Split(help, "\n") {
		if lipgloss.Width(line) > model.width {
			t.Fatalf("help line width = %d, want <= %d: %q", lipgloss.Width(line), model.width, line)
		}
	}
}

func TestContextHelpGroupsNavigationActionsAndGeneralCommands(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podDetailsScreen
	model.loading = false

	view := model.helpView()
	for _, expected := range []string{
		"Navigate:", "up/down scroll",
		"Actions:", "v diagnostics", "o owner",
		"General:", "esc back", "q quit",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("grouped help missing %q:\n%s", expected, view)
		}
	}
}

func TestContextHelpHighlightsKeysAndSticksToTerminalBottom(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.width = 100
	model.height = 24
	model.pods = []kube.PodSummary{{Name: "api", Ready: "1/1", Status: "Running"}}

	help := model.responsiveHelpView()
	if !helpKeyStyle.GetBold() || !helpGroupStyle.GetBold() ||
		!strings.Contains(help, "Navigate:") || !strings.Contains(help, "enter details") {
		t.Fatalf("help styles or content are not configured:\n%s", help)
	}
	if got := lipgloss.Height(model.View()); got != model.height {
		t.Fatalf("view height = %d, want terminal height %d:\n%s", got, model.height, model.View())
	}
}

func TestContextHelpHighlightsFirstActionAndGeneralCommand(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false

	help := model.responsiveHelpView()
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "Actions:") || strings.HasPrefix(line, "General:") {
			if strings.Contains(line, ":  ") {
				t.Fatalf("first command retains an unstyled leading blank: %q", line)
			}
		}
	}
}

func TestContentWrapsToTerminalWidthAndRemainsScrollable(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = resourceDiagnosticsScreen
	model.loading = false
	model.width = 36
	model.height = 12
	model.diagnostics = kube.ResourceDiagnostics{
		ResourceKind: "Pod",
		ResourceName: "api",
		Summary:      "This diagnostic summary contains important information that must remain visible on narrow terminals.",
		Causes:       []string{"A very long possible cause should wrap instead of disappearing beyond the terminal edge."},
	}

	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > model.width {
			t.Fatalf("wrapped line width = %d, want <= %d: %q", lipgloss.Width(line), model.width, line)
		}
	}
	if !strings.Contains(view, "lines 1-") {
		t.Fatalf("wrapped content is not vertically scrollable:\n%s", view)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !strings.Contains(updated.(Model).View(), "lines 2-") {
		t.Fatalf("scrolling did not advance through wrapped content:\n%s", updated.(Model).View())
	}
}

func TestResourceTablesAlignColumnsWithColoredAndVariableWidthValues(t *testing.T) {
	t.Parallel()

	view := podListView([]kube.PodSummary{
		{Name: "api", Ready: "1/1", Status: "Running", Restarts: 1},
		{Name: "long-worker-name", Ready: "0/2", Status: "CrashLoopBackOff", Restarts: 12, WarningCount: 3},
	}, 0)
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("table lines = %d, want 3:\n%s", len(lines), view)
	}
	wantSeparators := tableSeparatorPositions(lines[0])
	for _, line := range lines[1:] {
		if got := tableSeparatorPositions(line); !slices.Equal(got, wantSeparators) {
			t.Fatalf("separator positions = %v, want %v:\n%s", got, wantSeparators, view)
		}
	}
}

func tableSeparatorPositions(line string) []int {
	var positions []int
	offset := 0
	for {
		index := strings.Index(line[offset:], "|")
		if index < 0 {
			return positions
		}
		index += offset
		positions = append(positions, lipgloss.Width(line[:index]))
		offset = index + 1
	}
}

func TestDashboardAndProfileHelpAvoidDuplicateOrConflictingCommands(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false

	dashboardHelp := model.helpView()
	if strings.Contains(dashboardHelp, "Navigate:") ||
		strings.Count(dashboardHelp, "1-6 screens") != 1 ||
		!strings.Contains(dashboardHelp, "tab/left/right switch") {
		t.Fatalf("dashboard help is inconsistent:\n%s", dashboardHelp)
	}

	model.screen = profileScreen
	model.profileNames = []string{"staging"}
	profileHelp := model.helpView()
	for _, expected := range []string{"enter switch", "r rename", "d delete", "esc back", "q quit"} {
		if !strings.Contains(profileHelp, expected) {
			t.Fatalf("profile help missing %q:\n%s", expected, profileHelp)
		}
	}
	if strings.Contains(profileHelp, "r refresh") {
		t.Fatalf("profile help advertises conflicting refresh shortcut:\n%s", profileHelp)
	}
}

func TestListHelpTreatsEnterAsNavigationAndFilterAsAction(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false

	help := model.helpView()
	navigate, actions, _ := strings.Cut(help, "\n")
	if !strings.Contains(navigate, "enter details") || strings.Contains(navigate, "/ filter") {
		t.Fatalf("pod navigation group is inconsistent:\n%s", help)
	}
	if !strings.Contains(actions, "Actions:") || !strings.Contains(actions, "/ filter") {
		t.Fatalf("pod actions group is inconsistent:\n%s", help)
	}
}

func TestCtrlCCancelsTextInputAndConfirmationWithoutQuitting(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.filtering = true
	model.filterScreen = podScreen
	model.filterQuery = "api"
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	filtered := updated.(Model)
	if command != nil || filtered.filtering || filtered.filterQuery != "" {
		t.Fatalf("Ctrl+C did not cancel filtering: %#v", filtered)
	}

	model.screen = profileRenameScreen
	model.filtering = false
	model.profileInput = "new-name"
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	renaming := updated.(Model)
	if command != nil || renaming.screen != profileScreen || renaming.profileInput != "" {
		t.Fatalf("Ctrl+C did not cancel rename: %#v", renaming)
	}

	model.screen = podActionConfirmScreen
	model.dependencies.Profile.Production = false
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	confirming := updated.(Model)
	if command != nil || confirming.screen != podScreen {
		t.Fatalf("Ctrl+C did not cancel confirmation: %#v", confirming)
	}

	model.screen = workloadRestartConfirmScreen
	model.dependencies.Profile.Production = true
	model.confirmationInput = "Deployment/api"
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	productionConfirm := updated.(Model)
	if command != nil || productionConfirm.screen != workloadScreen || productionConfirm.confirmationInput != "" {
		t.Fatalf("Ctrl+C did not cancel production confirmation: %#v", productionConfirm)
	}
}

func TestDashboardShowsMissingDependencyGuidance(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	dependencies.Doctor = fakeDoctor{report: doctor.Report{Checks: []doctor.Check{{
		Dependency: doctor.Dependency{Name: "kubectl", Description: "Kubernetes command-line tool"},
		Err:        errors.New("not found"),
	}}}}
	model := NewModel(dependencies)
	message := model.Init()()
	updated, command := model.Update(message)
	message = command()
	updated, _ = updated.(Model).Update(message)

	view := updated.(Model).View()
	for _, expected := range []string{"fail kubectl", "not found", "Run kubewisp doctor for", "details"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("dashboard missing %q:\n%s", expected, view)
		}
	}
}

func TestPodListShowsHealthMarkers(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.pods = []kube.PodSummary{
		{Name: "api", Ready: "1/1", Status: "Running", Restarts: 8},
		{Name: "recent", Ready: "1/1", Status: "Running", Restarts: 1, LastRestartAt: time.Now().Add(-time.Minute)},
		{Name: "worker", Ready: "0/1", Status: "CrashLoopBackOff", Restarts: 4},
		{Name: "cleanup-running", Ready: "1/1", Status: "Running", OwnerKind: "Job"},
		{Name: "cleanup-pending", Ready: "0/1", Status: "Pending", OwnerKind: "Job"},
		{Name: "cleanup-complete", Ready: "0/1", Status: "Completed", OwnerKind: "Job"},
	}

	view := model.View()
	for _, expected := range []string{
		"● healthy", "● warning", "● unhealthy", "● running", "● pending", "● completed",
		"api", "recent", "worker", "cleanup-running", "cleanup-pending", "cleanup-complete",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestDashboardCountsCompletedPodsSeparately(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false
	model.pods = []kube.PodSummary{
		{Ready: "1/1", Status: "Running"},
		{Ready: "0/1", Status: "Completed", OwnerKind: "Job"},
	}

	view := model.View()
	for _, expected := range []string{"healthy 1", "completed 1", "warning 0", "unhealthy 0"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("dashboard missing %q:\n%s", expected, view)
		}
	}
}

func TestPodHealthIgnoresHistoricalRestartsButWarnsOnRecentRestart(t *testing.T) {
	t.Parallel()

	historical := kube.PodSummary{
		Ready:         "1/1",
		Status:        "Running",
		Restarts:      8,
		LastRestartAt: time.Now().Add(-time.Hour),
	}
	recent := historical
	recent.LastRestartAt = time.Now().Add(-time.Minute)

	if got := podHealthLevel(historical); got != podHealthy {
		t.Fatalf("historical restart health = %d, want healthy", got)
	}
	if got := podHealthLevel(recent); got != podWarning {
		t.Fatalf("recent restart health = %d, want warning", got)
	}
}

func TestPodDetailsIncludesTroubleshootingSections(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podDetailsScreen
	model.loading = false
	model.podDetails = testDependencies(t).Pods.(fakePods).details

	view := model.View()
	for _, expected := range []string{
		"● healthy",
		"Owners:",
		"ReplicaSet/api-123",
		"Conditions:",
		"Image: example/api:v1",
		"Requests: cpu=100m",
		"Environment names: API_TOKEN",
		"Volumes:",
		"Labels:",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestNavigateToPodsAndLoadList(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != podScreen {
		t.Fatalf("screen = %d, want podScreen", final.screen)
	}
	if !strings.Contains(final.View(), "api-abc") {
		t.Fatalf("view does not contain pod:\n%s", final.View())
	}
}

func TestListFilterSelectsVisibleResourceAndPersistsAcrossDetails(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	model := NewModel(dependencies)
	model.screen = podScreen
	model.loading = false
	model.pods = dependencies.Pods.(fakePods).pods

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("worker")})
	filtering := updated.(Model)
	if !filtering.filtering || filtering.itemCount() != 1 ||
		!strings.Contains(filtering.View(), "Filter /worker (editing, 1 of 2)") ||
		strings.Contains(filtering.View(), "api-abc") {
		t.Fatalf("unexpected live filter view:\n%s", filtering.View())
	}

	updated, _ = filtering.Update(tea.KeyMsg{Type: tea.KeyEnter})
	applied := updated.(Model)
	if applied.filtering || applied.filterQuery != "worker" {
		t.Fatalf("filter was not kept after Enter: %#v", applied)
	}
	updated, command := applied.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || updated.(Model).selectedPod != "worker-abc" {
		t.Fatalf("filtered selection did not open worker-abc: %#v", updated.(Model))
	}
	updated, _ = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	back := updated.(Model)
	if back.screen != podScreen || back.filterQuery != "worker" || strings.Contains(back.View(), "api-abc") {
		t.Fatalf("filter did not persist across details:\n%s", back.View())
	}
	updated, _ = back.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cleared := updated.(Model)
	if cleared.filterQuery != "" || !strings.Contains(cleared.View(), "api-abc") {
		t.Fatalf("Esc did not clear applied filter:\n%s", cleared.View())
	}
}

func TestListFilterMatchesResourceMetadata(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	tests := []struct {
		name   string
		screen screen
		query  string
		setup  func(*Model)
		want   int
	}{
		{
			name: "namespace", screen: namespaceScreen, query: "workers", want: 1,
			setup: func(model *Model) { model.namespaces = []string{"api", "workers"} },
		},
		{
			name: "workload kind", screen: workloadScreen, query: "cronjob", want: 1,
			setup: func(model *Model) { model.workloads = dependencies.Workloads.(fakeWorkloads).items },
		},
		{
			name: "network host", screen: networkScreen, query: "api.example.com", want: 1,
			setup: func(model *Model) { model.networkResources = dependencies.Network.(fakeNetwork).items },
		},
		{
			name: "event message", screen: eventScreen, query: "quota", want: 1,
			setup: func(model *Model) { model.events = dependencies.Events.(fakeEvents).items },
		},
		{
			name: "related pod status", screen: servicePodsScreen, query: "running", want: 1,
			setup: func(model *Model) {
				model.servicePods = []kube.PodSummary{
					{Name: "api", Status: "Running"},
					{Name: "worker", Status: "Pending"},
				}
			},
		},
		{
			name: "backend service type", screen: ingressServicesScreen, query: "clusterip", want: 1,
			setup: func(model *Model) {
				model.ingressServices = []kube.NetworkSummary{{Name: "api", Type: "ClusterIP"}}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			model := NewModel(dependencies)
			model.screen = test.screen
			model.loading = false
			test.setup(&model)
			model.filterScreen = test.screen
			model.filterQuery = test.query
			if got := model.itemCount(); got != test.want {
				t.Fatalf("itemCount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestListFilterShowsNoMatches(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	model := NewModel(dependencies)
	model.screen = podScreen
	model.loading = false
	model.pods = dependencies.Pods.(fakePods).pods
	model.filterScreen = podScreen
	model.filterQuery = "does-not-exist"

	if !strings.Contains(model.View(), `No resources match filter "does-not-exist".`) {
		t.Fatalf("missing no-match state:\n%s", model.View())
	}
}

func TestNetworkScreenShowsResourcesAndOpensDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	updated, _ = updated.(Model).Update(command())
	network := updated.(Model)
	for _, expected := range []string{"[Network]", "Service", "10.0.0.1", "Ingress", "api.example.com"} {
		if !strings.Contains(network.View(), expected) {
			t.Fatalf("network view does not contain %q:\n%s", expected, network.View())
		}
	}
	network.cursor = 1
	updated, command = network.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	for _, expected := range []string{"Service Details: api", "app=api", "10.2.0.5", "http:80/TCP"} {
		if !strings.Contains(details.View(), expected) {
			t.Fatalf("network details do not contain %q:\n%s", expected, details.View())
		}
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != networkScreen {
		t.Fatalf("details escape screen = %d, want networkScreen", updated.(Model).screen)
	}
}

func TestFreshTabCacheAvoidsReloadUntilRefresh(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.loadedAt[networkScreen] = model.now()

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	cached := updated.(Model)
	if command != nil || cached.loading {
		t.Fatal("fresh Network tab triggered a reload")
	}
	updated, command = cached.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command == nil || !updated.(Model).loading {
		t.Fatal("explicit refresh did not reload cached Network tab")
	}
}

func TestEventsScreenShowsWarningsAndDrillsIntoPod(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})
	updated, _ = updated.(Model).Update(command())
	events := updated.(Model)

	for _, expected := range []string{"[Events]", "Pod/worker-abc", "BackOff", "restarting failed container"} {
		if !strings.Contains(events.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, events.View())
		}
	}
	updated, command = events.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("pod event did not load pod details")
	}
	if updated.(Model).screen != podDetailsScreen || updated.(Model).selectedPod != "worker-abc" {
		t.Fatalf("event drill-down model = %#v", updated.(Model))
	}
}

func TestWorkloadEventSelectsAffectedWorkload(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = eventScreen
	model.loading = false
	model.events = testDependencies(t).Events.(fakeEvents).items
	model.cursor = 1

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)

	if final.screen != workloadScreen || final.cursor != 0 || final.status != "Selected Deployment/api" {
		t.Fatalf("workload event drill-down model = %#v", final)
	}
}

func TestWorkloadsScreenShowsReplicaHealth(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)

	if final.screen != workloadScreen {
		t.Fatalf("screen = %d, want workloadScreen", final.screen)
	}
	for _, expected := range []string{
		"[Workloads]", "Deployment", "api", "ready 2/3", "CronJob", "cleanup",
		"0 * * * *", "active 1, suspended", "● suspended", "● warning", "● healthy",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestWorkloadHelpChangesWithSelectedResource(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{
		{Kind: "Deployment", Name: "api"},
		{Kind: "CronJob", Name: "cleanup"},
	}

	if view := model.View(); !strings.Contains(view, "R rollout restart") || strings.Contains(view, "s suspend/resume") {
		t.Fatalf("deployment help is not dynamic:\n%s", view)
	}
	model.cursor = 1
	if view := model.View(); !strings.Contains(view, "s suspend/resume") || strings.Contains(view, "R rollout restart") {
		t.Fatalf("CronJob help is not dynamic:\n%s", view)
	}
}

func TestReplicaWorkloadEnterLoadsDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not load workload details")
	}
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)

	if final.screen != workloadDetailsScreen {
		t.Fatalf("screen = %d, want workloadDetailsScreen", final.screen)
	}
	for _, want := range []string{
		"Deployment Details: api", "RollingUpdate", "app=api",
		"app | image=example/api:v1", "MinimumReplicasAvailable",
	} {
		if !strings.Contains(final.View(), want) {
			t.Fatalf("details missing %q:\n%s", want, final.View())
		}
	}
	for _, want := range []string{"Navigate:", "Actions:", "v diagnostics", "p managed pods", "General:"} {
		if !strings.Contains(final.helpView(), want) {
			t.Fatalf("details help missing %q:\n%s", want, final.helpView())
		}
	}
}

func TestWorkloadDetailsOpenManagedPodsAndPodDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadDetailsScreen
	model.loading = false
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if command == nil {
		t.Fatal("p did not load managed pods")
	}
	updated, _ = updated.(Model).Update(command())
	pods := updated.(Model)
	if pods.screen != workloadPodsScreen ||
		!strings.Contains(pods.View(), "Pods managed by Deployment/api") ||
		!strings.Contains(pods.View(), "api-abc") {
		t.Fatalf("unexpected managed pods view:\n%s", pods.View())
	}

	updated, command = pods.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	if details.screen != podDetailsScreen || details.selectedPod != "api-abc" {
		t.Fatalf("unexpected pod details model: %#v", details)
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadPodsScreen {
		t.Fatalf("details escape screen = %d, want workloadPodsScreen", updated.(Model).screen)
	}
}

func TestWorkloadDetailsOpenLiveRolloutProgress(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadDetailsScreen
	model.loading = false
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if command == nil {
		t.Fatal("w did not load rollout progress")
	}
	updated, command = updated.(Model).Update(command())
	progress := updated.(Model)
	if progress.screen != workloadRolloutScreen || command == nil {
		t.Fatalf("rollout monitor did not start polling: %#v", progress)
	}
	for _, want := range []string{
		"Rollout Progress: Deployment/api", "● progressing", "Ready: 2/3",
		"Generation: 4", "Revision: 7", "ReplicaSetUpdated", "api-new",
		"auto-refreshing every 2s",
	} {
		if !strings.Contains(progress.View(), want) {
			t.Fatalf("rollout progress missing %q:\n%s", want, progress.View())
		}
	}
	updated, command = progress.Update(rolloutProgressMsg{progress: kube.RolloutProgress{
		WorkloadSummary: kube.WorkloadSummary{
			Kind: "Deployment", Name: "api", Ready: 3, Desired: 3, Updated: 3, Available: 3,
		},
		Generation: 4, ObservedGeneration: 4, Complete: true, Status: "Complete",
	}})
	if command != nil || !strings.Contains(updated.(Model).View(), "● complete") {
		t.Fatalf("completed rollout kept polling or did not render complete:\n%s", updated.(Model).View())
	}
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadDetailsScreen {
		t.Fatalf("rollout escape screen = %d, want workloadDetailsScreen", updated.(Model).screen)
	}
}

func TestWorkloadRestartOpensRolloutProgress(t *testing.T) {
	t.Parallel()

	restarted := ""
	dependencies := testDependencies(t)
	workloads := dependencies.Workloads.(fakeWorkloads)
	workloads.restarted = &restarted
	dependencies.Workloads = workloads
	model := NewModel(dependencies)
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", Ready: 3, Desired: 3}}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	updated, command := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, command = updated.(Model).Update(command())
	restarting := updated.(Model)
	if restarted != "Deployment/api" || restarting.screen != workloadRolloutScreen || command == nil {
		t.Fatalf("restart did not open rollout monitor: %#v, restarted=%q", restarting, restarted)
	}
	updated, _ = restarting.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadScreen {
		t.Fatalf("restart rollout escape screen = %d, want workloadScreen", updated.(Model).screen)
	}
}

func TestRolloutMonitorStopsForCompleteAndStalledRollouts(t *testing.T) {
	t.Parallel()

	if !rolloutStillRunning(kube.RolloutProgress{Status: "Progressing"}) {
		t.Fatal("progressing rollout should keep refreshing")
	}
	if rolloutStillRunning(kube.RolloutProgress{Complete: true, Status: "Complete"}) {
		t.Fatal("complete rollout should stop refreshing")
	}
	if rolloutStillRunning(kube.RolloutProgress{Status: "Stalled"}) {
		t.Fatal("stalled rollout should stop refreshing")
	}
}

func TestServiceDetailsOpenSelectedPods(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	network := dependencies.Network.(fakeNetwork)
	network.servicePods = []kube.PodSummary{{Name: "api-abc", Ready: "1/1", Status: "Running"}}
	dependencies.Network = network
	model := NewModel(dependencies)
	model.screen = networkDetailsScreen
	model.loading = false
	model.selectedNetwork = kube.NetworkSummary{Kind: "Service", Name: "api"}
	model.networkDetails = kube.NetworkDetails{NetworkSummary: model.selectedNetwork}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	updated, _ = updated.(Model).Update(command())
	pods := updated.(Model)
	if pods.screen != servicePodsScreen || !strings.Contains(pods.View(), "Pods selected by Service/api") {
		t.Fatalf("unexpected Service pods view:\n%s", pods.View())
	}
	updated, command = pods.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != servicePodsScreen {
		t.Fatalf("pod details escape screen = %d, want servicePodsScreen", updated.(Model).screen)
	}
}

func TestIngressDetailsOpenBackendServices(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	network := dependencies.Network.(fakeNetwork)
	network.ingressServices = []kube.NetworkSummary{{Kind: "Service", Name: "api", Type: "ClusterIP"}}
	dependencies.Network = network
	model := NewModel(dependencies)
	model.screen = networkDetailsScreen
	model.loading = false
	model.selectedNetwork = kube.NetworkSummary{Kind: "Ingress", Name: "public"}
	model.networkDetails = kube.NetworkDetails{NetworkSummary: model.selectedNetwork}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	updated, _ = updated.(Model).Update(command())
	services := updated.(Model)
	if services.screen != ingressServicesScreen || !strings.Contains(services.View(), "Services used by Ingress/public") {
		t.Fatalf("unexpected Ingress Services view:\n%s", services.View())
	}
	updated, command = services.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	if details.screen != networkDetailsScreen || details.selectedNetwork.Name != "api" {
		t.Fatalf("unexpected backend Service details model: %#v", details)
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	final := updated.(Model)
	if final.screen != networkDetailsScreen || final.selectedNetwork.Kind != "Ingress" || final.selectedNetwork.Name != "public" {
		t.Fatalf("Ingress relationship back navigation failed: %#v", final)
	}
}

func TestPodDetailsOpenOwnerWorkload(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	workloads := dependencies.Workloads.(fakeWorkloads)
	workloads.ownerWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}
	dependencies.Workloads = workloads
	model := NewModel(dependencies)
	model.screen = podDetailsScreen
	model.loading = false
	model.selectedPod = "api-abc"
	model.podDetails = kube.PodDetails{PodSummary: kube.PodSummary{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	if details.screen != workloadDetailsScreen || details.selectedWorkload.Name != "api" {
		t.Fatalf("unexpected owner workload details model: %#v", details)
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != podDetailsScreen {
		t.Fatalf("owner details escape screen = %d, want podDetailsScreen", updated.(Model).screen)
	}
}

func TestPodAndWorkloadDetailsOpenDiagnostics(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	events := dependencies.Events.(fakeEvents)
	events.diagnostics = kube.ResourceDiagnostics{
		ResourceKind: "Pod",
		ResourceName: "api-abc",
		Summary:      "Container app is repeatedly failing.",
		Causes:       []string{"Container app previously terminated with exit code 1."},
		Events: []kube.NamespaceEventSummary{{
			ObjectKind: "Pod", ObjectName: "api-abc", Reason: "BackOff",
			Message: "Back-off restarting failed container", Count: 7,
		}},
	}
	dependencies.Events = events
	model := NewModel(dependencies)
	model.screen = podDetailsScreen
	model.loading = false
	model.selectedPod = "api-abc"

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	updated, _ = updated.(Model).Update(command())
	diagnostics := updated.(Model)
	if diagnostics.screen != resourceDiagnosticsScreen {
		t.Fatalf("screen = %d, want resourceDiagnosticsScreen", diagnostics.screen)
	}
	for _, want := range []string{
		"Diagnostics: Pod/api-abc", "Container app is repeatedly failing",
		"exit code 1", "BackOff", "count=7",
	} {
		if !strings.Contains(diagnostics.View(), want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, diagnostics.View())
		}
	}
	updated, _ = diagnostics.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != podDetailsScreen {
		t.Fatalf("diagnostics escape screen = %d, want podDetailsScreen", updated.(Model).screen)
	}

	model.screen = workloadDetailsScreen
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if command == nil || updated.(Model).diagnosticBackScreen != workloadDetailsScreen {
		t.Fatalf("workload diagnostics did not open: %#v", updated.(Model))
	}
}

func TestDetailsShowRelatedWarningEvents(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	dependencies.Events = fakeEvents{items: []kube.NamespaceEventSummary{{
		ObjectKind: "Pod",
		ObjectName: "api-abc",
		Reason:     "BackOff",
		Message:    "Back-off restarting failed container",
		Count:      4,
		LastSeen:   time.Now().Add(-time.Minute),
	}, {
		ObjectKind: "Deployment",
		ObjectName: "api",
		Reason:     "FailedCreate",
		Message:    "quota exceeded",
		Count:      2,
		LastSeen:   time.Now().Add(-2 * time.Minute),
	}, {
		ObjectKind: "Service",
		ObjectName: "api",
		Reason:     "Unhealthy",
		Message:    "no ready endpoints",
		Count:      1,
		LastSeen:   time.Now().Add(-3 * time.Minute),
	}, {
		ObjectKind: "Pod",
		ObjectName: "worker-abc",
		Reason:     "BackOff",
		Message:    "unrelated worker failure",
		Count:      7,
		LastSeen:   time.Now().Add(-4 * time.Minute),
	}}}

	model := NewModel(dependencies)
	model.screen = podDetailsScreen
	model.selectedPod = "api-abc"
	updated, _ := model.Update(model.loadPodDetails()())
	podDetails := updated.(Model)
	if !strings.Contains(podDetails.View(), "Related Warning Events") ||
		!strings.Contains(podDetails.View(), "Pod/api-abc | BackOff | count=4") ||
		strings.Contains(podDetails.View(), "worker-abc") {
		t.Fatalf("pod details related warnings unexpected:\n%s", podDetails.View())
	}

	model = NewModel(dependencies)
	model.screen = workloadDetailsScreen
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}
	model.workloadPods = []kube.PodSummary{{Name: "api-abc"}}
	updated, _ = model.Update(model.loadWorkloadDetails()())
	workloadDetails := updated.(Model)
	for _, want := range []string{
		"Deployment/api | FailedCreate | count=2",
		"Pod/api-abc | BackOff | count=4",
	} {
		if !strings.Contains(workloadDetails.View(), want) {
			t.Fatalf("workload details missing %q:\n%s", want, workloadDetails.View())
		}
	}
	if strings.Contains(workloadDetails.View(), "worker-abc") {
		t.Fatalf("workload details included unrelated pod warning:\n%s", workloadDetails.View())
	}

	model = NewModel(dependencies)
	model.screen = networkDetailsScreen
	model.selectedNetwork = kube.NetworkSummary{Kind: "Service", Name: "api"}
	updated, _ = model.Update(model.loadNetworkDetails()())
	networkDetails := updated.(Model)
	if !strings.Contains(networkDetails.View(), "Service/api | Unhealthy | count=1") {
		t.Fatalf("network details missing service warning:\n%s", networkDetails.View())
	}
}

func TestDetailsKeepRenderingWhenRelatedWarningsAreUnavailable(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	dependencies.Events = fakeEvents{listErr: errors.New("events forbidden")}
	model := NewModel(dependencies)
	model.screen = workloadDetailsScreen
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}

	updated, _ := model.Update(model.loadWorkloadDetails()())
	view := updated.(Model).View()
	if !strings.Contains(view, "Deployment Details: api") ||
		!strings.Contains(view, "Warning events unavailable: events forbidden") {
		t.Fatalf("details did not keep rendering with warning event error:\n%s", view)
	}
}

func TestResourceDetailsOpenYAMLPreview(t *testing.T) {
	t.Parallel()

	yaml := &fakeResourceYAML{
		content: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  replicas: 2\n",
	}
	dependencies := testDependencies(t)
	dependencies.YAML = yaml
	model := NewModel(dependencies)
	model.screen = workloadDetailsScreen
	model.loading = false
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}
	model.height = 20

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("y did not load resource YAML")
	}
	updated, _ = updated.(Model).Update(command())
	preview := updated.(Model)
	if preview.screen != resourceYAMLScreen || yaml.kind != "Deployment" || yaml.name != "api" {
		t.Fatalf("unexpected YAML preview state: model=%#v target=%s/%s", preview, yaml.kind, yaml.name)
	}
	for _, want := range []string{"YAML: Deployment/api", "apiVersion: apps/v1", "replicas: 2"} {
		if !strings.Contains(preview.View(), want) {
			t.Fatalf("YAML preview missing %q:\n%s", want, preview.View())
		}
	}

	updated, _ = preview.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadDetailsScreen {
		t.Fatalf("Esc screen = %d, want workloadDetailsScreen", updated.(Model).screen)
	}
}

func TestResourceYAMLPreviewScrolls(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = resourceYAMLScreen
	model.loading = false
	model.width = 80
	model.height = 12
	model.resourceYAMLKind = "Deployment"
	model.resourceYAMLName = "api"
	model.resourceYAML = strings.Join([]string{
		"apiVersion: apps/v1",
		"kind: Deployment",
		"metadata:",
		"  name: api",
		"spec:",
		"  replicas: 2",
	}, "\n")

	view := model.View()
	if !strings.Contains(view, "lines 1-") {
		t.Fatalf("YAML preview did not show scroll position:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolled := updated.(Model)
	if scrolled.scroll != 1 || !strings.Contains(scrolled.View(), "lines 2-") {
		t.Fatalf("YAML preview did not scroll: scroll=%d\n%s", scrolled.scroll, scrolled.View())
	}
	updated, _ = scrolled.Update(tea.KeyMsg{Type: tea.KeyUp})
	if updated.(Model).scroll != 0 {
		t.Fatalf("YAML preview did not scroll back up: scroll=%d", updated.(Model).scroll)
	}
}

func TestScrollableViewSearchJumpsBetweenMatches(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = resourceYAMLScreen
	model.loading = false
	model.height = 12
	model.resourceYAMLKind = "Deployment"
	model.resourceYAMLName = "api"
	model.resourceYAML = strings.Join([]string{
		"apiVersion: apps/v1",
		"kind: Deployment",
		"metadata:",
		"  name: api",
		"spec:",
		"  template:",
		"    spec:",
		"      containers:",
		"      - name: app",
		"        image: example/api:v1",
		"      - name: sidecar",
		"        image: example/sidecar:v1",
	}, "\n")

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	searching := updated.(Model)
	if command != nil || !searching.searching || searching.filtering {
		t.Fatalf("slash did not start scrollable search: %#v", searching)
	}
	updated, _ = searching.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("image")})
	found := updated.(Model)
	if !found.searching || found.searchQuery != "image" || found.scroll == 0 ||
		!strings.Contains(found.View(), "Search /image (editing) (1/2 matches") {
		t.Fatalf("search did not jump to first match: %#v\n%s", found, found.View())
	}
	if !searchHitStyle.GetBold() || !searchFocusStyle.GetBold() {
		t.Fatal("search match styles should emphasize matches")
	}
	if searchHitStyle.GetBackground() == searchFocusStyle.GetBackground() {
		t.Fatal("focused search match should use a distinct background")
	}
	for _, line := range strings.Split(found.View(), "\n") {
		if found.width > 0 && lipgloss.Width(line) > found.width {
			t.Fatalf("highlighted search line width = %d, want <= %d: %q", lipgloss.Width(line), found.width, line)
		}
	}

	updated, _ = found.Update(tea.KeyMsg{Type: tea.KeyEnter})
	applied := updated.(Model)
	if applied.searching || applied.searchQuery != "image" {
		t.Fatalf("Enter did not apply search: %#v", applied)
	}
	firstMatch := applied.scroll

	updated, _ = applied.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	next := updated.(Model)
	if next.scroll <= firstMatch {
		t.Fatalf("n did not move to next match: first=%d next=%d", firstMatch, next.scroll)
	}
	if !strings.Contains(next.View(), "Search /image (2/2 matches") {
		t.Fatalf("next search status missing current match index:\n%s", next.View())
	}
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	previous := updated.(Model)
	if previous.scroll != firstMatch {
		t.Fatalf("N did not move to previous match: got %d, want %d", previous.scroll, firstMatch)
	}
	if !strings.Contains(previous.View(), "Search /image (1/2 matches") {
		t.Fatalf("previous search status missing current match index:\n%s", previous.View())
	}

	updated, _ = previous.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cleared := updated.(Model)
	if cleared.searchQuery != "" || cleared.searching {
		t.Fatalf("Esc did not clear search: %#v", cleared)
	}
}

func TestSlashStillFiltersListScreens(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.pods = []kube.PodSummary{{Name: "api"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	filtering := updated.(Model)
	if command != nil || !filtering.filtering || filtering.searching {
		t.Fatalf("slash did not start list filtering: %#v", filtering)
	}
}

func TestNetworkAndCronJobDetailsOpenYAMLTargets(t *testing.T) {
	t.Parallel()

	yaml := &fakeResourceYAML{content: "kind: Service\nmetadata:\n  name: api\n"}
	dependencies := testDependencies(t)
	dependencies.YAML = yaml
	model := NewModel(dependencies)
	model.screen = networkDetailsScreen
	model.selectedNetwork = kube.NetworkSummary{Kind: "Service", Name: "api"}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, _ = updated.(Model).Update(command())
	if yaml.kind != "Service" || yaml.name != "api" || updated.(Model).screen != resourceYAMLScreen {
		t.Fatalf("service YAML target = %s/%s, screen=%d", yaml.kind, yaml.name, updated.(Model).screen)
	}

	model.screen = cronJobDetailsScreen
	model.selectedWorkload = kube.WorkloadSummary{Kind: "CronJob", Name: "cleanup"}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, _ = updated.(Model).Update(command())
	if yaml.kind != "CronJob" || yaml.name != "cleanup" || updated.(Model).screen != resourceYAMLScreen {
		t.Fatalf("cronjob YAML target = %s/%s, screen=%d", yaml.kind, yaml.name, updated.(Model).screen)
	}
}

func TestPodAndWorkloadListsShowWarningCounts(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.pods = []kube.PodSummary{{Name: "api", Status: "Running", WarningCount: 3}}
	if view := model.View(); !strings.Contains(view, "WARNINGS") || !strings.Contains(view, "3") {
		t.Fatalf("pod warning count missing:\n%s", view)
	}

	model.screen = workloadScreen
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", WarningCount: 2}}
	if view := model.View(); !strings.Contains(view, "WARNINGS") || !strings.Contains(view, "2") {
		t.Fatalf("workload warning count missing:\n%s", view)
	}
}

func TestWorkloadManagedPodLogsReturnToManagedPods(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadPodsScreen
	model.loading = false
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}
	model.workloadPods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	logs := updated.(Model)
	if logs.screen != podLogsScreen || !strings.Contains(logs.View(), "hello from app") {
		t.Fatalf("unexpected logs view:\n%s", logs.View())
	}
	updated, _ = logs.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadPodsScreen {
		t.Fatalf("logs escape screen = %d, want workloadPodsScreen", updated.(Model).screen)
	}
}

func TestCronJobRolloutRestartIsBlockedInTUI(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	final := updated.(Model)

	if command != nil || final.screen != workloadScreen ||
		!strings.Contains(final.status, "does not support rollout restart") {
		t.Fatalf("unexpected CronJob restart result: %#v", final)
	}
}

func TestCronJobStatusMarkersDescribeLifecycle(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		workload kube.WorkloadSummary
		want     string
	}{
		{workload: kube.WorkloadSummary{Kind: "CronJob", Active: 1}, want: "running"},
		{workload: kube.WorkloadSummary{Kind: "CronJob"}, want: "scheduled"},
		{workload: kube.WorkloadSummary{Kind: "CronJob", Suspended: true}, want: "suspended"},
	} {
		if got := workloadStatusMarker(test.workload); !strings.Contains(got, test.want) {
			t.Fatalf("marker = %q, want %q", got, test.want)
		}
	}
}

func TestCronJobEnterLoadsDetailsAndSuspends(t *testing.T) {
	t.Parallel()

	suspended := false
	dependencies := testDependencies(t)
	workloads := dependencies.Workloads.(fakeWorkloads)
	workloads.suspended = &suspended
	dependencies.Workloads = workloads
	model := NewModel(dependencies)
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	for _, want := range []string{"CronJob Details: cleanup", "Concurrency Policy: Forbid", "cleanup-123", "Completed"} {
		if !strings.Contains(details.View(), want) {
			t.Fatalf("details missing %q:\n%s", want, details.View())
		}
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	confirm := updated.(Model)
	if confirm.screen != cronJobStateConfirmScreen ||
		!strings.Contains(confirm.View(), "Current state: active") ||
		!strings.Contains(confirm.View(), "New state: suspended") {
		t.Fatalf("unexpected state confirmation:\n%s", confirm.View())
	}
	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)
	if !suspended || !strings.Contains(final.status, "CronJob/cleanup is now suspended") {
		t.Fatalf("final state model = %#v, suspended=%t", final, suspended)
	}
}

func TestProductionCronJobStateRequiresExactReferenceInTUI(t *testing.T) {
	t.Parallel()

	suspended := false
	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	workloads := dependencies.Workloads.(fakeWorkloads)
	workloads.suspended = &suspended
	dependencies.Workloads = workloads
	model := NewModel(dependencies)
	model.screen = cronJobDetailsScreen
	model.selectedWorkload = kube.WorkloadSummary{Kind: "CronJob", Name: "cleanup"}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	confirm := updated.(Model)
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("wrong")})
	updated, command := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || suspended {
		t.Fatal("wrong production confirmation suspended CronJob")
	}
	confirm = updated.(Model)
	confirm.confirmationInput = ""
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("CronJob/cleanup")})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exact CronJob reference did not create state command")
	}
	updated, _ = updated.(Model).Update(command())
	if !suspended {
		t.Fatal("exact confirmation did not suspend CronJob")
	}
}

func TestProductionWorkloadRestartRequiresExactReference(t *testing.T) {
	t.Parallel()

	restarted := ""
	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	dependencies.Workloads = fakeWorkloads{
		items:     []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", Ready: 3, Desired: 3}},
		restarted: &restarted,
	}
	model := NewModel(dependencies)
	model.screen = workloadScreen
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", Ready: 3, Desired: 3}}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	confirm := updated.(Model)
	if confirm.screen != workloadRestartConfirmScreen || !strings.Contains(confirm.View(), "every pod") {
		t.Fatalf("unexpected confirmation:\n%s", confirm.View())
	}
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Deployment/api")})
	updated, command := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exact workload reference did not create restart command")
	}
	updated, _ = updated.(Model).Update(command())
	if restarted != "Deployment/api" {
		t.Fatalf("restarted = %q", restarted)
	}
}

func TestCopySelectedResource(t *testing.T) {
	t.Parallel()

	clipboard := &fakeClipboard{}
	dependencies := testDependencies(t)
	dependencies.Clipboard = clipboard
	model := NewModel(dependencies)
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	final := updated.(Model)
	if command != nil || clipboard.copied != "Deployment/api" {
		t.Fatalf("copied = %q, command=%v", clipboard.copied, command)
	}
	if !strings.Contains(final.status, "Copied workload to clipboard: Deployment/api") {
		t.Fatalf("copy status = %q", final.status)
	}
	if !strings.Contains(final.helpView(), "c copy") {
		t.Fatalf("help missing copy action:\n%s", final.helpView())
	}
}

func TestCopyNetworkDetailsPrefersUsefulAddressOrHost(t *testing.T) {
	t.Parallel()

	clipboard := &fakeClipboard{}
	dependencies := testDependencies(t)
	dependencies.Clipboard = clipboard
	model := NewModel(dependencies)
	model.screen = networkDetailsScreen
	model.selectedNetwork = kube.NetworkSummary{Kind: "Service", Name: "api"}
	model.networkDetails = kube.NetworkDetails{NetworkSummary: kube.NetworkSummary{
		Kind: "Service", Name: "api", Address: "10.0.0.1",
	}}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if clipboard.copied != "10.0.0.1" || !strings.Contains(updated.(Model).status, "service address") {
		t.Fatalf("service copy = %q, status=%q", clipboard.copied, updated.(Model).status)
	}

	model.networkDetails = kube.NetworkDetails{NetworkSummary: kube.NetworkSummary{
		Kind: "Ingress", Name: "public", Hosts: []string{"api.example.com"},
	}}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if clipboard.copied != "api.example.com" || !strings.Contains(updated.(Model).status, "ingress host") {
		t.Fatalf("ingress copy = %q, status=%q", clipboard.copied, updated.(Model).status)
	}
}

func TestCopySearchFocusedLine(t *testing.T) {
	t.Parallel()

	clipboard := &fakeClipboard{}
	dependencies := testDependencies(t)
	dependencies.Clipboard = clipboard
	model := NewModel(dependencies)
	model.screen = resourceYAMLScreen
	model.loading = false
	model.resourceYAMLKind = "Deployment"
	model.resourceYAMLName = "api"
	model.resourceYAML = strings.Join([]string{
		"apiVersion: apps/v1",
		"kind: Deployment",
		"spec:",
		"  template:",
		"    spec:",
		"      containers:",
		"      - image: example/api:v1",
		"      - image: example/worker:v1",
	}, "\n")
	model.searchScreen = resourceYAMLScreen
	model.searchQuery = "image"
	model.jumpToFirstSearchMatch()
	model.jumpToSearchMatch(1)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if clipboard.copied != "- image: example/worker:v1" {
		t.Fatalf("copied line = %q", clipboard.copied)
	}
	if !strings.Contains(updated.(Model).status, "Copied line to clipboard") {
		t.Fatalf("copy status = %q", updated.(Model).status)
	}
}

func TestCopyWrappedLogLineCopiesFullSourceLine(t *testing.T) {
	t.Parallel()

	clipboard := &fakeClipboard{}
	dependencies := testDependencies(t)
	dependencies.Clipboard = clipboard
	model := NewModel(dependencies)
	model.screen = podLogsScreen
	model.loading = false
	model.width = 28
	model.selectedPod = "api-abc"
	fullLine := "2026-06-28T10:00:00Z request_id=abc123 path=/api/orders status=500 duration_ms=1200 message=upstream timeout"
	model.logs = fullLine
	model.scroll = 3

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if clipboard.copied != fullLine {
		t.Fatalf("copied wrapped line = %q, want full source line %q", clipboard.copied, fullLine)
	}
	if !strings.Contains(updated.(Model).status, "Copied line to clipboard") {
		t.Fatalf("copy status = %q", updated.(Model).status)
	}
	if strings.Contains(updated.(Model).status, "upstream timeout") {
		t.Fatalf("copy status should show a shortened preview, got %q", updated.(Model).status)
	}
}

func TestNamespaceEnterPersistsSelection(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	model := NewModel(dependencies)
	model.screen = namespaceScreen
	model.namespaces = []string{"api", "workers"}
	model.cursor = 1

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.dependencies.Profile.CurrentNamespace != "workers" {
		t.Fatalf("CurrentNamespace = %q, want workers", final.dependencies.Profile.CurrentNamespace)
	}
	cfg, err := (config.Store{Path: dependencies.ConfigPath}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Profiles["staging"].CurrentNamespace != "workers" {
		t.Fatalf("persisted namespace = %q, want workers", cfg.Profiles["staging"].CurrentNamespace)
	}
}
