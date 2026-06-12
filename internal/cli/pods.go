package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
	"github.com/nicopiov/kubewisp/internal/selector"
	"github.com/spf13/cobra"
)

func newPodsCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "pods",
		Short: "Inspect pods in the selected namespace",
	}
	command.AddCommand(
		newPodsListCommand(dependencies, configPath),
		newPodsDescribeCommand(dependencies, configPath),
		newPodsLogsCommand(dependencies, configPath),
		newPodsPortForwardCommand(dependencies, configPath),
		newPodsExecCommand(dependencies, configPath),
		newPodsDeleteCommand(dependencies, configPath),
		newPodsRestartCommand(dependencies, configPath),
	)
	return command
}

func newPodsDeleteCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return newPodsDestructiveCommand(dependencies, configPath, false)
}

func newPodsRestartCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return newPodsDestructiveCommand(dependencies, configPath, true)
}

func newPodsDestructiveCommand(dependencies Dependencies, configPath *string, restart bool) *cobra.Command {
	action := "delete"
	short := "Delete a pod after confirmation"
	if restart {
		action = "restart"
		short = "Restart a controller-managed pod by deleting it"
	}
	return &cobra.Command{
		Use:   action + " [pod]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Pods == nil {
				return errors.New("Kubernetes pod service is not configured")
			}
			namespace := selectedNamespace(profile)
			pod, err := optionalPod(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "Pod selection cancelled.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("select pod for profile %q: %w", profileName, err)
			}
			info, err := dependencies.Pods.ActionInfo(command.Context(), namespace, pod)
			if err != nil {
				return err
			}
			if restart && info.ControllerOwner == "" {
				return errors.New("restart is blocked because this pod has no controller owner")
			}

			writePodActionContext(command.OutOrStdout(), action, profileName, profile, namespace, pod, info)
			confirmed, err := confirmPodAction(command, profile.Production, action, pod)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintf(command.OutOrStdout(), "%s cancelled.\n", strings.ToUpper(action[:1])+action[1:])
				return nil
			}
			if err := dependencies.Pods.Delete(command.Context(), namespace, pod); err != nil {
				return err
			}
			if restart {
				fmt.Fprintf(command.OutOrStdout(), "Pod %q deleted; controller %s will recreate it.\n", pod, info.ControllerOwner)
			} else {
				fmt.Fprintf(command.OutOrStdout(), "Pod %q deleted.\n", pod)
			}
			return nil
		},
	}
}

func confirmPodAction(command *cobra.Command, production bool, action, pod string) (bool, error) {
	reader := bufio.NewReader(command.InOrStdin())
	if production {
		value, err := promptText(reader, command.OutOrStdout(), fmt.Sprintf("PRODUCTION: type pod name %q to %s", pod, action), "")
		if err != nil {
			return false, err
		}
		return value == pod, nil
	}
	return promptYesNo(reader, command.OutOrStdout(), fmt.Sprintf("%s pod %q?", strings.Title(action), pod), false)
}

func writePodActionContext(
	output io.Writer,
	action, profileName string,
	profile config.Profile,
	namespace, pod string,
	info kube.PodActionInfo,
) {
	fmt.Fprintf(output, "%s target:\n", strings.Title(action))
	fmt.Fprintf(output, "  Profile: %s\n", profileName)
	fmt.Fprintf(output, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(output, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(output, "  Namespace: %s\n", namespace)
	fmt.Fprintf(output, "  Pod: %s\n", pod)
	fmt.Fprintf(output, "  Controller: %s\n", valueOrDash(info.ControllerOwner))
}

func newPodsExecCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	var container string
	var shell string

	command := &cobra.Command{
		Use:   "exec [pod]",
		Short: "Open an interactive shell in a pod container",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Pods == nil {
				return errors.New("Kubernetes pod service is not configured")
			}
			if dependencies.Exec == nil {
				return errors.New("kubectl exec service is not configured")
			}
			if strings.TrimSpace(shell) == "" {
				return errors.New("--shell cannot be empty")
			}

			namespace := selectedNamespace(profile)
			pod, err := optionalPod(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "Pod selection cancelled.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("select pod for profile %q: %w", profileName, err)
			}
			if container == "" {
				container, err = choosePodContainer(command, dependencies, namespace, pod)
				if errors.Is(err, selector.ErrCancelled) {
					fmt.Fprintln(command.OutOrStdout(), "Container selection cancelled.")
					return nil
				}
				if err != nil {
					return fmt.Errorf("select container for profile %q: %w", profileName, err)
				}
			}

			writeExecContext(command.OutOrStdout(), profileName, profile, namespace, pod, container, shell)
			if profile.Production {
				confirmed, err := promptYesNo(
					bufio.NewReader(command.InOrStdin()),
					command.OutOrStdout(),
					"Production profile. Open this shell?",
					false,
				)
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(command.OutOrStdout(), "Exec cancelled.")
					return nil
				}
			}

			return dependencies.Exec.Exec(
				command.Context(),
				command.InOrStdin(),
				command.OutOrStdout(),
				command.ErrOrStderr(),
				kubectl.ExecOptions{
					Namespace: namespace,
					Pod:       pod,
					Container: container,
					Command:   shell,
				},
			)
		},
	}
	command.Flags().StringVarP(&container, "container", "c", "", "container name")
	command.Flags().StringVar(&shell, "shell", "/bin/sh", "shell executable")
	return command
}

