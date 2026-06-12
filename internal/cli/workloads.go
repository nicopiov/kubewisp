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
	"github.com/nicopiov/kubewisp/internal/selector"
	"github.com/spf13/cobra"
)

func newWorkloadsCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "workloads",
		Short: "Inspect and operate Kubernetes workloads",
	}
	command.AddCommand(
		newWorkloadsListCommand(dependencies, configPath),
		newWorkloadsRestartCommand(dependencies, configPath),
		newCronJobCommand(dependencies, configPath),
	)
	return command
}

func newCronJobCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "cronjob",
		Short: "Inspect and manage CronJobs",
	}
	command.AddCommand(
		newCronJobDescribeCommand(dependencies, configPath),
		newCronJobStateCommand(dependencies, configPath, true),
		newCronJobStateCommand(dependencies, configPath, false),
	)
	return command
}

func newCronJobDescribeCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "describe [name]",
		Short: "Show CronJob details and recent Jobs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Workloads == nil {
				return errors.New("Kubernetes workload service is not configured")
			}
			namespace := selectedNamespace(profile)
			cronJob, err := chooseCronJob(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "CronJob selection cancelled.")
				return nil
			}
			if err != nil {
				return err
			}
			details, err := dependencies.Workloads.DescribeCronJob(command.Context(), namespace, cronJob.Name)
			if err != nil {
				return fmt.Errorf("describe cronjob for profile %q: %w", profileName, err)
			}
			writeCronJobDetails(command.OutOrStdout(), details, time.Now())
			return nil
		},
	}
}

func newCronJobStateCommand(dependencies Dependencies, configPath *string, suspended bool) *cobra.Command {
	action := "resume"
	short := "Resume a suspended CronJob"
	if suspended {
		action = "suspend"
		short = "Suspend future CronJob schedules"
	}
	return &cobra.Command{
		Use:   action + " [name]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Workloads == nil {
				return errors.New("Kubernetes workload service is not configured")
			}
			namespace := selectedNamespace(profile)
			cronJob, err := chooseCronJob(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "CronJob selection cancelled.")
				return nil
			}
			if err != nil {
				return err
			}
			if cronJob.Suspended == suspended {
				fmt.Fprintf(command.OutOrStdout(), "CronJob/%s is already %s.\n", cronJob.Name, cronJobStateWord(suspended))
				return nil
			}
			writeCronJobStateContext(command.OutOrStdout(), profileName, profile, namespace, cronJob, suspended)
			confirmed, err := confirmCronJobState(command, profile.Production, cronJob, suspended)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintf(command.OutOrStdout(), "CronJob %s cancelled.\n", action)
				return nil
			}
			if err := dependencies.Workloads.SetCronJobSuspended(command.Context(), namespace, cronJob.Name, suspended); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "CronJob/%s is now %s.\n", cronJob.Name, cronJobStateWord(suspended))
			return nil
		},
	}
}

func newWorkloadsListCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List deployments, statefulsets, daemonsets, and cronjobs",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Workloads == nil {
				return errors.New("Kubernetes workload service is not configured")
			}
			workloads, err := dependencies.Workloads.List(command.Context(), selectedNamespace(profile))
			if err != nil {
				return fmt.Errorf("list workloads for profile %q: %w", profileName, err)
			}
			writeWorkloadList(command.OutOrStdout(), workloads)
			return nil
		},
	}
}

func newWorkloadsRestartCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "restart [kind/name]",
		Short: "Trigger a guarded rollout restart",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Workloads == nil {
				return errors.New("Kubernetes workload service is not configured")
			}
			namespace := selectedNamespace(profile)
			workload, err := chooseWorkload(command, dependencies, namespace, args)
			if errors.Is(err, selector.ErrCancelled) {
				fmt.Fprintln(command.OutOrStdout(), "Workload selection cancelled.")
				return nil
			}
			if err != nil {
				return err
			}
			writeWorkloadRestartContext(command.OutOrStdout(), profileName, profile, namespace, workload)
			confirmed, err := confirmWorkloadRestart(command, profile.Production, workload)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(command.OutOrStdout(), "Rollout restart cancelled.")
				return nil
			}
			if err := dependencies.Workloads.RolloutRestart(command.Context(), namespace, workload.Kind, workload.Name); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Rollout restart requested for %s/%s.\n", workload.Kind, workload.Name)
			return nil
		},
	}
}

