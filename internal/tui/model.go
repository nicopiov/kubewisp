package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type screen int

const (
	dashboardScreen screen = iota
	namespaceScreen
	podScreen
	workloadScreen
	networkScreen
	eventScreen
	podDetailsScreen
	podLogsScreen
	containerScreen
	portForwardScreen
	execContainerScreen
	execConfirmScreen
	podActionConfirmScreen
	workloadRestartConfirmScreen
	workloadDetailsScreen
	workloadRolloutScreen
	workloadPodsScreen
	resourceDiagnosticsScreen
	networkDetailsScreen
	servicePodsScreen
	ingressServicesScreen
	cronJobDetailsScreen
	cronJobStateConfirmScreen
	profileScreen
	profileRenameScreen
	profileDeleteConfirmScreen
)

type Dependencies struct {
	ConfigPath   string
	ProfileName  string
	Profile      config.Profile
	Connectivity kube.ConnectivityChecker
	Namespaces   kube.NamespaceService
	Pods         kube.PodService
	Workloads    kube.WorkloadService
	Network      kube.NetworkService
	Events       kube.EventService
	Doctor       doctor.Reporter
	PortForward  kubectl.PortForwarder
	Exec         kubectl.Executor
	Profiles     ProfileConnector
}

type Model struct {
	dependencies Dependencies
	screen       screen
	cursor       int
	scroll       int
	width        int
	height       int
	loading      bool
	status       string
	err          error
	loadedAt     map[screen]time.Time
	cacheTTL     time.Duration
	now          func() time.Time
	filtering    bool
	filterQuery  string
	filterScreen screen

	connectivity         kube.ConnectivityReport
	namespaces           []string
	pods                 []kube.PodSummary
	podDetails           kube.PodDetails
	logs                 string
	containers           []string
	selectedPod          string
	selectedContainer    string
	doctorReport         doctor.Report
	ports                []kube.PodPort
	workloads            []kube.WorkloadSummary
	workloadPods         []kube.PodSummary
	servicePods          []kube.PodSummary
	networkResources     []kube.NetworkSummary
	ingressServices      []kube.NetworkSummary
	selectedNetwork      kube.NetworkSummary
	networkDetails       kube.NetworkDetails
	parentNetwork        kube.NetworkSummary
	parentNetworkDetails kube.NetworkDetails
	events               []kube.NamespaceEventSummary
	selectedWorkload     kube.WorkloadSummary
	workloadDetails      kube.WorkloadDetails
	rolloutProgress      kube.RolloutProgress
	rolloutTickPending   bool
	diagnostics          kube.ResourceDiagnostics
	cronJobDetails       kube.CronJobDetails
	pendingWorkload      kube.NamespaceEventSummary
	podAction            string
	podActionInfo        kube.PodActionInfo
	confirmationInput    string
	cronJobSuspended     bool
	podBackScreen        screen
	workloadBackScreen   screen
	rolloutBackScreen    screen
	diagnosticBackScreen screen
	networkBackScreen    screen
	profileNames         []string
	profileInput         string
	selectedProfile      string
}

type connectivityMsg struct {
	report kube.ConnectivityReport
	err    error
}

type namespacesMsg struct {
	names []string
	err   error
}

type podsMsg struct {
	pods []kube.PodSummary
	err  error
}

type namespaceSwitchedMsg struct {
	namespace string
	err       error
}

type podDetailsMsg struct {
	details kube.PodDetails
	err     error
}

type containersMsg struct {
	names []string
	err   error
}

type podLogsMsg struct {
	logs string
	err  error
}

type dashboardDataMsg struct {
	pods   []kube.PodSummary
	report doctor.Report
	err    error
}

type portsMsg struct {
	ports []kube.PodPort
	err   error
}

type portForwardFinishedMsg struct {
	err error
}

type execFinishedMsg struct {
	err error
}

type podActionInfoMsg struct {
	info kube.PodActionInfo
	err  error
}

type podDeletedMsg struct {
	action string
	err    error
}

type workloadsMsg struct {
	workloads []kube.WorkloadSummary
	err       error
}

type workloadPodsMsg struct {
	pods []kube.PodSummary
	err  error
}

type servicePodsMsg struct {
	pods []kube.PodSummary
	err  error
}

type networkMsg struct {
	resources []kube.NetworkSummary
	err       error
}

type ingressServicesMsg struct {
	services []kube.NetworkSummary
	err      error
}

type podOwnerWorkloadMsg struct {
	workload kube.WorkloadSummary
	err      error
}

type networkDetailsMsg struct {
	details kube.NetworkDetails
	err     error
}

type eventsMsg struct {
	events []kube.NamespaceEventSummary
	err    error
}

type diagnosticsMsg struct {
	report kube.ResourceDiagnostics
	err    error
}

type workloadRestartedMsg struct {
	err error
}

type cronJobDetailsMsg struct {
	details kube.CronJobDetails
	err     error
}

type workloadDetailsMsg struct {
	details kube.WorkloadDetails
	err     error
}

type rolloutProgressMsg struct {
	progress kube.RolloutProgress
	err      error
}

type rolloutTickMsg time.Time

type cronJobStateMsg struct {
	suspended bool
	err       error
}

type profilesMsg struct {
	names []string
	err   error
}

type profileSwitchedMsg struct {
	name    string
	profile config.Profile
	err     error
}

type profileRenamedMsg struct {
	oldName string
	newName string
	err     error
}

type profileDeletedMsg struct {
	name string
	err  error
}

var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	healthyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpKeyStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	helpGroupStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	tableHeadStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
)

type tableColumn struct {
	header     string
	alignRight bool
}

type tableRow struct {
	cursor string
	cells  []string
}