func writeExecContext(
	output io.Writer,
	profileName string,
	profile config.Profile,
	namespace, pod, container, shell string,
) {
	fmt.Fprintln(output, "Exec target:")
	fmt.Fprintf(output, "  Profile: %s\n", profileName)
	fmt.Fprintf(output, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(output, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(output, "  Namespace: %s\n", namespace)
	fmt.Fprintf(output, "  Pod: %s\n", pod)
	fmt.Fprintf(output, "  Container: %s\n", container)
	fmt.Fprintf(output, "  Shell: %s\n", shell)
}

func newPodsPortForwardCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	var remotePort int32
	var localPort int32

	command := &cobra.Command{
		Use:   "port-forward [pod]",
		Short: "Forward a local port to a pod",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Pods == nil {
				return errors.New("Kubernetes pod service is not configured")
			}
			if dependencies.PortForward == nil {
				return errors.New("kubectl port-forward service is not configured")
			}
			if err := validatePort("--port", remotePort, true); err != nil {
				return err
			}
			if err := validatePort("--local-port", localPort, true); err != nil {
				return err
			}

			namespace := selectedNamespace(profile)
			pod, err := optionalPod(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "Pod selection cancelled.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("select pod for profile %q: %w", profileName, err)
			}
			if remotePort == 0 {
				remotePort, err = choosePodPort(command, dependencies, namespace, pod)
				if errors.Is(err, selector.ErrCancelled) {
					fmt.Fprintln(command.OutOrStdout(), "Port selection cancelled.")
					return nil
				}
				if err != nil {
					return fmt.Errorf("select port for profile %q: %w", profileName, err)
				}
			}
			if localPort == 0 {
				localPort = remotePort
			}

			fmt.Fprintf(
				command.OutOrStdout(),
				"Forwarding localhost:%d to %s/%s:%d. Press Ctrl+C to stop.\n",
				localPort,
				namespace,
				pod,
				remotePort,
			)
			return dependencies.PortForward.PortForward(
				command.Context(),
				command.InOrStdin(),
				command.OutOrStdout(),
				command.ErrOrStderr(),
				kubectl.PortForwardOptions{
					Namespace:  namespace,
					Pod:        pod,
					LocalPort:  localPort,
					RemotePort: remotePort,
				},
			)
		},
	}
	command.Flags().Int32Var(&remotePort, "port", 0, "remote pod port; prompts when omitted")
	command.Flags().Int32Var(&localPort, "local-port", 0, "local port; defaults to the remote port")
	return command
}

func choosePodPort(
	command *cobra.Command,
	dependencies Dependencies,
	namespace, pod string,
) (int32, error) {
	ports, err := dependencies.Pods.Ports(command.Context(), namespace, pod)
	if err != nil {
		return 0, err
	}
	ports = tcpPorts(ports)
	if len(ports) == 0 {
		return 0, errors.New("pod has no declared TCP container ports; provide --port")
	}
	if len(ports) == 1 {
		return ports[0].Port, nil
	}
	if dependencies.Selector == nil {
		return 0, errors.New("interactive selector is not configured; provide --port")
	}

	options := make([]string, 0, len(ports))
	portNumbers := make(map[string]int32, len(ports))
	for _, port := range ports {
		option := podPortLabel(port)
		options = append(options, option)
		portNumbers[option] = port.Port
	}
	selected, err := dependencies.Selector.Select(
		command.Context(),
		command.InOrStdin(),
		command.OutOrStdout(),
		fmt.Sprintf("Select port for %s", pod),
		options,
		options[0],
	)
	if err != nil {
		return 0, err
	}
	return portNumbers[selected], nil
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

func validatePort(flag string, port int32, allowZero bool) error {
	if allowZero && port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", flag)
	}
	return nil
}

func newPodsLogsCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	var container string
	var tailLines int64
	var follow bool
	var previous bool
	var timestamps bool

	command := &cobra.Command{
		Use:   "logs [pod]",
		Short: "Print or follow pod logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Pods == nil {
				return errors.New("Kubernetes pod service is not configured")
			}
			if tailLines < 0 {
				return errors.New("--tail must be zero or greater")
			}

			namespace := selectedNamespace(profile)
			pod, err := optionalPod(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "Pod selection cancelled.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("select pod for profile %q: %w", profileName, err)
			}
			if container == "" {
				container, err = choosePodContainer(command, dependencies, namespace, pod)
				if errors.Is(err, selector.ErrCancelled) {
					fmt.Fprintln(command.OutOrStdout(), "Container selection cancelled.")
					return nil
				}
				if err != nil {
					return fmt.Errorf("select container for profile %q: %w", profileName, err)
				}
			}

			stream, err := dependencies.Pods.Logs(command.Context(), kube.PodLogsOptions{
				Namespace:  namespace,
				Pod:        pod,
				Container:  container,
				TailLines:  tailLines,
				Follow:     follow,
				Previous:   previous,
				Timestamps: timestamps,
			})
			if err != nil {
				return fmt.Errorf("read logs for profile %q: %w", profileName, err)
			}
			defer stream.Close()
			if _, err := io.Copy(command.OutOrStdout(), stream); err != nil {
				return fmt.Errorf("write pod logs: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&container, "container", "c", "", "container name")
	command.Flags().Int64Var(&tailLines, "tail", 200, "number of recent lines")
	command.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log stream")
	command.Flags().BoolVar(&previous, "previous", false, "show logs from the previous container instance")
	command.Flags().BoolVar(&timestamps, "timestamps", false, "include timestamps")
	return command
}

func optionalPod(
	command *cobra.Command,
	dependencies Dependencies,
	namespace string,
	args []string,
) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return choosePod(command, dependencies, namespace)
}

func choosePod(command *cobra.Command, dependencies Dependencies, namespace string) (string, error) {
	if dependencies.Selector == nil {
		return "", errors.New("interactive selector is not configured; provide a pod name")
	}
	pods, err := dependencies.Pods.List(command.Context(), namespace)
	if err != nil {
		return "", err
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("no pods found in namespace %q", namespace)
	}

	options := make([]string, 0, len(pods))
	podNames := make(map[string]string, len(pods))
	for _, pod := range pods {
		option := fmt.Sprintf("%s | %s | ready %s | restarts %d", pod.Name, pod.Status, pod.Ready, pod.Restarts)
		options = append(options, option)
		podNames[option] = pod.Name
	}
	selected, err := dependencies.Selector.Select(
		command.Context(),
		command.InOrStdin(),
		command.OutOrStdout(),
		fmt.Sprintf("Select pod in %s", namespace),
		options,
		options[0],
	)
	if err != nil {
		return "", err
	}
	return podNames[selected], nil
}

func choosePodContainer(
	command *cobra.Command,
	dependencies Dependencies,
	namespace, pod string,
) (string, error) {
	containers, err := dependencies.Pods.Containers(command.Context(), namespace, pod)
	if err != nil {
		return "", err
	}
	switch len(containers) {
	case 0:
		return "", errors.New("pod has no containers")
	case 1:
		return containers[0], nil
	default:
		if dependencies.Selector == nil {
			return "", errors.New("interactive selector is not configured; provide --container")
		}
		return dependencies.Selector.Select(
			command.Context(),
			command.InOrStdin(),
			command.OutOrStdout(),
			fmt.Sprintf("Select container for %s", pod),
			containers,
			containers[0],
		)
	}
}

func newPodsListCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pods in the selected namespace",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Pods == nil {
				return errors.New("Kubernetes pod service is not configured")
			}

			namespace := selectedNamespace(profile)
			pods, err := dependencies.Pods.List(command.Context(), namespace)
			if err != nil {
				return fmt.Errorf("list pods for profile %q: %w", profileName, err)
			}
			if len(pods) == 0 {
				fmt.Fprintf(command.OutOrStdout(), "No pods found in namespace %q.\n", namespace)
				return nil
			}

			writePodList(command.OutOrStdout(), pods, time.Now())
			return nil
		},
	}
}

func newPodsDescribeCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "describe [pod]",
		Short: "Show safe pod troubleshooting details",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Pods == nil {
				return errors.New("Kubernetes pod service is not configured")
			}

			namespace := selectedNamespace(profile)
			pod, err := optionalPod(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "Pod selection cancelled.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("select pod for profile %q: %w", profileName, err)
			}
			details, err := dependencies.Pods.Describe(command.Context(), namespace, pod)
			if err != nil {
				return fmt.Errorf("describe pod for profile %q: %w", profileName, err)
			}
			writePodDetails(command.OutOrStdout(), details, time.Now())
			return nil
		},
	}
}