func chooseWorkload(
	command *cobra.Command,
	dependencies Dependencies,
	namespace string,
	args []string,
) (kube.WorkloadSummary, error) {
	workloads, err := dependencies.Workloads.List(command.Context(), namespace)
	if err != nil {
		return kube.WorkloadSummary{}, err
	}
	if len(args) == 1 {
		kind, name, ok := strings.Cut(args[0], "/")
		if !ok || kind == "" || name == "" {
			return kube.WorkloadSummary{}, errors.New("workload must be formatted as kind/name")
		}
		for _, workload := range workloads {
			if strings.EqualFold(workload.Kind, kind) && workload.Name == name {
				if !kube.SupportsRolloutRestart(workload.Kind) {
					return kube.WorkloadSummary{}, fmt.Errorf("%s does not support rollout restart", workload.Kind)
				}
				return workload, nil
			}
		}
		return kube.WorkloadSummary{}, fmt.Errorf("workload %q was not found in namespace %q", args[0], namespace)
	}
	if len(workloads) == 0 {
		return kube.WorkloadSummary{}, fmt.Errorf("no workloads found in namespace %q", namespace)
	}
	if dependencies.Selector == nil {
		return kube.WorkloadSummary{}, errors.New("interactive selector is not configured; provide kind/name")
	}
	options := make([]string, 0, len(workloads))
	byOption := make(map[string]kube.WorkloadSummary, len(workloads))
	for _, workload := range workloads {
		if !kube.SupportsRolloutRestart(workload.Kind) {
			continue
		}
		option := fmt.Sprintf("%s/%s | ready %d/%d | updated %d", workload.Kind, workload.Name, workload.Ready, workload.Desired, workload.Updated)
		options = append(options, option)
		byOption[option] = workload
	}
	if len(options) == 0 {
		return kube.WorkloadSummary{}, fmt.Errorf("no workloads that support rollout restart found in namespace %q", namespace)
	}
	selected, err := dependencies.Selector.Select(
		command.Context(),
		command.InOrStdin(),
		command.OutOrStdout(),
		fmt.Sprintf("Select workload in %s", namespace),
		options,
		options[0],
	)
	if err != nil {
		return kube.WorkloadSummary{}, err
	}
	return byOption[selected], nil
}

func chooseCronJob(
	command *cobra.Command,
	dependencies Dependencies,
	namespace string,
	args []string,
) (kube.WorkloadSummary, error) {
	workloads, err := dependencies.Workloads.List(command.Context(), namespace)
	if err != nil {
		return kube.WorkloadSummary{}, err
	}
	var cronJobs []kube.WorkloadSummary
	for _, workload := range workloads {
		if strings.EqualFold(workload.Kind, "CronJob") {
			cronJobs = append(cronJobs, workload)
		}
	}
	if len(args) == 1 {
		name := strings.TrimPrefix(args[0], "CronJob/")
		for _, cronJob := range cronJobs {
			if cronJob.Name == name {
				return cronJob, nil
			}
		}
		return kube.WorkloadSummary{}, fmt.Errorf("cronjob %q was not found in namespace %q", name, namespace)
	}
	if len(cronJobs) == 0 {
		return kube.WorkloadSummary{}, fmt.Errorf("no cronjobs found in namespace %q", namespace)
	}
	if dependencies.Selector == nil {
		return kube.WorkloadSummary{}, errors.New("interactive selector is not configured; provide a cronjob name")
	}
	options := make([]string, 0, len(cronJobs))
	byOption := make(map[string]kube.WorkloadSummary, len(cronJobs))
	for _, cronJob := range cronJobs {
		option := fmt.Sprintf("%s | %s | active %d", cronJob.Name, cronJob.Schedule, cronJob.Active)
		options = append(options, option)
		byOption[option] = cronJob
	}
	selected, err := dependencies.Selector.Select(
		command.Context(),
		command.InOrStdin(),
		command.OutOrStdout(),
		fmt.Sprintf("Select CronJob in %s", namespace),
		options,
		options[0],
	)
	if err != nil {
		return kube.WorkloadSummary{}, err
	}
	return byOption[selected], nil
}