func NewModel(dependencies Dependencies) Model {
	return Model{
		dependencies: dependencies, loading: true, podBackScreen: podScreen,
		workloadBackScreen: workloadScreen, rolloutBackScreen: workloadScreen, networkBackScreen: networkScreen,
		diagnosticBackScreen: podDetailsScreen,
		loadedAt:             make(map[screen]time.Time), cacheTTL: 15 * time.Second, now: time.Now,
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadCurrent()
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case connectivityMsg:
		m.err = message.err
		m.connectivity = message.report
		if message.err == nil {
			m.loading = true
			return m, m.loadDashboardData()
		}
		m.loading = false
	case namespacesMsg:
		m.loading = false
		m.err = message.err
		m.namespaces = message.names
		m.markLoaded(namespaceScreen)
		m.clampCursor(m.itemCount())
	case podsMsg:
		m.loading = false
		m.err = message.err
		m.pods = message.pods
		m.markLoaded(podScreen)
		m.clampCursor(m.itemCount())
	case workloadsMsg:
		m.loading = false
		m.err = message.err
		m.workloads = message.workloads
		if m.pendingWorkload.ObjectName != "" {
			for index, workload := range m.workloads {
				if strings.EqualFold(workload.Kind, m.pendingWorkload.ObjectKind) &&
					workload.Name == m.pendingWorkload.ObjectName {
					m.cursor = index
					m.status = "Selected " + workloadReference(workload)
					break
				}
			}
			m.pendingWorkload = kube.NamespaceEventSummary{}
		}
		m.clampCursor(m.itemCount())
		m.markLoaded(workloadScreen)
	case workloadPodsMsg:
		m.loading = false
		m.err = message.err
		m.workloadPods = message.pods
		m.clampCursor(m.itemCount())
	case servicePodsMsg:
		m.loading = false
		m.err = message.err
		m.servicePods = message.pods
		m.clampCursor(m.itemCount())
	case networkMsg:
		m.loading = false
		m.err = message.err
		m.networkResources = message.resources
		m.markLoaded(networkScreen)
		m.clampCursor(m.itemCount())
	case networkDetailsMsg:
		m.loading = false
		m.err = message.err
		m.networkDetails = message.details
	case ingressServicesMsg:
		m.loading = false
		m.err = message.err
		m.ingressServices = message.services
		m.clampCursor(m.itemCount())
	case eventsMsg:
		m.loading = false
		m.err = message.err
		m.events = message.events
		m.markLoaded(eventScreen)
		m.clampCursor(m.itemCount())
	case diagnosticsMsg:
		m.loading = false
		m.err = message.err
		m.diagnostics = message.report
	case namespaceSwitchedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.dependencies.Profile.CurrentNamespace = message.namespace
			m.loadedAt = make(map[screen]time.Time)
			m.status = fmt.Sprintf("Namespace switched to %s", message.namespace)
		}
	case podDetailsMsg:
		m.loading = false
		m.err = message.err
		m.podDetails = message.details
	case containersMsg:
		m.loading = false
		m.err = message.err
		m.containers = message.names
		m.clampCursor(len(m.containers))
		if message.err == nil && len(message.names) == 1 && m.screen == containerScreen {
			m.selectedContainer = message.names[0]
			m.screen = podLogsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadLogs(message.names[0])
		}
		if message.err == nil && len(message.names) == 1 && m.screen == execContainerScreen {
			m.selectedContainer = message.names[0]
			m.screen = execConfirmScreen
		}
	case podLogsMsg:
		m.loading = false
		m.err = message.err
		m.logs = message.logs
	case dashboardDataMsg:
		m.loading = false
		m.err = message.err
		m.pods = message.pods
		m.doctorReport = message.report
		m.markLoaded(dashboardScreen)
		m.markLoaded(podScreen)
	case portsMsg:
		m.loading = false
		m.err = message.err
		m.ports = message.ports
		m.clampCursor(len(m.ports))
	case portForwardFinishedMsg:
		m.screen = podScreen
		m.loading = false
		m.err = nil
		if message.err != nil && !isInterrupted(message.err) {
			m.err = message.err
			m.status = ""
		} else {
			m.status = "Port-forward stopped"
		}
	case execFinishedMsg:
		m.screen = podScreen
		m.loading = false
		m.err = message.err
		m.status = ""
		if message.err == nil || isInterrupted(message.err) {
			m.err = nil
			m.status = "Exec session ended"
		}
	case podActionInfoMsg:
		m.loading = false
		m.err = message.err
		m.podActionInfo = message.info
		if message.err == nil {
			if m.podAction == "restart" && message.info.ControllerOwner == "" {
				m.err = errors.New("restart is blocked because this pod has no controller owner")
				m.screen = podScreen
			} else {
				m.screen = podActionConfirmScreen
			}
		}
	case podDeletedMsg:
		m.loading = false
		m.err = message.err
		m.screen = podScreen
		if message.err == nil {
			m.status = strings.Title(message.action) + " requested for " + m.selectedPod
			return m, m.loadPods()
		}
	case workloadRestartedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.status = "Rollout restart requested for " + workloadReference(m.selectedWorkload)
			m.screen = workloadRolloutScreen
			m.loading = true
			return m, m.loadRolloutProgress()
		}
		m.screen = workloadScreen
	case cronJobDetailsMsg:
		m.loading = false
		m.err = message.err
		m.cronJobDetails = message.details
	case workloadDetailsMsg:
		m.loading = false
		m.err = message.err
		m.workloadDetails = message.details
	case rolloutProgressMsg:
		m.loading = false
		m.err = message.err
		m.rolloutProgress = message.progress
		if message.err == nil && m.monitoringRollout() &&
			rolloutStillRunning(message.progress) && !m.rolloutTickPending {
			m.rolloutTickPending = true
			return m, rolloutTick()
		}
	case rolloutTickMsg:
		m.rolloutTickPending = false
		if m.monitoringRollout() && rolloutStillRunning(m.rolloutProgress) {
			m.loading = true
			return m, m.loadRolloutProgress()
		}
	case podOwnerWorkloadMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.selectedWorkload = message.workload
			m.workloadBackScreen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			if strings.EqualFold(message.workload.Kind, "CronJob") {
				m.screen = cronJobDetailsScreen
				return m, m.loadCronJobDetails()
			}
			m.screen = workloadDetailsScreen
			return m, m.loadWorkloadDetails()
		}
	case cronJobStateMsg:
		m.loading = false
		m.err = message.err
		m.screen = cronJobDetailsScreen
		if message.err == nil {
			m.selectedWorkload.Suspended = message.suspended
			m.status = "CronJob/" + m.selectedWorkload.Name + " is now " + cronJobStateWord(message.suspended)
			return m, m.loadCronJobDetails()
		}
	case profilesMsg:
		m.loading = false
		m.err = message.err
		m.profileNames = message.names
		m.clampCursor(len(m.profileNames))
	case profileSwitchedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.dependencies.ProfileName = message.name
			m.dependencies.Profile = message.profile
			m.resetClusterData()
			m.screen = dashboardScreen
			m.status = "Switched to profile " + message.name
			m.loading = true
			return m, m.loadCurrent()
		}
	case profileRenamedMsg:
		m.loading = false
		m.err = message.err
		m.screen = profileScreen
		if message.err == nil {
			if m.dependencies.ProfileName == message.oldName {
				m.dependencies.ProfileName = message.newName
			}
			m.status = fmt.Sprintf("Profile %s renamed to %s", message.oldName, message.newName)
			return m, m.loadProfiles()
		}
	case profileDeletedMsg:
		m.loading = false
		m.err = message.err
		m.screen = profileScreen
		if message.err == nil {
			m.status = "Profile " + message.name + " deleted"
			return m, m.loadProfiles()
		}
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch key.String() {
		case "ctrl+c":
			m.clearFilter()
			return m, nil
		case "esc":
			m.clearFilter()
			return m, nil
		case "enter":
			m.filtering = false
			return m, nil
		case "backspace":
			runes := []rune(m.filterQuery)
			if len(runes) > 0 {
				m.filterQuery = string(runes[:len(runes)-1])
				m.cursor = 0
			}
			return m, nil
		default:
			if key.Type == tea.KeyRunes {
				m.filterQuery += string(key.Runes)
				m.cursor = 0
			}
			return m, nil
		}
	}
	if m.screen == profileRenameScreen {
		switch key.String() {
		case "ctrl+c", "esc":
			m.screen = profileScreen
			m.profileInput = ""
			return m, nil
		case "backspace":
			if len(m.profileInput) > 0 {
				m.profileInput = m.profileInput[:len(m.profileInput)-1]
			}
			return m, nil
		case "enter":
			if strings.TrimSpace(m.profileInput) == "" {
				return m, nil
			}
			m.loading = true
			return m, m.renameProfile(m.selectedProfile, strings.TrimSpace(m.profileInput))
		default:
			if key.Type == tea.KeyRunes {
				m.profileInput += string(key.Runes)
			}
			return m, nil
		}
	}
	if m.screen == profileDeleteConfirmScreen {
		switch key.String() {
		case "ctrl+c", "esc", "n":
			m.screen = profileScreen
			return m, nil
		case "y":
			m.loading = true
			return m, m.deleteProfile(m.selectedProfile)
		default:
			return m, nil
		}
	}
	if (m.screen == podActionConfirmScreen || m.screen == workloadRestartConfirmScreen ||
		m.screen == cronJobStateConfirmScreen) &&
		m.dependencies.Profile.Production {
		switch key.String() {
		case "ctrl+c", "esc":
			if m.screen == workloadRestartConfirmScreen || m.screen == cronJobStateConfirmScreen {
				if m.screen == cronJobStateConfirmScreen {
					m.screen = cronJobDetailsScreen
				} else {
					m.screen = workloadScreen
				}
			} else {
				m.screen = podScreen
			}
			m.confirmationInput = ""
			return m, nil
		case "backspace":
			if len(m.confirmationInput) > 0 {
				m.confirmationInput = m.confirmationInput[:len(m.confirmationInput)-1]
			}
			return m, nil
		case "enter":
			expected := m.selectedPod
			if m.screen == workloadRestartConfirmScreen || m.screen == cronJobStateConfirmScreen {
				expected = workloadReference(m.selectedWorkload)
			}
			if m.confirmationInput == expected {
				if m.screen == workloadRestartConfirmScreen {
					return m.executeWorkloadRestart()
				}
				if m.screen == cronJobStateConfirmScreen {
					return m.executeCronJobState()
				}
				return m.executePodAction()
			}
			return m, nil
		default:
			if key.Type == tea.KeyRunes {
				m.confirmationInput += string(key.Runes)
			}
			return m, nil
		}
	}
	if key.String() == "ctrl+c" && m.isConfirmationScreen() {
		key = tea.KeyMsg{Type: tea.KeyEsc}
	}

	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
		if m.isFilterableScreen() {
			m.filtering = true
			m.filterScreen = m.screen
			m.filterQuery = ""
			m.cursor = 0
			m.status = ""
			m.err = nil
			return m, nil
		}
	case "esc":
		if m.filterQuery != "" && m.filterScreen == m.screen {
			m.clearFilter()
			return m, nil
		}
		if m.isNestedScreen() {
			if m.screen == workloadRestartConfirmScreen {
				m.screen = workloadScreen
			} else if m.screen == profileScreen || m.screen == profileRenameScreen ||
				m.screen == profileDeleteConfirmScreen {
				m.screen = dashboardScreen
			} else if m.screen == networkDetailsScreen {
				m.screen = m.networkBackScreen
			} else if m.screen == servicePodsScreen || m.screen == ingressServicesScreen {
				m.selectedNetwork = m.parentNetwork
				m.networkDetails = m.parentNetworkDetails
				m.networkBackScreen = networkScreen
				m.screen = networkDetailsScreen
			} else if m.screen == workloadDetailsScreen || m.screen == cronJobDetailsScreen ||
				m.screen == cronJobStateConfirmScreen {
				m.screen = m.workloadBackScreen
			} else if m.screen == workloadRolloutScreen {
				m.screen = m.rolloutBackScreen
			} else if m.screen == resourceDiagnosticsScreen {
				m.screen = m.diagnosticBackScreen
			} else if m.screen == workloadPodsScreen {
				m.screen = workloadDetailsScreen
			} else if m.screen == podDetailsScreen || m.screen == podLogsScreen || m.screen == containerScreen {
				m.screen = m.podBackScreen
			} else {
				m.screen = podScreen
			}
			m.cursor = 0
			m.scroll = 0
			m.loading = false
			m.err = nil
			m.status = ""
			return m, nil
		}
	case "1":
		return m.changeScreen(dashboardScreen)
	case "2":
		return m.changeScreen(namespaceScreen)
	case "3":
		return m.changeScreen(podScreen)
	case "4":
		return m.changeScreen(workloadScreen)
	case "5":
		return m.changeScreen(networkScreen)
	case "6":
		return m.changeScreen(eventScreen)
	case "P":
		if !m.isNestedScreen() {
			m.screen = profileScreen
			m.cursor = 0
			m.loading = true
			m.status = ""
			m.err = nil
			return m, m.loadProfiles()
		}
	case "tab", "right":
		if !m.isNestedScreen() {
			return m.changeScreen((m.screen + 1) % 6)
		}
	case "shift+tab", "left":
		if !m.isNestedScreen() {
			return m.changeScreen((m.screen + 5) % 6)
		}
	case "up", "k":
		if m.screen == podDetailsScreen || m.screen == podLogsScreen ||
			m.screen == workloadDetailsScreen || m.screen == workloadRolloutScreen ||
			m.screen == resourceDiagnosticsScreen || m.screen == networkDetailsScreen || m.screen == cronJobDetailsScreen {
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		}
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.screen == podDetailsScreen || m.screen == podLogsScreen ||
			m.screen == workloadDetailsScreen || m.screen == workloadRolloutScreen ||
			m.screen == resourceDiagnosticsScreen || m.screen == networkDetailsScreen || m.screen == cronJobDetailsScreen {
			m.scroll++
			return m, nil
		}
		if m.cursor < m.itemCount()-1 {
			m.cursor++
		}
	case "l":
		if (m.screen == podScreen && len(m.visiblePods()) > 0) ||
			(m.screen == workloadPodsScreen && len(m.visibleWorkloadPods()) > 0) ||
			(m.screen == servicePodsScreen && len(m.visibleServicePods()) > 0) ||
			m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.visiblePods()[m.cursor].Name
				m.podBackScreen = podScreen
			}
			if m.screen == workloadPodsScreen {
				m.selectedPod = m.visibleWorkloadPods()[m.cursor].Name
				m.podBackScreen = workloadPodsScreen
			}
			if m.screen == servicePodsScreen {
				m.selectedPod = m.visibleServicePods()[m.cursor].Name
				m.podBackScreen = servicePodsScreen
			}
			m.screen = containerScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadContainers()
		}
	case "p":
		if m.screen == networkDetailsScreen && strings.EqualFold(m.selectedNetwork.Kind, "Service") {
			m.parentNetwork = m.selectedNetwork
			m.parentNetworkDetails = m.networkDetails
			m.screen = servicePodsScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadServicePods()
		}
		if m.screen == workloadDetailsScreen {
			m.screen = workloadPodsScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadWorkloadPods()
		}
		if (m.screen == podScreen && len(m.visiblePods()) > 0) || m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.visiblePods()[m.cursor].Name
			}
			m.screen = portForwardScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadPorts()
		}
	case "w":
		if m.screen == workloadDetailsScreen {
			m.rolloutBackScreen = workloadDetailsScreen
			m.screen = workloadRolloutScreen
			m.scroll = 0
			m.loading = true
			m.status = ""
			m.err = nil
			return m, m.loadRolloutProgress()
		}
	case "v":
		if m.screen == podDetailsScreen {
			m.diagnosticBackScreen = podDetailsScreen
			m.screen = resourceDiagnosticsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadDiagnostics("Pod", m.selectedPod)
		}
		if m.screen == workloadDetailsScreen {
			m.diagnosticBackScreen = workloadDetailsScreen
			m.screen = resourceDiagnosticsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadDiagnostics(m.selectedWorkload.Kind, m.selectedWorkload.Name)
		}
	case "e":
		if (m.screen == podScreen && len(m.visiblePods()) > 0) || m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.visiblePods()[m.cursor].Name
			}
			m.screen = execContainerScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadContainers()
		}
	case "d":
		if m.screen == profileScreen && len(m.profileNames) > 0 {
			m.selectedProfile = m.profileNames[m.cursor]
			if m.selectedProfile == m.dependencies.ProfileName {
				m.status = "Switch away from the active profile before deleting it"
				return m, nil
			}
			m.screen = profileDeleteConfirmScreen
			return m, nil
		}
		if (m.screen == podScreen && len(m.visiblePods()) > 0) || m.screen == podDetailsScreen {
			return m.beginPodAction("delete")
		}
	case "r":
		if m.screen == profileScreen && len(m.profileNames) > 0 {
			m.selectedProfile = m.profileNames[m.cursor]
			m.profileInput = ""
			m.screen = profileRenameScreen
			m.loading = false
			return m, nil
		}
		delete(m.loadedAt, m.screen)
		m.loading = true
		m.status = ""
		m.err = nil
		return m, m.loadCurrent()
	case "R":
		if (m.screen == workloadScreen && len(m.visibleWorkloads()) > 0) || m.screen == workloadDetailsScreen {
			if m.screen == workloadScreen {
				m.selectedWorkload = m.visibleWorkloads()[m.cursor]
				m.rolloutBackScreen = workloadScreen
			} else {
				m.rolloutBackScreen = workloadDetailsScreen
			}
			if !kube.SupportsRolloutRestart(m.selectedWorkload.Kind) {
				m.status = fmt.Sprintf("%s does not support rollout restart", m.selectedWorkload.Kind)
				return m, nil
			}
			m.confirmationInput = ""
			m.status = ""
			m.err = nil
			m.screen = workloadRestartConfirmScreen
			m.loading = false
			return m, nil
		}
		if (m.screen == podScreen && len(m.visiblePods()) > 0) || m.screen == podDetailsScreen {
			return m.beginPodAction("restart")
		}
	case "s":
		if m.screen == networkDetailsScreen && strings.EqualFold(m.selectedNetwork.Kind, "Ingress") {
			m.parentNetwork = m.selectedNetwork
			m.parentNetworkDetails = m.networkDetails
			m.screen = ingressServicesScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadIngressServices()
		}
		if m.screen == workloadScreen && len(m.visibleWorkloads()) > 0 {
			m.selectedWorkload = m.visibleWorkloads()[m.cursor]
			if !strings.EqualFold(m.selectedWorkload.Kind, "CronJob") {
				m.status = "Suspend/resume is available only for CronJobs"
				return m, nil
			}
			return m.beginCronJobState()
		}
		if m.screen == cronJobDetailsScreen {
			return m.beginCronJobState()
		}
	case "o":
		if m.screen == podDetailsScreen {
			m.loading = true
			m.status = ""
			m.err = nil
			return m, m.loadPodOwnerWorkload()
		}
	case "y":
		if m.screen == execConfirmScreen && m.dependencies.Profile.Production {
			return m.startExec()
		}
		if m.screen == podActionConfirmScreen && !m.dependencies.Profile.Production {
			return m.executePodAction()
		}
		if m.screen == workloadRestartConfirmScreen && !m.dependencies.Profile.Production {
			return m.executeWorkloadRestart()
		}
		if m.screen == cronJobStateConfirmScreen && !m.dependencies.Profile.Production {
			return m.executeCronJobState()
		}
	case "enter":
		if m.screen == profileScreen && len(m.profileNames) > 0 {
			name := m.profileNames[m.cursor]
			if name == m.dependencies.ProfileName {
				m.status = "Profile " + name + " is already active"
				return m, nil
			}
			m.loading = true
			m.status = "Connecting profile " + name
			return m, m.switchProfile(name)
		}
		if m.screen == namespaceScreen && len(m.visibleNamespaces()) > 0 {
			m.loading = true
			return m, m.switchNamespace(m.visibleNamespaces()[m.cursor])
		}
		if m.screen == podScreen && len(m.visiblePods()) > 0 {
			m.selectedPod = m.visiblePods()[m.cursor].Name
			m.podBackScreen = podScreen
			m.screen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadPodDetails()
		}
		if m.screen == workloadPodsScreen && len(m.visibleWorkloadPods()) > 0 {
			m.selectedPod = m.visibleWorkloadPods()[m.cursor].Name
			m.podBackScreen = workloadPodsScreen
			m.screen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadPodDetails()
		}
		if m.screen == servicePodsScreen && len(m.visibleServicePods()) > 0 {
			m.selectedPod = m.visibleServicePods()[m.cursor].Name
			m.podBackScreen = servicePodsScreen
			m.screen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadPodDetails()
		}
		if m.screen == workloadScreen && len(m.visibleWorkloads()) > 0 {
			m.selectedWorkload = m.visibleWorkloads()[m.cursor]
			m.workloadBackScreen = workloadScreen
			if strings.EqualFold(m.selectedWorkload.Kind, "CronJob") {
				m.screen = cronJobDetailsScreen
				m.scroll = 0
				m.loading = true
				return m, m.loadCronJobDetails()
			}
			m.screen = workloadDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadWorkloadDetails()
		}
		if m.screen == networkScreen && len(m.visibleNetworkResources()) > 0 {
			m.selectedNetwork = m.visibleNetworkResources()[m.cursor]
			m.networkBackScreen = networkScreen
			m.screen = networkDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadNetworkDetails()
		}
		if m.screen == ingressServicesScreen && len(m.visibleIngressServices()) > 0 {
			m.selectedNetwork = m.visibleIngressServices()[m.cursor]
			m.networkBackScreen = ingressServicesScreen
			m.screen = networkDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadNetworkDetails()
		}
		if m.screen == eventScreen && len(m.visibleEvents()) > 0 {
			event := m.visibleEvents()[m.cursor]
			switch strings.ToLower(event.ObjectKind) {
			case "pod":
				m.selectedPod = event.ObjectName
				m.podBackScreen = podScreen
				m.screen = podDetailsScreen
				m.scroll = 0
				m.loading = true
				return m, m.loadPodDetails()
			case "deployment", "statefulset", "daemonset", "cronjob":
				m.pendingWorkload = event
				return m.changeScreen(workloadScreen)
			default:
				m.status = fmt.Sprintf("No drill-down available for %s/%s", event.ObjectKind, event.ObjectName)
				return m, nil
			}
		}
		if m.screen == containerScreen && len(m.containers) > 0 {
			container := m.containers[m.cursor]
			m.selectedContainer = container
			m.screen = podLogsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadLogs(container)
		}
		if m.screen == execContainerScreen && len(m.containers) > 0 {
			m.selectedContainer = m.containers[m.cursor]
			m.screen = execConfirmScreen
			m.loading = false
			return m, nil
		}
		if m.screen == execConfirmScreen && !m.dependencies.Profile.Production {
			return m.startExec()
		}
		if m.screen == portForwardScreen && len(m.ports) > 0 {
			port := m.ports[m.cursor]
			options := kubectl.PortForwardOptions{
				Namespace:  selectedNamespace(m.dependencies.Profile),
				Pod:        m.selectedPod,
				LocalPort:  port.Port,
				RemotePort: port.Port,
			}
			if m.dependencies.PortForward == nil {
				m.err = errors.New("kubectl port-forward service is not configured")
				return m, nil
			}
			m.status = ""
			return m, tea.Exec(
				&portForwardCommand{
					forwarder: m.dependencies.PortForward,
					options:   options,
				},
				func(err error) tea.Msg {
					return portForwardFinishedMsg{err: err}
				},
			)
		}
	}
	return m, nil
}