func selectedNamespace(profile config.Profile) string {
	if profile.CurrentNamespace != "" {
		return profile.CurrentNamespace
	}
	return profile.DefaultNamespace
}

func writePodList(output io.Writer, pods []kube.PodSummary, now time.Time) {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tREADY\tSTATUS\tRESTARTS\tAGE\tNODE")
	for _, pod := range pods {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%d\t%s\t%s\n",
			pod.Name,
			pod.Ready,
			pod.Status,
			pod.Restarts,
			formatAge(now, pod.CreatedAt),
			valueOrDash(pod.Node),
		)
	}
	writer.Flush()
}

func writePodDetails(output io.Writer, details kube.PodDetails, now time.Time) {
	fmt.Fprintf(output, "Name: %s\n", details.Name)
	fmt.Fprintf(output, "Namespace: %s\n", details.Namespace)
	fmt.Fprintf(output, "Status: %s\n", details.Status)
	fmt.Fprintf(output, "Ready: %s\n", details.Ready)
	fmt.Fprintf(output, "Restarts: %d\n", details.Restarts)
	fmt.Fprintf(output, "Age: %s\n", formatAge(now, details.CreatedAt))
	fmt.Fprintf(output, "Node: %s\n", valueOrDash(details.Node))
	fmt.Fprintf(output, "Pod IP: %s\n", valueOrDash(details.PodIP))
	fmt.Fprintf(output, "Host IP: %s\n", valueOrDash(details.HostIP))
	fmt.Fprintf(output, "Service Account: %s\n", valueOrDash(details.ServiceAccount))
	fmt.Fprintf(output, "QoS Class: %s\n", valueOrDash(details.QoSClass))
	writeSection(output, "Owners", details.Owners)
	writeConditions(output, details.Conditions)

	fmt.Fprintln(output, "Containers:")
	for _, container := range details.Containers {
		fmt.Fprintf(
			output,
			"  - %s | image=%s | ready=%t | restarts=%d\n",
			container.Name,
			container.Image,
			container.Ready,
			container.Restarts,
		)
		fmt.Fprintf(output, "    State: %s\n", container.State)
		fmt.Fprintf(output, "    Last state: %s\n", container.LastState)
		writeIndentedValues(output, "Ports", container.Ports)
		writeIndentedValues(output, "Requests", container.Requests)
		writeIndentedValues(output, "Limits", container.Limits)
		writeIndentedValues(output, "Mounts", container.Mounts)
		writeIndentedValues(output, "Probes", container.Probes)
		if len(container.EnvironmentNames) > 0 {
			fmt.Fprintf(output, "    Environment names: %s\n", strings.Join(container.EnvironmentNames, ", "))
		}
	}
	writeSection(output, "Volumes", details.Volumes)
	writeSection(output, "Labels", details.Labels)
	writeSection(output, "Annotation names", details.Annotations)
	writeEvents(output, details.Events, details.EventsWarning, now)
}

func writeSection(output io.Writer, title string, values []string) {
	fmt.Fprintf(output, "%s:\n", title)
	if len(values) == 0 {
		fmt.Fprintln(output, "  -")
		return
	}
	for _, value := range values {
		fmt.Fprintf(output, "  - %s\n", value)
	}
}

func writeConditions(output io.Writer, conditions []kube.ConditionSummary) {
	fmt.Fprintln(output, "Conditions:")
	if len(conditions) == 0 {
		fmt.Fprintln(output, "  -")
		return
	}
	for _, condition := range conditions {
		fmt.Fprintf(
			output,
			"  - %s=%s | reason=%s | message=%s\n",
			condition.Type,
			condition.Status,
			valueOrDash(condition.Reason),
			valueOrDash(condition.Message),
		)
	}
}

func writeIndentedValues(output io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(output, "    %s: %s\n", title, strings.Join(values, ", "))
}

func writeEvents(output io.Writer, events []kube.EventSummary, warning string, now time.Time) {
	fmt.Fprintln(output, "Recent Events:")
	if warning != "" {
		fmt.Fprintf(output, "  Warning: %s\n", warning)
	}
	if len(events) == 0 {
		fmt.Fprintln(output, "  -")
		return
	}
	for _, event := range events {
		fmt.Fprintf(
			output,
			"  - %s %s | count=%d | last=%s | %s\n",
			valueOrDash(event.Type),
			valueOrDash(event.Reason),
			event.Count,
			formatAge(now, event.LastSeen),
			valueOrDash(event.Message),
		)
	}
}

func formatAge(now, createdAt time.Time) string {
	if createdAt.IsZero() {
		return "-"
	}
	age := now.Sub(createdAt)
	if age < 0 {
		age = 0
	}
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
