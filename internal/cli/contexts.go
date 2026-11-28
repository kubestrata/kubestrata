package cli

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/kubestrata/kubestrata/internal/kube"
)

// hygiene names what a context does that exposes its credential.
func hygiene(c kube.Context) string {
	var notes []string
	if c.InsecureSkipTLSVerify {
		notes = append(notes, "TLS not verified")
	}
	if c.StaticToken {
		notes = append(notes, "static token")
	}
	return strings.Join(notes, ", ")
}

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
			current = g.mask(current)
			for i := range ctxs {
				c := &ctxs[i]
				c.Name, c.Cluster, c.User, c.Server = g.mask(c.Name), g.mask(c.Cluster), g.mask(c.User), g.mask(c.Server)
			}
			out := cmd.OutOrStdout()
			if g.format == "json" {
				return writeJSON(out, contextsDoc{SchemaVersion: SchemaVersion, Current: current, Contexts: ctxs})
			}
			if len(ctxs) == 0 {
				_, err := fmt.Fprintln(out, "no contexts in kubeconfig")
				return err
			}
			t := newTable("", "CONTEXT", "SERVER", "USER", "AUTH", "NAMESPACE", "NOTES")
			flagged := false
			for _, c := range ctxs {
				mark := ""
				if c.Current {
					mark = "*"
				}
				auth := c.Auth
				if c.AuthExec != "" {
					auth = "exec: " + c.AuthExec
				}
				notes := hygiene(c)
				flagged = flagged || notes != ""
				t.Row(mark, c.Name, c.Server, c.User, auth, c.Namespace, notes)
			}
			lines := t.String()
			if flagged {
				lines += "\nnotes: trust the cluster CA instead of skipping TLS verification, and prefer an exec plugin or a " +
					"short-lived token to a static one (README, \"Kubeconfig hygiene\")"
			}
			_, err = lipgloss.Fprintln(out, lines)
			return err
		},
	}
}