func (m Model) changeScreen(next screen) (tea.Model, tea.Cmd) {
	if next != m.screen {
		m.clearFilter()
	}
	m.screen = next
	m.cursor = 0
	m.scroll = 0
	m.loading = !m.cacheFresh(next)
	m.status = ""
	m.err = nil
	if !m.loading {
		return m, nil
	}
	return m, m.loadCurrent()
}

func (m Model) View() string {
	var view strings.Builder
	view.WriteString(m.header())
	view.WriteString("\n\n")
	view.WriteString(m.tabs())
	view.WriteString("\n\n")

	if m.loading {
		view.WriteString("Loading...\n")
	} else if m.err != nil {
		view.WriteString(errorStyle.Render(m.err.Error()))
		view.WriteString("\n")
	} else {
		switch m.screen {
		case dashboardScreen:
			view.WriteString(m.dashboardView())
		case namespaceScreen:
			view.WriteString(m.namespaceView())
		case podScreen:
			view.WriteString(m.podView())
		case workloadPodsScreen:
			view.WriteString(m.workloadPodsView())
		case resourceDiagnosticsScreen:
			view.WriteString(m.scrollView(m.resourceDiagnosticsView()))
		case servicePodsScreen:
			view.WriteString(m.servicePodsView())
		case workloadScreen:
			view.WriteString(m.workloadView())
		case networkScreen:
			view.WriteString(m.networkView())
		case ingressServicesScreen:
			view.WriteString(m.ingressServicesView())
		case eventScreen:
			view.WriteString(m.eventView())
		case podDetailsScreen:
			view.WriteString(m.scrollView(m.podDetailsView()))
		case podLogsScreen:
			view.WriteString(m.scrollView(m.podLogsView()))
		case containerScreen:
			view.WriteString(m.containerView())
		case portForwardScreen:
			view.WriteString(m.portForwardView())
		case execContainerScreen:
			view.WriteString(m.execContainerView())
		case execConfirmScreen:
			view.WriteString(m.execConfirmView())
		case podActionConfirmScreen:
			view.WriteString(m.podActionConfirmView())
		case workloadRestartConfirmScreen:
			view.WriteString(m.workloadRestartConfirmView())
		case workloadDetailsScreen:
			view.WriteString(m.scrollView(m.workloadDetailsView()))
		case workloadRolloutScreen:
			view.WriteString(m.scrollView(m.workloadRolloutView()))
		case networkDetailsScreen:
			view.WriteString(m.scrollView(m.networkDetailsView()))
		case cronJobDetailsScreen:
			view.WriteString(m.scrollView(m.cronJobDetailsView()))
		case cronJobStateConfirmScreen:
			view.WriteString(m.cronJobStateConfirmView())
		case profileScreen:
			view.WriteString(m.profileView())
		case profileRenameScreen:
			view.WriteString(m.profileRenameView())
		case profileDeleteConfirmScreen:
			view.WriteString(m.profileDeleteConfirmView())
		}
	}
	if m.status != "" {
		view.WriteString("\n")
		view.WriteString(activeTabStyle.Render(m.status))
		view.WriteString("\n")
	}
	if m.filtering || (m.filterQuery != "" && m.filterScreen == m.screen) {
		view.WriteString("\n")
		view.WriteString(m.filterView())
		view.WriteString("\n")
	}
	body := strings.TrimRight(m.wrapContent(view.String()), "\n")
	footer := m.responsiveHelpView()
	gap := 1
	if m.height > 0 {
		available := m.height - lipgloss.Height(body) - lipgloss.Height(footer) + 1
		if available > gap {
			gap = available
		}
	}
	return body + strings.Repeat("\n", gap) + footer
}

func (m Model) header() string {
	profile := m.dependencies.Profile
	namespace := selectedNamespace(profile)
	production := ""
	if profile.Production {
		production = " | PRODUCTION"
	}
	return titleStyle.Render("kubewisp") + "\n" +
		fmt.Sprintf(
			"Profile: %s%s | Project: %s | Cluster: %s | Namespace: %s",
			m.dependencies.ProfileName,
			production,
			profile.ProjectID,
			profile.ClusterName,
			namespace,
		)
}

func (m Model) tabs() string {
	labels := []string{"Dashboard", "Namespaces", "Pods", "Workloads", "Network", "Events"}
	for index := range labels {
		if screen(index) == m.screen {
			labels[index] = activeTabStyle.Render("[" + labels[index] + "]")
		}
	}
	return strings.Join(labels, "   ")
}

