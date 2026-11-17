package cli

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"

	"github.com/leosavio/kubestrata/internal/kube"
)

type contextsDoc struct {
	SchemaVersion int            `json:"schema_version"`
	Current       string         `json:"current"`
	Contexts      []kube.Context `json:"contexts"`
}

func newContextsCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "contexts",
		Short: "List kubeconfig contexts (no cluster access)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			ctxs, current, err := kube.LoadContexts(g.kubeconfig)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if g.format == "json" {
				return writeJSON(out, contextsDoc{SchemaVersion: SchemaVersion, Current: current, Contexts: ctxs})
			}
			if len(ctxs) == 0 {
				_, err := fmt.Fprintln(out, "no contexts in kubeconfig")
				return err
			}
			head := lipgloss.NewStyle().Bold(true).Padding(0, 1)
			cell := lipgloss.NewStyle().Padding(0, 1)
			t := table.New().
				Border(lipgloss.NormalBorder()).
				Headers("", "CONTEXT", "SERVER", "USER", "AUTH", "NAMESPACE").
				StyleFunc(func(row, _ int) lipgloss.Style {
					if row == table.HeaderRow {
						return head
					}
					return cell
				})
			for _, c := range ctxs {
				mark := ""
				if c.Current {
					mark = "*"
				}
				t.Row(mark, c.Name, c.Server, c.User, c.AuthExec, c.Namespace)
			}
			_, err = lipgloss.Fprintln(out, t.String())
			return err
		},
	}
}
