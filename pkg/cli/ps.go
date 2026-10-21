package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/wzshiming/my-compose/pkg/plan"
)

func (a *app) psCommand(g *globals) *cobra.Command {
	var all, quiet bool
	var format string
	cmd := &cobra.Command{
		Use:   "ps [SERVICE...]",
		Short: "List containers",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "table" && format != "json" {
				return fmt.Errorf("unsupported format %q: use table or json", format)
			}
			ctx := cmd.Context()
			project, err := g.load(ctx)
			if err != nil {
				return err
			}
			for _, arg := range args {
				if _, ok := project.Services[arg]; !ok {
					return fmt.Errorf("no such service: %s", arg)
				}
			}
			r, err := a.runner(g, cmd)
			if err != nil {
				return err
			}
			if r.DryRun {
				p, w, err := plan.Ps(project)
				if err != nil {
					return err
				}
				return r.Run(ctx, p, w)
			}
			st, err := state(ctx, r, project, plan.Ps)
			if err != nil {
				return err
			}
			containers := selectContainers(st.Containers, args, all)
			switch {
			case quiet:
				for _, c := range containers {
					fmt.Fprintln(cmd.OutOrStdout(), c.Name)
				}
				return nil
			case format == "json":
				return writeJSON(cmd.OutOrStdout(), containers)
			}
			return writeTable(cmd.OutOrStdout(), containers)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all containers, including stopped ones")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table or json")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only display container names")
	return cmd
}

// selectContainers hides stopped containers unless all, like docker compose ps.
func selectContainers(containers []plan.ContainerState, services []string, all bool) []plan.ContainerState {
	var kept []plan.ContainerState
	for _, c := range containers {
		if len(services) > 0 && !slices.Contains(services, c.Service) {
			continue
		}
		if !all && (c.Status == plan.StatusExited || c.Status == plan.StatusCreated || c.Status == plan.StatusDead) {
			continue
		}
		kept = append(kept, c)
	}
	slices.SortFunc(kept, func(a, b plan.ContainerState) int {
		return cmp.Or(cmp.Compare(a.Service, b.Service), cmp.Compare(a.Number, b.Number), cmp.Compare(a.Name, b.Name))
	})
	return kept
}

func writeTable(w io.Writer, containers []plan.ContainerState) error {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprint(tw, "NAME\tIMAGE\tSERVICE\tSTATUS\tPORTS\n")
	for _, c := range containers {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.Name, c.Image, c.Service, status(c), ports(c.Ports))
	}
	return tw.Flush()
}

func status(c plan.ContainerState) string {
	switch {
	case c.Status == plan.StatusExited:
		return fmt.Sprintf("exited (%d)", c.ExitCode)
	case c.Health != "" && c.Health != plan.HealthNone:
		return c.Status + " (" + c.Health + ")"
	}
	return c.Status
}

func ports(ps []plan.Port) string {
	var ss []string
	for _, p := range ps {
		s := strconv.FormatUint(uint64(p.Target), 10)
		if p.Protocol != "" {
			s += "/" + p.Protocol
		}
		if p.Published != "" {
			s = p.Published + "->" + s
			if p.HostIP != "" {
				s = p.HostIP + ":" + s
			}
		}
		ss = append(ss, s)
	}
	return strings.Join(ss, ", ")
}

type psEntry struct {
	Name     string   `json:"name"`
	Service  string   `json:"service"`
	Number   int      `json:"number"`
	Image    string   `json:"image"`
	Status   string   `json:"status"`
	Health   string   `json:"health"`
	ExitCode int      `json:"exit_code"`
	Ports    []psPort `json:"ports"`
	Command  []string `json:"command"`
}

type psPort struct {
	HostIP    string `json:"host_ip"`
	Published string `json:"published"`
	Target    uint32 `json:"target"`
	Protocol  string `json:"protocol"`
}

func writeJSON(w io.Writer, containers []plan.ContainerState) error {
	entries := make([]psEntry, 0, len(containers))
	for _, c := range containers {
		ports := make([]psPort, 0, len(c.Ports))
		for _, p := range c.Ports {
			ports = append(ports, psPort{HostIP: p.HostIP, Published: p.Published, Target: p.Target, Protocol: p.Protocol})
		}
		entries = append(entries, psEntry{
			Name: c.Name, Service: c.Service, Number: c.Number, Image: c.Image,
			Status: c.Status, Health: c.Health, ExitCode: c.ExitCode,
			Ports: ports, Command: append([]string{}, c.Command...),
		})
	}
	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", out)
	return err
}