func (m Model) helpView() string {
	if m.filtering {
		return "Input: type filter | backspace edit | enter apply\nGeneral: esc cancel"
	}
	if confirmation := m.confirmationHelp(); confirmation != "" {
		return confirmation
	}
	navigate, actions := m.contextualHelpGroups()
	general := "r refresh | q quit"
	if m.isNestedScreen() {
		general = "r refresh | esc back | q quit"
	} else {
		general = "1-6 screens | tab/left/right switch | P profiles | r refresh | q quit"
	}
	if m.screen == profileScreen {
		general = "esc back | q quit"
	}
	groups := make([]string, 0, 3)
	if navigate != "" {
		groups = append(groups, "Navigate: "+navigate)
	}
	if actions != "" {
		groups = append(groups, "Actions:  "+actions)
	}
	return strings.Join(append(groups, "General:  "+general), "\n")
}

func (m Model) responsiveHelpView() string {
	lines := strings.Split(m.helpView(), "\n")
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(wrapHelp(line, m.width), "\n")...)
	}
	for index, line := range wrapped {
		wrapped[index] = renderHelpLine(line)
	}
	return strings.Join(wrapped, "\n")
}

func (m Model) contextualHelpGroups() (string, string) {
	navigate := "up/down move"
	actions := ""
	switch m.screen {
	case dashboardScreen:
		navigate = ""
	case namespaceScreen:
		navigate += " | enter switch"
		actions = "/ filter"
	case podScreen:
		navigate += " | enter details"
		actions = "/ filter | l logs | p port-forward | e exec | d delete | R restart"
	case workloadScreen:
		navigate += " | enter details"
		actions = "/ filter"
		workloads := m.visibleWorkloads()
		if m.cursor < len(workloads) && strings.EqualFold(workloads[m.cursor].Kind, "CronJob") {
			actions += " | s suspend/resume"
		} else if m.cursor < len(workloads) {
			actions += " | R rollout restart"
		}
	case networkScreen:
		navigate += " | enter details"
		actions = "/ filter"
	case eventScreen:
		navigate += " | enter inspect"
		actions = "/ filter"
	case podDetailsScreen:
		navigate = "up/down scroll"
		actions = "v diagnostics | o owner | l logs | p port-forward | e exec | d delete | R restart"
	case workloadDetailsScreen:
		navigate = "up/down scroll"
		actions = "v diagnostics | w watch rollout | p managed pods | R rollout restart"
	case workloadRolloutScreen:
		navigate = "up/down scroll"
		if rolloutStillRunning(m.rolloutProgress) {
			actions = "auto-refreshing every 2s"
		} else {
			actions = "monitor stopped"
		}
	case workloadPodsScreen, servicePodsScreen:
		navigate += " | enter details"
		actions = "/ filter | l logs"
	case ingressServicesScreen:
		navigate += " | enter details"
		actions = "/ filter"
	case networkDetailsScreen:
		navigate = "up/down scroll"
		if strings.EqualFold(m.selectedNetwork.Kind, "Service") {
			actions = "p selected pods"
		} else if strings.EqualFold(m.selectedNetwork.Kind, "Ingress") {
			actions = "s backend services"
		}
	case cronJobDetailsScreen:
		navigate = "up/down scroll"
		actions = "s suspend/resume"
	case resourceDiagnosticsScreen, podLogsScreen:
		navigate = "up/down scroll"
	case containerScreen, execContainerScreen, portForwardScreen:
		navigate += " | enter select"
	case profileScreen:
		navigate += " | enter switch"
		actions = "r rename | d delete"
	default:
		navigate = "up/down move"
	}
	return navigate, actions
}

func (m Model) confirmationHelp() string {
	switch m.screen {
	case execConfirmScreen:
		if m.dependencies.Profile.Production {
			return "Actions: y confirm production exec\nGeneral: esc cancel | q quit"
		}
		return "Actions: enter start shell\nGeneral: esc cancel | q quit"
	case podActionConfirmScreen:
		if m.dependencies.Profile.Production {
			return "Input: type exact pod name | backspace edit | enter confirm\nGeneral: esc cancel"
		}
		return "Actions: y confirm\nGeneral: esc cancel | q quit"
	case workloadRestartConfirmScreen:
		if m.dependencies.Profile.Production {
			return "Input: type exact Kind/name | backspace edit | enter confirm\nGeneral: esc cancel"
		}
		return "Actions: y confirm rollout restart\nGeneral: esc cancel | q quit"
	case cronJobStateConfirmScreen:
		if m.dependencies.Profile.Production {
			return "Input: type exact CronJob/name | backspace edit | enter confirm\nGeneral: esc cancel"
		}
		return "Actions: y confirm state change\nGeneral: esc cancel | q quit"
	case profileRenameScreen:
		return "Input: type new name | backspace edit | enter rename\nGeneral: esc cancel"
	case profileDeleteConfirmScreen:
		return "Actions: y confirm delete\nGeneral: n/esc cancel | q quit"
	default:
		return ""
	}
}

