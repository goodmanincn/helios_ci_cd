package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newProjectsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "projects",
		Short: "项目管理",
	}
	c.AddCommand(newProjectsListCmd(), newProjectsGetCmd())
	return c
}

func newProjectsListCmd() *cobra.Command {
	var (
		q      string
		jsonOut bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出当前 org 的项目",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			path := "/api/v1/projects"
			if q != "" {
				path += "?q=" + q
			}
			var list []project
			if err := cli.Do("GET", path, nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			return writeProjectTable(cmd.OutOrStdout(), list)
		},
	}
	c.Flags().StringVarP(&q, "query", "q", "", "按名称/slug 过滤")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newProjectsGetCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "get <project-id>",
		Short: "查看项目详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid project id: %w", err)
			}
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			var p project
			if err := cli.Do("GET", "/api/v1/projects/"+strconv.FormatInt(id, 10), nil, &p); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), p)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"id        : %d\nname      : %s\nslug      : %s\nrepo_url  : %s\nrepo_type : %s\ndefault   : %s\nvis       : %s\n",
				p.ID, p.Name, p.Slug, p.RepoURL, p.RepoType, p.DefaultBranch, p.Visibility)
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

type project struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	RepoURL       string `json:"repo_url"`
	RepoType      string `json:"repo_type"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
}

// ===== 渲染 helpers =====

func writeProjectTable(w io.Writer, list []project) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSLUG\tNAME\tREPO\tBRANCH")
	for _, p := range list {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
			p.ID, p.Slug, truncate(p.Name, 30), truncate(p.RepoURL, 50), p.DefaultBranch)
	}
	return tw.Flush()
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
