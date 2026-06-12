package cli

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/spf13/cobra"
)

func newEventsCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List grouped warning events in the selected namespace",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Events == nil {
				return errors.New("Kubernetes event service is not configured")
			}
			events, err := dependencies.Events.ListWarnings(command.Context(), selectedNamespace(profile))
			if err != nil {
				return fmt.Errorf("list warning events for profile %q: %w", profileName, err)
			}
			writeEventList(command.OutOrStdout(), events, time.Now())
			return nil
		},
	}
}

func writeEventList(output io.Writer, events []kube.NamespaceEventSummary, now time.Time) {
	if len(events) == 0 {
		fmt.Fprintln(output, "No warning events found.")
		return
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "LAST SEEN\tCOUNT\tOBJECT\tREASON\tMESSAGE")
	for _, event := range events {
		fmt.Fprintf(
			writer,
			"%s\t%d\t%s/%s\t%s\t%s\n",
			formatAge(now, event.LastSeen),
			event.Count,
			event.ObjectKind,
			event.ObjectName,
			event.Reason,
			event.Message,
		)
	}
	writer.Flush()
}