func wrapHelp(help string, width int) string {
	if width <= 0 || lipgloss.Width(help) <= width {
		return help
	}
	segments := strings.Split(help, " | ")
	var lines []string
	current := ""
	for _, segment := range segments {
		next := segment
		if current != "" {
			next = current + " | " + segment
		}
		if current != "" && lipgloss.Width(next) > width {
			lines = append(lines, current)
			current = segment
			continue
		}
		current = next
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}

func renderHelpLine(line string) string {
	group, commands, found := strings.Cut(line, ": ")
	if !found {
		return mutedStyle.Render(line)
	}
	commands = strings.TrimSpace(commands)
	segments := strings.Split(commands, " | ")
	for index, segment := range segments {
		segment = strings.TrimSpace(segment)
		key, description, found := strings.Cut(segment, " ")
		if !found {
			segments[index] = helpKeyStyle.Render(segment)
			continue
		}
		segments[index] = helpKeyStyle.Render(key) + " " + mutedStyle.Render(description)
	}
	return helpGroupStyle.Render(group+":") + " " +
		strings.Join(segments, mutedStyle.Render(" | "))
}

func (m Model) profileView() string {
	var view strings.Builder
	fmt.Fprintln(&view, "Profiles")
	fmt.Fprintln(&view)
	for index, name := range m.profileNames {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		active := ""
		if name == m.dependencies.ProfileName {
			active = " (active)"
		}
		fmt.Fprintf(&view, "%s%s%s\n", cursor, name, active)
	}
	return view.String()
}

func (m Model) profileRenameView() string {
	return fmt.Sprintf("Rename profile %s\n\nNew name: %s", m.selectedProfile, m.profileInput)
}

func (m Model) profileDeleteConfirmView() string {
	return fmt.Sprintf(
		"Delete profile %s?\n\nThis removes only Kubewisp's saved profile. It does not delete the GKE cluster.",
		m.selectedProfile,
	)
}

func (m Model) dashboardView() string {
	status := "connected"
	if m.connectivity.ServerVersion == "" {
		status = "unknown"
	}
	healthy, completed, warnings, unhealthy := podHealthCounts(m.pods)
	profile := m.dependencies.Profile
	clusterCard := dashboardCard("Cluster", []string{
		"Connection: " + status,
		"Kubernetes: " + valueOrDash(m.connectivity.ServerVersion),
		"Project: " + valueOrDash(profile.ProjectID),
		"Cluster: " + valueOrDash(profile.ClusterName),
		"Location: " + locationLabel(profile),
		"Namespace: " + selectedNamespace(profile),
	})
	podCard := dashboardCard("Pod Health", []string{
		healthyStyle.Render(fmt.Sprintf("● healthy %d", healthy)),
		mutedStyle.Render(fmt.Sprintf("● completed %d", completed)),
		warningStyle.Render(fmt.Sprintf("● warning %d", warnings)),
		errorStyle.Render(fmt.Sprintf("● unhealthy %d", unhealthy)),
		fmt.Sprintf("Total: %d", len(m.pods)),
	})
	dependencyCard := dashboardCard("Local Dependencies", dependencyStatusLines(m.doctorReport))
	if m.width >= 105 {
		return lipgloss.JoinHorizontal(lipgloss.Top, clusterCard, " ", podCard, " ", dependencyCard)
	}
	return lipgloss.JoinVertical(lipgloss.Left, clusterCard, podCard, dependencyCard)
}

func dashboardCard(title string, lines []string) string {
	content := titleStyle.Render(title) + "\n\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(30).
		Render(content)
}

func dependencyStatusLines(report doctor.Report) []string {
	if len(report.Checks) == 0 {
		return []string{mutedStyle.Render("Checking...")}
	}
	lines := make([]string, 0, len(report.Checks)*3)
	for _, check := range report.Checks {
		if check.Passed() {
			lines = append(lines, healthyStyle.Render("● pass")+" "+check.Dependency.Name)
			lines = append(lines, mutedStyle.Render("  "+check.Path))
			continue
		}
		lines = append(lines, errorStyle.Render("● fail")+" "+check.Dependency.Name)
		lines = append(lines, mutedStyle.Render("  not found"))
	}
	if report.Healthy() {
		lines = append(lines, "", healthyStyle.Render("All available"))
	} else {
		lines = append(lines, "", warningStyle.Render("Run kubewisp doctor for details"))
	}
	return lines
}

func locationLabel(profile config.Profile) string {
	if profile.Location == "" {
		return "-"
	}
	if profile.LocationType == "" {
		return profile.Location
	}
	return fmt.Sprintf("%s (%s)", profile.Location, profile.LocationType)
}

func (m Model) namespaceView() string {
	namespaces := m.visibleNamespaces()
	if len(m.namespaces) == 0 {
		return "No accessible namespaces."
	}
	if len(namespaces) == 0 {
		return m.noFilterMatchesView()
	}
	var view strings.Builder
	current := selectedNamespace(m.dependencies.Profile)
	for index, namespace := range namespaces {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		active := ""
		if namespace == current {
			active = " *"
		}
		fmt.Fprintf(&view, "%s%s%s\n", cursor, namespace, active)
	}
	view.WriteString("\nEnter switches namespace for this profile.")
	return view.String()
}

func (m Model) podView() string {
	pods := m.visiblePods()
	if len(m.pods) == 0 {
		return fmt.Sprintf("No pods found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	if len(pods) == 0 {
		return m.noFilterMatchesView()
	}
	return podListView(pods, m.cursor)
}

func (m Model) workloadPodsView() string {
	pods := m.visibleWorkloadPods()
	if len(m.workloadPods) == 0 {
		return fmt.Sprintf("No pods found for %s.", workloadReference(m.selectedWorkload))
	}
	if len(pods) == 0 {
		return m.noFilterMatchesView()
	}
	var view strings.Builder
	fmt.Fprintf(&view, "Pods managed by %s\n\n", workloadReference(m.selectedWorkload))
	view.WriteString(podListView(pods, m.cursor))
	return view.String()
}

func (m Model) servicePodsView() string {
	pods := m.visibleServicePods()
	if len(m.servicePods) == 0 {
		return fmt.Sprintf("No pods selected by Service/%s.", m.parentNetwork.Name)
	}
	if len(pods) == 0 {
		return m.noFilterMatchesView()
	}
	var view strings.Builder
	fmt.Fprintf(&view, "Pods selected by Service/%s\n\n", m.parentNetwork.Name)
	view.WriteString(podListView(pods, m.cursor))
	return view.String()
}

func podListView(pods []kube.PodSummary, cursorIndex int) string {
	rows := make([]tableRow, 0, len(pods))
	for index, pod := range pods {
		cursor := "  "
		if index == cursorIndex {
			cursor = "> "
		}
		rows = append(rows, tableRow{cursor: cursor, cells: []string{
			podStatusMarker(pod),
			pod.Name,
			pod.Ready,
			pod.Status,
			fmt.Sprintf("%d", pod.Restarts),
			fmt.Sprintf("%d", pod.WarningCount),
			formatAge(time.Now(), pod.CreatedAt),
		}})
	}
	return renderTable([]tableColumn{
		{header: "HEALTH"},
		{header: "NAME"},
		{header: "READY", alignRight: true},
		{header: "STATUS"},
		{header: "RESTARTS", alignRight: true},
		{header: "WARNINGS", alignRight: true},
		{header: "AGE", alignRight: true},
	}, rows)
}

func (m Model) workloadView() string {
	workloads := m.visibleWorkloads()
	if len(m.workloads) == 0 {
		return fmt.Sprintf("No workloads found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	if len(workloads) == 0 {
		return m.noFilterMatchesView()
	}
	rows := make([]tableRow, 0, len(workloads))
	for index, workload := range workloads {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		rows = append(rows, tableRow{cursor: cursor, cells: []string{
			workloadStatusMarker(workload),
			workload.Kind,
			workload.Name,
			workloadStatusText(workload),
			fmt.Sprintf("%d", workload.WarningCount),
			valueOrDash(workload.Schedule),
			formatAge(time.Now(), workload.LastScheduleTime),
			formatAge(time.Now(), workload.CreatedAt),
		}})
	}
	return renderTable([]tableColumn{
		{header: "HEALTH"},
		{header: "KIND"},
		{header: "NAME"},
		{header: "STATUS"},
		{header: "WARNINGS", alignRight: true},
		{header: "SCHEDULE"},
		{header: "LAST RUN", alignRight: true},
		{header: "AGE", alignRight: true},
	}, rows)
}

func (m Model) networkView() string {
	resources := m.visibleNetworkResources()
	if len(m.networkResources) == 0 {
		return fmt.Sprintf("No Services or Ingresses found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	if len(resources) == 0 {
		return m.noFilterMatchesView()
	}
	return networkListView(resources, m.cursor)
}

func (m Model) ingressServicesView() string {
	services := m.visibleIngressServices()
	if len(m.ingressServices) == 0 {
		return fmt.Sprintf("No backend Services found for Ingress/%s.", m.parentNetwork.Name)
	}
	if len(services) == 0 {
		return m.noFilterMatchesView()
	}
	var view strings.Builder
	fmt.Fprintf(&view, "Services used by Ingress/%s\n\n", m.parentNetwork.Name)
	view.WriteString(networkListView(services, m.cursor))
	return view.String()
}

func networkListView(resources []kube.NetworkSummary, cursorIndex int) string {
	rows := make([]tableRow, 0, len(resources))
	for index, resource := range resources {
		cursor := "  "
		if index == cursorIndex {
			cursor = "> "
		}
		exposure := strings.Join(resource.Ports, ", ")
		if resource.Kind == "Ingress" {
			exposure = strings.Join(resource.Hosts, ", ")
		}
		rows = append(rows, tableRow{cursor: cursor, cells: []string{
			resource.Kind,
			resource.Name,
			resource.Type,
			valueOrDash(resource.Address),
			valueOrDash(exposure),
			formatAge(time.Now(), resource.CreatedAt),
		}})
	}
	return renderTable([]tableColumn{
		{header: "KIND"},
		{header: "NAME"},
		{header: "TYPE/CLASS"},
		{header: "ADDRESS"},
		{header: "PORTS/HOSTS"},
		{header: "AGE", alignRight: true},
	}, rows)
}

func (m Model) eventView() string {
	events := m.visibleEvents()
	if len(m.events) == 0 {
		return fmt.Sprintf("No warning events found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	if len(events) == 0 {
		return m.noFilterMatchesView()
	}
	rows := make([]tableRow, 0, len(events))
	for index, event := range events {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		rows = append(rows, tableRow{cursor: cursor, cells: []string{
			warningStyle.Render(formatAge(time.Now(), event.LastSeen)),
			fmt.Sprintf("%d", event.Count),
			event.ObjectKind + "/" + event.ObjectName,
			event.Reason,
			event.Message,
		}})
	}
	return renderTable([]tableColumn{
		{header: "LAST SEEN", alignRight: true},
		{header: "COUNT", alignRight: true},
		{header: "OBJECT"},
		{header: "REASON"},
		{header: "MESSAGE"},
	}, rows)
}

func renderTable(columns []tableColumn, rows []tableRow) string {
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = lipgloss.Width(column.header)
	}
	for _, row := range rows {
		for index, cell := range row.cells {
			if index < len(widths) {
				widths[index] = max(widths[index], lipgloss.Width(cell))
			}
		}
	}

	var view strings.Builder
	headers := make([]string, len(columns))
	for index, column := range columns {
		headers[index] = tableHeadStyle.Render(padTableCell(column.header, widths[index], column.alignRight))
	}
	fmt.Fprintf(&view, "  %s\n", strings.Join(headers, mutedStyle.Render(" | ")))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for index, column := range columns {
			value := ""
			if index < len(row.cells) {
				value = row.cells[index]
			}
			cells[index] = padTableCell(value, widths[index], column.alignRight)
		}
		fmt.Fprintf(&view, "%s%s\n", row.cursor, strings.Join(cells, mutedStyle.Render(" | ")))
	}
	return view.String()
}

func padTableCell(value string, width int, alignRight bool) string {
	padding := strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
	if alignRight {
		return padding + value
	}
	return value + padding
}

func (m Model) podDetailsView() string {
	details := m.podDetails
	var view strings.Builder
	fmt.Fprintf(&view, "Pod Details: %s\n\n", details.Name)
	fmt.Fprintf(&view, "Health: %s\n", podStatusMarker(details.PodSummary))
	fmt.Fprintf(&view, "Status: %s | Ready: %s | Restarts: %d\n", details.Status, details.Ready, details.Restarts)
	fmt.Fprintf(&view, "Node: %s | Pod IP: %s | Host IP: %s\n", valueOrDash(details.Node), valueOrDash(details.PodIP), valueOrDash(details.HostIP))
	fmt.Fprintf(&view, "Service Account: %s | QoS: %s\n", valueOrDash(details.ServiceAccount), valueOrDash(details.QoSClass))
	writeDetailSection(&view, "Owners", details.Owners)
	fmt.Fprintln(&view, "\nConditions:")
	if len(details.Conditions) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, condition := range details.Conditions {
		fmt.Fprintf(
			&view,
			"  %s=%s | reason=%s | %s\n",
			condition.Type,
			condition.Status,
			valueOrDash(condition.Reason),
			valueOrDash(condition.Message),
		)
	}
	fmt.Fprintln(&view, "\nContainers:")
	for _, container := range details.Containers {
		fmt.Fprintf(
			&view,
			"  %s | ready=%t | restarts=%d | %s | last=%s\n",
			container.Name,
			container.Ready,
			container.Restarts,
			container.State,
			container.LastState,
		)
		writeDetailValues(&view, "Image", []string{container.Image})
		writeDetailValues(&view, "Ports", container.Ports)
		writeDetailValues(&view, "Requests", container.Requests)
		writeDetailValues(&view, "Limits", container.Limits)
		writeDetailValues(&view, "Mounts", container.Mounts)
		writeDetailValues(&view, "Probes", container.Probes)
		writeDetailValues(&view, "Environment names", container.EnvironmentNames)
	}
	writeDetailSection(&view, "Volumes", details.Volumes)
	writeDetailSection(&view, "Labels", details.Labels)
	writeDetailSection(&view, "Annotation names", details.Annotations)
	fmt.Fprintln(&view, "\nRecent Events:")
	if details.EventsWarning != "" {
		fmt.Fprintf(&view, "  Warning: %s\n", details.EventsWarning)
	}
	for _, event := range details.Events {
		fmt.Fprintf(&view, "  %s %s | %s\n", event.Type, event.Reason, event.Message)
	}
	if len(details.Events) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	return view.String()
}

func writeDetailSection(view *strings.Builder, title string, values []string) {
	fmt.Fprintf(view, "\n%s:\n", title)
	if len(values) == 0 {
		fmt.Fprintln(view, "  -")
		return
	}
	for _, value := range values {
		fmt.Fprintf(view, "  %s\n", value)
	}
}

func writeDetailValues(view *strings.Builder, title string, values []string) {
	if len(values) > 0 {
		fmt.Fprintf(view, "    %s: %s\n", title, strings.Join(values, ", "))
	}
}

func (m Model) podLogsView() string {
	return fmt.Sprintf("Logs: %s (latest 200 lines)\n\n%s", m.selectedPod, m.logs)
}

func (m Model) scrollView(content string) string {
	content = m.wrapContent(content)
	lines := strings.Split(content, "\n")
	visible := m.height - 9
	if visible <= 0 || len(lines) <= visible {
		return content
	}
	scroll := min(m.scroll, len(lines)-visible)
	return strings.Join(lines[scroll:scroll+visible], "\n") +
		"\n\n" + mutedStyle.Render(fmt.Sprintf("lines %d-%d of %d", scroll+1, scroll+visible, len(lines)))
}

func (m Model) wrapContent(content string) string {
	if m.width <= 0 {
		return content
	}
	return ansi.Wrap(content, m.width, "")
}

func (m Model) containerView() string {
	var view strings.Builder
	fmt.Fprintf(&view, "Select container for %s\n\n", m.selectedPod)
	for index, container := range m.containers {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, container)
	}
	return view.String()
}

func (m Model) portForwardView() string {
	var view strings.Builder
	fmt.Fprintf(&view, "Select TCP port to forward for %s\n\n", m.selectedPod)
	if len(m.ports) == 0 {
		view.WriteString("No declared TCP container ports. Use the CLI with --port for an undeclared port.")
		return view.String()
	}
	for index, port := range m.ports {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, podPortLabel(port))
	}
	view.WriteString("\nThe local port will match the selected pod port.")
	view.WriteString("\nPress Ctrl+C while forwarding to stop and return to Kubewisp.")
	return view.String()
}

func (m Model) execContainerView() string {
	var view strings.Builder
	fmt.Fprintf(&view, "Select container for exec in %s\n\n", m.selectedPod)
	if len(m.containers) == 0 {
		view.WriteString("Pod has no containers.")
		return view.String()
	}
	for index, container := range m.containers {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, container)
	}
	return view.String()
}

func (m Model) execConfirmView() string {
	profile := m.dependencies.Profile
	var view strings.Builder
	fmt.Fprintln(&view, "Exec target:")
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  Pod: %s\n", m.selectedPod)
	fmt.Fprintf(&view, "  Container: %s\n", m.selectedContainer)
	fmt.Fprintln(&view, "  Shell: /bin/sh")
	if profile.Production {
		fmt.Fprintln(&view, "\nPRODUCTION profile. Press y to open this shell.")
	} else {
		fmt.Fprintln(&view, "\nPress Enter to open this shell.")
	}
	return view.String()
}

func (m Model) podActionConfirmView() string {
	profile := m.dependencies.Profile
	var view strings.Builder
	fmt.Fprintf(&view, "%s target:\n", strings.Title(m.podAction))
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  Pod: %s\n", m.selectedPod)
	fmt.Fprintf(&view, "  Controller: %s\n", valueOrDash(m.podActionInfo.ControllerOwner))
	if m.podAction == "restart" {
		fmt.Fprintln(&view, "\nRestart deletes this pod so its controller recreates it.")
	}
	if profile.Production {
		fmt.Fprintf(&view, "\nPRODUCTION: type the exact pod name to confirm:\n> %s", m.confirmationInput)
	} else {
		fmt.Fprintf(&view, "\nPress y to confirm %s.", m.podAction)
	}
	return view.String()
}

