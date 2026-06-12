package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/nicopiov/kubewisp/internal/cli"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
	"github.com/nicopiov/kubewisp/internal/runner"
	"github.com/nicopiov/kubewisp/internal/selector"
	"github.com/nicopiov/kubewisp/internal/tui"
)

func main() {
	clientFactory := kube.NewKubeconfigClientFactory()
	connectivity := kube.NewConnectivityCheckerWithFactory(clientFactory)
	namespaces := kube.NewNamespaceServiceWithFactory(clientFactory)
	pods := kube.NewPodServiceWithFactory(clientFactory)
	workloads := kube.NewWorkloadServiceWithFactory(clientFactory)
	events := kube.NewEventServiceWithFactory(clientFactory)
	network := kube.NewNetworkServiceWithFactory(clientFactory)
	commandRunner := runner.NewOSRunner()
	doctorReporter := doctor.NewService(commandRunner)
	kubectlService := kubectl.NewService(commandRunner)
	command := cli.NewRootCommand(cli.Dependencies{
		Runner:       commandRunner,
		Connectivity: connectivity,
		Namespaces:   namespaces,
		Pods:         pods,
		Workloads:    workloads,
		Events:       events,
		PortForward:  kubectlService,
		Exec:         kubectlService,
		Selector:     selector.NewTerminal(),
		TUI:          tui.NewRunner(connectivity, namespaces, pods, workloads, network, events, doctorReporter, kubectlService, kubectlService),
		Version:      buildVersion(),
	})

	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "dev"
	}
	return info.Main.Version
}