func confirmCronJobState(command *cobra.Command, production bool, cronJob kube.WorkloadSummary, suspended bool) (bool, error) {
	reader := bufio.NewReader(command.InOrStdin())
	reference := "CronJob/" + cronJob.Name
	action := cronJobStateAction(suspended)
	if production {
		value, err := promptText(reader, command.OutOrStdout(), fmt.Sprintf("PRODUCTION: type %q to %s", reference, action), "")
		if err != nil {
			return false, err
		}
		return value == reference, nil
	}
	return promptYesNo(reader, command.OutOrStdout(), fmt.Sprintf("%s %s?", strings.Title(action), reference), false)
}

func writeCronJobStateContext(
	output io.Writer,
	profileName string,
	profile config.Profile,
	namespace string,
	cronJob kube.WorkloadSummary,
	suspended bool,
) {
	fmt.Fprintf(output, "CronJob %s target:\n", cronJobStateAction(suspended))
	fmt.Fprintf(output, "  Profile: %s\n", profileName)
	fmt.Fprintf(output, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(output, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(output, "  Namespace: %s\n", namespace)
	fmt.Fprintf(output, "  CronJob: %s\n", cronJob.Name)
	fmt.Fprintf(output, "  Schedule: %s\n", cronJob.Schedule)
	fmt.Fprintf(output, "  Current state: %s\n", cronJobStateWord(cronJob.Suspended))
	fmt.Fprintf(output, "  New state: %s\n", cronJobStateWord(suspended))
}

func writeCronJobDetails(output io.Writer, details kube.CronJobDetails, now time.Time) {
	fmt.Fprintf(output, "CronJob: %s\n", details.Name)
	fmt.Fprintf(output, "Schedule: %s\n", details.Schedule)
	fmt.Fprintf(output, "Suspended: %t\n", details.Suspended)
	fmt.Fprintf(output, "Active Jobs: %d\n", details.Active)
	fmt.Fprintf(output, "Concurrency Policy: %s\n", valueOrDash(details.ConcurrencyPolicy))
	fmt.Fprintf(output, "Last Scheduled: %s\n", formatAge(now, details.LastScheduleTime))
	fmt.Fprintf(output, "Last Successful: %s\n", formatAge(now, details.LastSuccessfulTime))
	fmt.Fprintln(output, "\nRecent Jobs:")
	if len(details.Jobs) == 0 {
		fmt.Fprintln(output, "  -")
		return
	}
	for _, job := range details.Jobs {
		fmt.Fprintf(
			output,
			"  %s | %s | active=%d succeeded=%d failed=%d | age=%s\n",
			job.Name,
			job.Status,
			job.Active,
			job.Succeeded,
			job.Failed,
			formatAge(now, job.CreatedAt),
		)
	}
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

func confirmWorkloadRestart(command *cobra.Command, production bool, workload kube.WorkloadSummary) (bool, error) {
	reader := bufio.NewReader(command.InOrStdin())
	reference := workload.Kind + "/" + workload.Name
	if production {
		value, err := promptText(reader, command.OutOrStdout(), fmt.Sprintf("PRODUCTION: type workload %q to restart", reference), "")
		if err != nil {
			return false, err
		}
		return value == reference, nil
	}
	return promptYesNo(reader, command.OutOrStdout(), fmt.Sprintf("Rollout restart %s?", reference), false)
}

func writeWorkloadRestartContext(
	output io.Writer,
	profileName string,
	profile config.Profile,
	namespace string,
	workload kube.WorkloadSummary,
) {
	fmt.Fprintln(output, "Rollout restart target:")
	fmt.Fprintf(output, "  Profile: %s\n", profileName)
	fmt.Fprintf(output, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(output, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(output, "  Namespace: %s\n", namespace)
	fmt.Fprintf(output, "  Workload: %s/%s\n", workload.Kind, workload.Name)
	fmt.Fprintf(output, "  Ready: %d/%d\n", workload.Ready, workload.Desired)
}

func writeWorkloadList(output io.Writer, workloads []kube.WorkloadSummary) {
	if len(workloads) == 0 {
		fmt.Fprintln(output, "No workloads found.")
		return
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "KIND\tNAME\tSTATUS\tSCHEDULE\tLAST RUN")
	for _, workload := range workloads {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\n",
			workload.Kind,
			workload.Name,
			workloadStatusText(workload),
			valueOrDash(workload.Schedule),
			formatAge(time.Now(), workload.LastScheduleTime),
		)
	}
	writer.Flush()
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