func (m Model) workloadRestartConfirmView() string {
	profile := m.dependencies.Profile
	workload := m.selectedWorkload
	var view strings.Builder
	fmt.Fprintln(&view, "Rollout restart target:")
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  Workload: %s\n", workloadReference(workload))
	fmt.Fprintf(&view, "  Ready: %d/%d\n", workload.Ready, workload.Desired)
	fmt.Fprintln(&view, "\nThis will restart every pod managed by this workload.")
	if profile.Production {
		fmt.Fprintf(&view, "\nPRODUCTION: type the exact Kind/name to confirm:\n> %s", m.confirmationInput)
	} else {
		fmt.Fprintln(&view, "\nPress y to confirm rollout restart.")
	}
	return view.String()
}

func (m Model) workloadDetailsView() string {
	details := m.workloadDetails
	var view strings.Builder
	fmt.Fprintf(&view, "%s Details: %s\n\n", details.Kind, details.Name)
	fmt.Fprintf(&view, "Health: %s\n", workloadStatusMarker(details.WorkloadSummary))
	fmt.Fprintf(
		&view,
		"Ready: %d/%d | Updated: %d | Available: %d | Age: %s\n",
		details.Ready,
		details.Desired,
		details.Updated,
		details.Available,
		formatAge(time.Now(), details.CreatedAt),
	)
	fmt.Fprintf(&view, "Strategy: %s\n", valueOrDash(details.Strategy))
	fmt.Fprintf(&view, "Selector: %s\n", valueOrDash(details.Selector))
	fmt.Fprintf(&view, "Service Account: %s\n", valueOrDash(details.ServiceAccount))
	writeDetailSection(&view, "Pod Template Containers", details.Containers)
	fmt.Fprintln(&view, "\nConditions:")
	if len(details.Conditions) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, condition := range details.Conditions {
		fmt.Fprintf(
			&view,
			"  %s=%s | reason=%s | %s\n",
			condition.Type,
			condition.Status,
			valueOrDash(condition.Reason),
			valueOrDash(condition.Message),
		)
	}
	return view.String()
}

func (m Model) resourceDiagnosticsView() string {
	report := m.diagnostics
	var view strings.Builder
	fmt.Fprintf(&view, "Diagnostics: %s/%s\n\n", report.ResourceKind, report.ResourceName)
	fmt.Fprintf(&view, "Summary: %s\n", report.Summary)
	fmt.Fprintln(&view, "\nPossible Causes:")
	if len(report.Causes) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, cause := range report.Causes {
		fmt.Fprintf(&view, "  %s\n", cause)
	}
	fmt.Fprintln(&view, "\nRelated Warning Events:")
	if len(report.Events) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, event := range report.Events {
		fmt.Fprintf(
			&view,
			"  %s/%s | %s | count=%d | last=%s\n    %s\n",
			event.ObjectKind,
			event.ObjectName,
			event.Reason,
			event.Count,
			formatAge(time.Now(), event.LastSeen),
			event.Message,
		)
	}
	return view.String()
}

func (m Model) workloadRolloutView() string {
	progress := m.rolloutProgress
	var view strings.Builder
	fmt.Fprintf(&view, "Rollout Progress: %s\n\n", workloadReference(progress.WorkloadSummary))
	state := warningStyle.Render("● progressing")
	if progress.Complete {
		state = healthyStyle.Render("● complete")
	} else if strings.EqualFold(progress.Status, "Stalled") {
		state = errorStyle.Render("● stalled")
	}
	fmt.Fprintf(&view, "State: %s\n", state)
	fmt.Fprintf(
		&view,
		"Ready: %d/%d | Updated: %d/%d | Available: %d/%d\n",
		progress.Ready, progress.Desired,
		progress.Updated, progress.Desired,
		progress.Available, progress.Desired,
	)
	fmt.Fprintf(&view, "Generation: %d | Observed: %d\n", progress.Generation, progress.ObservedGeneration)
	if progress.Revision != "" {
		fmt.Fprintf(&view, "Revision: %s\n", progress.Revision)
	}
	if progress.CurrentRevision != "" || progress.UpdateRevision != "" {
		fmt.Fprintf(
			&view,
			"Current Revision: %s | Update Revision: %s\n",
			valueOrDash(progress.CurrentRevision),
			valueOrDash(progress.UpdateRevision),
		)
	}
	fmt.Fprintf(&view, "Restarted: %s\n", formatAge(time.Now(), progress.RestartedAt))
	fmt.Fprintf(
		&view,
		"Status: %s | Reason: %s\n",
		valueOrDash(progress.Status),
		valueOrDash(progress.Reason),
	)
	if progress.Message != "" {
		fmt.Fprintf(&view, "Message: %s\n", progress.Message)
	}
	fmt.Fprintln(&view, "\nPods:")
	if len(progress.Pods) == 0 {
		fmt.Fprintln(&view, "  -")
	} else {
		view.WriteString(podListView(progress.Pods, -1))
	}
	return view.String()
}

func (m Model) networkDetailsView() string {
	details := m.networkDetails
	var view strings.Builder
	fmt.Fprintf(&view, "%s Details: %s\n\n", details.Kind, details.Name)
	fmt.Fprintf(&view, "Type/Class: %s\n", valueOrDash(details.Type))
	fmt.Fprintf(&view, "Address: %s\n", valueOrDash(details.Address))
	fmt.Fprintf(&view, "Age: %s\n", formatAge(time.Now(), details.CreatedAt))
	writeDetailSection(&view, "Ports", details.Ports)
	writeDetailSection(&view, "Hosts", details.Hosts)
	writeDetailSection(&view, "Selector", details.Selector)
	writeDetailSection(&view, "Ready Endpoints", details.Endpoints)
	writeDetailSection(&view, "Routes", details.Routes)
	return view.String()
}

func (m Model) cronJobDetailsView() string {
	details := m.cronJobDetails
	var view strings.Builder
	fmt.Fprintf(&view, "CronJob Details: %s\n\n", details.Name)
	fmt.Fprintf(&view, "State: %s\n", workloadStatusMarker(details.WorkloadSummary))
	fmt.Fprintf(&view, "Schedule: %s | Suspended: %t | Active Jobs: %d\n", details.Schedule, details.Suspended, details.Active)
	fmt.Fprintf(&view, "Concurrency Policy: %s\n", valueOrDash(details.ConcurrencyPolicy))
	fmt.Fprintf(&view, "Last Scheduled: %s | Last Successful: %s\n", formatAge(time.Now(), details.LastScheduleTime), formatAge(time.Now(), details.LastSuccessfulTime))
	fmt.Fprintln(&view, "\nRecent Jobs:")
	if len(details.Jobs) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, job := range details.Jobs {
		fmt.Fprintf(
			&view,
			"  %s | %s | active=%d succeeded=%d failed=%d | age=%s\n",
			job.Name,
			job.Status,
			job.Active,
			job.Succeeded,
			job.Failed,
			formatAge(time.Now(), job.CreatedAt),
		)
	}
	return view.String()
}

func (m Model) cronJobStateConfirmView() string {
	profile := m.dependencies.Profile
	cronJob := m.selectedWorkload
	var view strings.Builder
	fmt.Fprintf(&view, "CronJob %s target:\n", cronJobStateAction(m.cronJobSuspended))
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  CronJob: %s\n", cronJob.Name)
	fmt.Fprintf(&view, "  Schedule: %s\n", cronJob.Schedule)
	fmt.Fprintf(&view, "  Current state: %s\n", cronJobStateWord(cronJob.Suspended))
	fmt.Fprintf(&view, "  New state: %s\n", cronJobStateWord(m.cronJobSuspended))
	if profile.Production {
		fmt.Fprintf(&view, "\nPRODUCTION: type the exact CronJob/name to confirm:\n> %s", m.confirmationInput)
	} else {
		fmt.Fprintf(&view, "\nPress y to confirm %s.\n", cronJobStateAction(m.cronJobSuspended))
	}
	return view.String()
}

func (m Model) beginCronJobState() (tea.Model, tea.Cmd) {
	m.confirmationInput = ""
	m.cronJobSuspended = !m.selectedWorkload.Suspended
	m.status = ""
	m.err = nil
	m.screen = cronJobStateConfirmScreen
	m.loading = false
	return m, nil
}

func (m Model) beginPodAction(action string) (tea.Model, tea.Cmd) {
	if m.screen == podScreen {
		m.selectedPod = m.visiblePods()[m.cursor].Name
	}
	m.podAction = action
	m.confirmationInput = ""
	m.loading = true
	m.err = nil
	m.status = ""
	return m, m.loadPodActionInfo()
}

func (m Model) executePodAction() (tea.Model, tea.Cmd) {
	m.loading = true
	m.confirmationInput = ""
	namespace := selectedNamespace(m.dependencies.Profile)
	action := m.podAction
	pod := m.selectedPod
	return m, func() tea.Msg {
		err := m.dependencies.Pods.Delete(context.Background(), namespace, pod)
		return podDeletedMsg{action: action, err: err}
	}
}

func (m Model) executeWorkloadRestart() (tea.Model, tea.Cmd) {
	m.loading = true
	m.confirmationInput = ""
	namespace := selectedNamespace(m.dependencies.Profile)
	workload := m.selectedWorkload
	return m, func() tea.Msg {
		err := m.dependencies.Workloads.RolloutRestart(context.Background(), namespace, workload.Kind, workload.Name)
		return workloadRestartedMsg{err: err}
	}
}

func (m Model) executeCronJobState() (tea.Model, tea.Cmd) {
	m.loading = true
	m.confirmationInput = ""
	namespace := selectedNamespace(m.dependencies.Profile)
	cronJob := m.selectedWorkload
	suspended := m.cronJobSuspended
	return m, func() tea.Msg {
		err := m.dependencies.Workloads.SetCronJobSuspended(context.Background(), namespace, cronJob.Name, suspended)
		return cronJobStateMsg{suspended: suspended, err: err}
	}
}

func (m Model) startExec() (tea.Model, tea.Cmd) {
	if m.dependencies.Exec == nil {
		m.err = errors.New("kubectl exec service is not configured")
		return m, nil
	}
	m.status = ""
	return m, tea.Exec(
		&execCommand{
			executor: m.dependencies.Exec,
			options: kubectl.ExecOptions{
				Namespace: selectedNamespace(m.dependencies.Profile),
				Pod:       m.selectedPod,
				Container: m.selectedContainer,
				Command:   "/bin/sh",
			},
		},
		func(err error) tea.Msg {
			return execFinishedMsg{err: err}
		},
	)
}

func (m Model) loadPodActionInfo() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		info, err := m.dependencies.Pods.ActionInfo(context.Background(), namespace, m.selectedPod)
		return podActionInfoMsg{info: info, err: err}
	}
}

func (m Model) loadCurrent() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	switch m.screen {
	case dashboardScreen:
		return func() tea.Msg {
			report, err := m.dependencies.Connectivity.Check(context.Background(), namespace)
			return connectivityMsg{report: report, err: err}
		}
	case namespaceScreen:
		return func() tea.Msg {
			names, err := m.dependencies.Namespaces.List(context.Background())
			return namespacesMsg{names: names, err: err}
		}
	case podScreen:
		return func() tea.Msg {
			pods, err := m.dependencies.Pods.List(context.Background(), namespace)
			return podsMsg{pods: pods, err: err}
		}
	case workloadScreen:
		return m.loadWorkloads()
	case networkScreen:
		return m.loadNetwork()
	case eventScreen:
		return m.loadEvents()
	case podDetailsScreen:
		return m.loadPodDetails()
	case containerScreen:
		return m.loadContainers()
	case execContainerScreen:
		return m.loadContainers()
	case execConfirmScreen:
		return nil
	case podActionConfirmScreen:
		return nil
	case workloadRestartConfirmScreen:
		return nil
	case workloadDetailsScreen:
		return m.loadWorkloadDetails()
	case workloadRolloutScreen:
		return m.loadRolloutProgress()
	case workloadPodsScreen:
		return m.loadWorkloadPods()
	case resourceDiagnosticsScreen:
		return m.loadDiagnostics(m.diagnostics.ResourceKind, m.diagnostics.ResourceName)
	case servicePodsScreen:
		return m.loadServicePods()
	case networkDetailsScreen:
		return m.loadNetworkDetails()
	case ingressServicesScreen:
		return m.loadIngressServices()
	case cronJobDetailsScreen:
		return m.loadCronJobDetails()
	case cronJobStateConfirmScreen:
		return nil
	case profileScreen:
		return m.loadProfiles()
	case profileRenameScreen:
		return nil
	case profileDeleteConfirmScreen:
		return nil
	case portForwardScreen:
		return m.loadPorts()
	case podLogsScreen:
		return m.loadLogs(m.selectedContainer)
	default:
		return nil
	}
}

func (m Model) loadDashboardData() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		report := doctor.Report{}
		if m.dependencies.Doctor != nil {
			report = m.dependencies.Doctor.Run(context.Background())
		}
		pods, err := m.dependencies.Pods.List(context.Background(), namespace)
		return dashboardDataMsg{pods: pods, report: report, err: err}
	}
}

func (m Model) loadWorkloadDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return workloadDetailsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		details, err := m.dependencies.Workloads.Describe(
			context.Background(),
			namespace,
			m.selectedWorkload.Kind,
			m.selectedWorkload.Name,
		)
		return workloadDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadDiagnostics(kind, name string) tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Events == nil {
			return diagnosticsMsg{err: errors.New("Kubernetes event service is not configured")}
		}
		report, err := m.dependencies.Events.Diagnose(context.Background(), namespace, kind, name)
		return diagnosticsMsg{report: report, err: err}
	}
}

func (m Model) loadRolloutProgress() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return rolloutProgressMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		progress, err := m.dependencies.Workloads.RolloutProgress(
			context.Background(),
			namespace,
			m.selectedWorkload.Kind,
			m.selectedWorkload.Name,
		)
		return rolloutProgressMsg{progress: progress, err: err}
	}
}

func rolloutTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(now time.Time) tea.Msg {
		return rolloutTickMsg(now)
	})
}

func rolloutStillRunning(progress kube.RolloutProgress) bool {
	return !progress.Complete && !strings.EqualFold(progress.Status, "Stalled")
}

func (m Model) monitoringRollout() bool {
	return m.screen == workloadRolloutScreen
}

func (m Model) loadCronJobDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return cronJobDetailsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		details, err := m.dependencies.Workloads.DescribeCronJob(context.Background(), namespace, m.selectedWorkload.Name)
		return cronJobDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadWorkloadPods() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return workloadPodsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		pods, err := m.dependencies.Workloads.Pods(
			context.Background(),
			namespace,
			m.selectedWorkload.Kind,
			m.selectedWorkload.Name,
		)
		return workloadPodsMsg{pods: pods, err: err}
	}
}

func (m Model) loadServicePods() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Network == nil {
			return servicePodsMsg{err: errors.New("Kubernetes network service is not configured")}
		}
		pods, err := m.dependencies.Network.PodsForService(context.Background(), namespace, m.parentNetwork.Name)
		return servicePodsMsg{pods: pods, err: err}
	}
}

func (m Model) loadIngressServices() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Network == nil {
			return ingressServicesMsg{err: errors.New("Kubernetes network service is not configured")}
		}
		services, err := m.dependencies.Network.ServicesForIngress(context.Background(), namespace, m.parentNetwork.Name)
		return ingressServicesMsg{services: services, err: err}
	}
}

func (m Model) loadPodOwnerWorkload() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return podOwnerWorkloadMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		workload, err := m.dependencies.Workloads.OwnerForPod(context.Background(), namespace, m.selectedPod)
		return podOwnerWorkloadMsg{workload: workload, err: err}
	}
}

func (m Model) loadWorkloads() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return workloadsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		workloads, err := m.dependencies.Workloads.List(context.Background(), namespace)
		return workloadsMsg{workloads: workloads, err: err}
	}
}

func (m Model) loadNetwork() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Network == nil {
			return networkMsg{err: errors.New("Kubernetes network service is not configured")}
		}
		resources, err := m.dependencies.Network.List(context.Background(), namespace)
		return networkMsg{resources: resources, err: err}
	}
}

func (m Model) loadNetworkDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Network == nil {
			return networkDetailsMsg{err: errors.New("Kubernetes network service is not configured")}
		}
		details, err := m.dependencies.Network.Describe(
			context.Background(), namespace, m.selectedNetwork.Kind, m.selectedNetwork.Name,
		)
		return networkDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadEvents() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Events == nil {
			return eventsMsg{err: errors.New("Kubernetes event service is not configured")}
		}
		events, err := m.dependencies.Events.ListWarnings(context.Background(), namespace)
		return eventsMsg{events: events, err: err}
	}
}

func (m Model) loadProfiles() tea.Cmd {
	return func() tea.Msg {
		cfg, err := (config.Store{Path: m.dependencies.ConfigPath}).Load()
		if err != nil {
			return profilesMsg{err: err}
		}
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		return profilesMsg{names: names}
	}
}

func (m Model) switchProfile(name string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := (config.Store{Path: m.dependencies.ConfigPath}).Load()
		if err != nil {
			return profileSwitchedMsg{name: name, err: err}
		}
		profile, exists := cfg.Profiles[name]
		if !exists {
			return profileSwitchedMsg{name: name, err: fmt.Errorf("profile %q does not exist", name)}
		}
		if m.dependencies.Profiles == nil {
			return profileSwitchedMsg{name: name, err: errors.New("profile connector is not configured")}
		}
		if err := m.dependencies.Profiles.Connect(context.Background(), profile); err != nil {
			return profileSwitchedMsg{name: name, err: err}
		}
		cfg.CurrentProfile = name
		if err := (config.Store{Path: m.dependencies.ConfigPath}).Save(cfg); err != nil {
			return profileSwitchedMsg{name: name, err: err}
		}
		return profileSwitchedMsg{name: name, profile: profile}
	}
}

func (m Model) renameProfile(oldName, newName string) tea.Cmd {
	return func() tea.Msg {
		store := config.Store{Path: m.dependencies.ConfigPath}
		cfg, err := store.Load()
		if err != nil {
			return profileRenamedMsg{oldName: oldName, newName: newName, err: err}
		}
		profile, exists := cfg.Profiles[oldName]
		if !exists {
			return profileRenamedMsg{oldName: oldName, newName: newName, err: fmt.Errorf("profile %q does not exist", oldName)}
		}
		if _, exists := cfg.Profiles[newName]; exists {
			return profileRenamedMsg{oldName: oldName, newName: newName, err: fmt.Errorf("profile %q already exists", newName)}
		}
		delete(cfg.Profiles, oldName)
		cfg.Profiles[newName] = profile
		if cfg.CurrentProfile == oldName {
			cfg.CurrentProfile = newName
		}
		err = store.Save(cfg)
		return profileRenamedMsg{oldName: oldName, newName: newName, err: err}
	}
}

func (m Model) deleteProfile(name string) tea.Cmd {
	return func() tea.Msg {
		store := config.Store{Path: m.dependencies.ConfigPath}
		cfg, err := store.Load()
		if err != nil {
			return profileDeletedMsg{name: name, err: err}
		}
		if name == cfg.CurrentProfile {
			return profileDeletedMsg{name: name, err: errors.New("switch away from the active profile before deleting it")}
		}
		if _, exists := cfg.Profiles[name]; !exists {
			return profileDeletedMsg{name: name, err: fmt.Errorf("profile %q does not exist", name)}
		}
		delete(cfg.Profiles, name)
		err = store.Save(cfg)
		return profileDeletedMsg{name: name, err: err}
	}
}

func (m Model) loadPorts() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		ports, err := m.dependencies.Pods.Ports(context.Background(), namespace, m.selectedPod)
		if err != nil {
			return portsMsg{err: err}
		}
		return portsMsg{ports: tcpPorts(ports)}
	}
}

func (m Model) loadPods() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		pods, err := m.dependencies.Pods.List(context.Background(), namespace)
		return podsMsg{pods: pods, err: err}
	}
}

func (m Model) loadPodDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		details, err := m.dependencies.Pods.Describe(context.Background(), namespace, m.selectedPod)
		return podDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadContainers() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		names, err := m.dependencies.Pods.Containers(context.Background(), namespace, m.selectedPod)
		return containersMsg{names: names, err: err}
	}
}

func (m Model) loadLogs(container string) tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		stream, err := m.dependencies.Pods.Logs(context.Background(), kube.PodLogsOptions{
			Namespace: namespace,
			Pod:       m.selectedPod,
			Container: container,
			TailLines: 200,
		})
		if err != nil {
			return podLogsMsg{err: err}
		}
		defer stream.Close()
		logs, err := io.ReadAll(stream)
		return podLogsMsg{logs: string(logs), err: err}
	}
}

func (m Model) switchNamespace(namespace string) tea.Cmd {
	return func() tea.Msg {
		if err := m.dependencies.Namespaces.Exists(context.Background(), namespace); err != nil {
			return namespaceSwitchedMsg{err: err}
		}
		store := config.Store{Path: m.dependencies.ConfigPath}
		cfg, err := store.Load()
		if err != nil {
			return namespaceSwitchedMsg{err: err}
		}
		profile := cfg.Profiles[m.dependencies.ProfileName]
		profile.CurrentNamespace = namespace
		cfg.Profiles[m.dependencies.ProfileName] = profile
		if err := store.Save(cfg); err != nil {
			return namespaceSwitchedMsg{err: err}
		}
		return namespaceSwitchedMsg{namespace: namespace}
	}
}

func (m *Model) clampCursor(length int) {
	if length == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= length {
		m.cursor = length - 1
	}
}

func (m Model) itemCount() int {
	switch m.screen {
	case namespaceScreen:
		return len(m.visibleNamespaces())
	case podScreen:
		return len(m.visiblePods())
	case workloadPodsScreen:
		return len(m.visibleWorkloadPods())
	case servicePodsScreen:
		return len(m.visibleServicePods())
	case workloadScreen:
		return len(m.visibleWorkloads())
	case networkScreen:
		return len(m.visibleNetworkResources())
	case ingressServicesScreen:
		return len(m.visibleIngressServices())
	case eventScreen:
		return len(m.visibleEvents())
	case profileScreen:
		return len(m.profileNames)
	case containerScreen:
		return len(m.containers)
	case execContainerScreen:
		return len(m.containers)
	case portForwardScreen:
		return len(m.ports)
	default:
		return 0
	}
}

func (m Model) isFilterableScreen() bool {
	switch m.screen {
	case namespaceScreen, podScreen, workloadScreen, networkScreen, eventScreen,
		workloadPodsScreen, servicePodsScreen, ingressServicesScreen:
		return true
	default:
		return false
	}
}

func (m *Model) clearFilter() {
	m.filtering = false
	m.filterQuery = ""
	m.filterScreen = dashboardScreen
	m.cursor = 0
}

func (m Model) filterView() string {
	state := "applied"
	if m.filtering {
		state = "editing"
	}
	return activeTabStyle.Render(fmt.Sprintf(
		"Filter /%s (%s, %d of %d)",
		m.filterQuery,
		state,
		m.itemCount(),
		m.unfilteredItemCount(),
	))
}

func (m Model) noFilterMatchesView() string {
	return fmt.Sprintf("No resources match filter %q.", m.filterQuery)
}

func (m Model) filterMatches(target screen, values ...string) bool {
	if m.filterScreen != target || strings.TrimSpace(m.filterQuery) == "" {
		return true
	}
	query := strings.ToLower(strings.TrimSpace(m.filterQuery))
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (m Model) visibleNamespaces() []string {
	result := make([]string, 0, len(m.namespaces))
	for _, namespace := range m.namespaces {
		if m.filterMatches(namespaceScreen, namespace) {
			result = append(result, namespace)
		}
	}
	return result
}

func (m Model) visiblePodsFor(target screen, pods []kube.PodSummary) []kube.PodSummary {
	result := make([]kube.PodSummary, 0, len(pods))
	for _, pod := range pods {
		if m.filterMatches(target, pod.Name, pod.Status, pod.Ready, pod.Node, pod.OwnerKind, pod.OwnerName) {
			result = append(result, pod)
		}
	}
	return result
}

func (m Model) visiblePods() []kube.PodSummary {
	return m.visiblePodsFor(podScreen, m.pods)
}

func (m Model) visibleWorkloadPods() []kube.PodSummary {
	return m.visiblePodsFor(workloadPodsScreen, m.workloadPods)
}

func (m Model) visibleServicePods() []kube.PodSummary {
	return m.visiblePodsFor(servicePodsScreen, m.servicePods)
}

func (m Model) visibleWorkloads() []kube.WorkloadSummary {
	result := make([]kube.WorkloadSummary, 0, len(m.workloads))
	for _, workload := range m.workloads {
		if m.filterMatches(
			workloadScreen,
			workload.Kind,
			workload.Name,
			workloadStatusText(workload),
			workload.Schedule,
		) {
			result = append(result, workload)
		}
	}
	return result
}

func (m Model) visibleNetworkFor(target screen, resources []kube.NetworkSummary) []kube.NetworkSummary {
	result := make([]kube.NetworkSummary, 0, len(resources))
	for _, resource := range resources {
		if m.filterMatches(
			target,
			resource.Kind,
			resource.Name,
			resource.Type,
			resource.Address,
			strings.Join(resource.Ports, " "),
			strings.Join(resource.Hosts, " "),
		) {
			result = append(result, resource)
		}
	}
	return result
}

func (m Model) visibleNetworkResources() []kube.NetworkSummary {
	return m.visibleNetworkFor(networkScreen, m.networkResources)
}

func (m Model) visibleIngressServices() []kube.NetworkSummary {
	return m.visibleNetworkFor(ingressServicesScreen, m.ingressServices)
}

func (m Model) visibleEvents() []kube.NamespaceEventSummary {
	result := make([]kube.NamespaceEventSummary, 0, len(m.events))
	for _, event := range m.events {
		if m.filterMatches(eventScreen, event.ObjectKind, event.ObjectName, event.Reason, event.Message) {
			result = append(result, event)
		}
	}
	return result
}

func (m Model) unfilteredItemCount() int {
	switch m.screen {
	case namespaceScreen:
		return len(m.namespaces)
	case podScreen:
		return len(m.pods)
	case workloadPodsScreen:
		return len(m.workloadPods)
	case servicePodsScreen:
		return len(m.servicePods)
	case workloadScreen:
		return len(m.workloads)
	case networkScreen:
		return len(m.networkResources)
	case ingressServicesScreen:
		return len(m.ingressServices)
	case eventScreen:
		return len(m.events)
	default:
		return m.itemCount()
	}
}

func (m Model) isNestedScreen() bool {
	return m.isNestedTarget(m.screen)
}

func (m Model) isConfirmationScreen() bool {
	return m.screen == execConfirmScreen || m.screen == podActionConfirmScreen ||
		m.screen == workloadRestartConfirmScreen || m.screen == cronJobStateConfirmScreen ||
		m.screen == profileDeleteConfirmScreen
}

func (m Model) isNestedTarget(target screen) bool {
	return target == podDetailsScreen || target == podLogsScreen ||
		target == containerScreen || target == portForwardScreen ||
		target == execContainerScreen || target == execConfirmScreen ||
		target == podActionConfirmScreen || target == workloadRestartConfirmScreen ||
		target == workloadDetailsScreen || target == workloadRolloutScreen ||
		target == workloadPodsScreen || target == resourceDiagnosticsScreen || target == networkDetailsScreen ||
		target == servicePodsScreen || target == ingressServicesScreen ||
		target == cronJobDetailsScreen ||
		target == cronJobStateConfirmScreen || target == profileScreen ||
		target == profileRenameScreen || target == profileDeleteConfirmScreen
}

func (m *Model) markLoaded(value screen) {
	if m.err == nil {
		m.loadedAt[value] = m.now()
	}
}

func (m Model) cacheFresh(value screen) bool {
	loaded, ok := m.loadedAt[value]
	return ok && m.now().Sub(loaded) < m.cacheTTL
}

func (m *Model) resetClusterData() {
	m.clearFilter()
	m.connectivity = kube.ConnectivityReport{}
	m.namespaces = nil
	m.pods = nil
	m.podDetails = kube.PodDetails{}
	m.logs = ""
	m.containers = nil
	m.workloads = nil
	m.workloadPods = nil
	m.rolloutProgress = kube.RolloutProgress{}
	m.rolloutTickPending = false
	m.diagnostics = kube.ResourceDiagnostics{}
	m.servicePods = nil
	m.networkResources = nil
	m.ingressServices = nil
	m.networkDetails = kube.NetworkDetails{}
	m.parentNetwork = kube.NetworkSummary{}
	m.parentNetworkDetails = kube.NetworkDetails{}
	m.podBackScreen = podScreen
	m.workloadBackScreen = workloadScreen
	m.rolloutBackScreen = workloadScreen
	m.networkBackScreen = networkScreen
	m.events = nil
	m.loadedAt = make(map[screen]time.Time)
	m.cursor = 0
	m.scroll = 0
	m.err = nil
}

func cronJobStateAction(suspended bool) string {
	if suspended {
		return "suspend"
	}
	return "resume"
}

func cronJobStateWord(suspended bool) string {
	if suspended {
		return "suspended"
	}
	return "active"
}

func selectedNamespace(profile config.Profile) string {
	if profile.CurrentNamespace != "" {
		return profile.CurrentNamespace
	}
	return profile.DefaultNamespace
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func podPortLabel(port kube.PodPort) string {
	name := ""
	if port.Name != "" {
		name = port.Name + " | "
	}
	return fmt.Sprintf("%s%d/%s | container %s", name, port.Port, port.Protocol, port.Container)
}

func tcpPorts(ports []kube.PodPort) []kube.PodPort {
	var values []kube.PodPort
	for _, port := range ports {
		if strings.EqualFold(port.Protocol, "TCP") || port.Protocol == "" {
			values = append(values, port)
		}
	}
	return values
}

func isInterrupted(err error) bool {
	return errors.Is(err, context.Canceled) ||
		strings.Contains(strings.ToLower(err.Error()), "signal: interrupt")
}

type podHealth int

const (
	podHealthy podHealth = iota
	podCompleted
	podWarning
	podUnhealthy
)

func podHealthLevel(pod kube.PodSummary) podHealth {
	status := strings.ToLower(pod.Status)
	switch {
	case status == "completed", status == "succeeded":
		return podCompleted
	case strings.Contains(status, "crash"),
		strings.Contains(status, "error"),
		strings.Contains(status, "failed"),
		strings.Contains(status, "imagepull"),
		strings.Contains(status, "invalid"),
		strings.Contains(status, "unknown"):
		return podUnhealthy
	case status != "running", !allReady(pod.Ready), recentlyRestarted(pod, time.Now()):
		return podWarning
	default:
		return podHealthy
	}
}

func recentlyRestarted(pod kube.PodSummary, now time.Time) bool {
	return !pod.LastRestartAt.IsZero() && now.Sub(pod.LastRestartAt) <= 10*time.Minute
}

func workloadReference(workload kube.WorkloadSummary) string {
	return workload.Kind + "/" + workload.Name
}

func workloadStatusMarker(workload kube.WorkloadSummary) string {
	if strings.EqualFold(workload.Kind, "CronJob") {
		if workload.Suspended {
			return mutedStyle.Render("● suspended")
		}
		if workload.Active > 0 {
			return healthyStyle.Render("● running")
		}
		return healthyStyle.Render("● scheduled")
	}
	switch {
	case workload.Desired > 0 && workload.Ready == 0:
		return errorStyle.Render("● unhealthy")
	case workload.Ready != workload.Desired,
		workload.Updated != workload.Desired,
		workload.Available != workload.Desired:
		return warningStyle.Render("● warning")
	default:
		return healthyStyle.Render("● healthy")
	}
}

func workloadStatusText(workload kube.WorkloadSummary) string {
	if strings.EqualFold(workload.Kind, "CronJob") {
		status := fmt.Sprintf("active %d", workload.Active)
		if workload.Suspended {
			status += ", suspended"
		}
		if !workload.LastSuccessfulTime.IsZero() {
			status += ", last success " + formatAge(time.Now(), workload.LastSuccessfulTime)
		}
		return status
	}
	return fmt.Sprintf(
		"ready %d/%d, updated %d, available %d",
		workload.Ready,
		workload.Desired,
		workload.Updated,
		workload.Available,
	)
}

func podStatusMarker(pod kube.PodSummary) string {
	switch podHealthLevel(pod) {
	case podCompleted:
		return mutedStyle.Render("● completed")
	case podUnhealthy:
		return errorStyle.Render("● unhealthy")
	case podWarning:
		if strings.EqualFold(pod.OwnerKind, "Job") && strings.EqualFold(pod.Status, "Pending") {
			return warningStyle.Render("● pending")
		}
		return warningStyle.Render("● warning")
	default:
		if strings.EqualFold(pod.OwnerKind, "Job") {
			return healthyStyle.Render("● running")
		}
		return healthyStyle.Render("● healthy")
	}
}

func podHealthCounts(pods []kube.PodSummary) (healthy, completed, warnings, unhealthy int) {
	for _, pod := range pods {
		switch podHealthLevel(pod) {
		case podCompleted:
			completed++
		case podUnhealthy:
			unhealthy++
		case podWarning:
			warnings++
		default:
			healthy++
		}
	}
	return healthy, completed, warnings, unhealthy
}

func allReady(ready string) bool {
	parts := strings.Split(ready, "/")
	return len(parts) == 2 && parts[0] == parts[1] && parts[0] != "0"
}

func formatAge(now, createdAt time.Time) string {
	if createdAt.IsZero() {
		return "-"
	}
	age := now.Sub(createdAt)
	if age < time.Minute {
		return fmt.Sprintf("%ds", max(0, int(age.Seconds())))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}
