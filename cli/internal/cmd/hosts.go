package cmd

import (
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newHostsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hosts",
		Short: "物理机 / SSH 主机管理",
	}
	c.AddCommand(newHostsListCmd(), newHostsTestCmd())
	return c
}

func newHostsListCmd() *cobra.Command {
	var (
		q       string
		jsonOut bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出 SSH 主机",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			path := "/api/v1/hosts"
			if q != "" {
				path += "?q=" + url.QueryEscape(q)
			}
			var list []hostItem
			if err := cli.Do("GET", path, nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			return writeHostsTable(cmd.OutOrStdout(), list)
		},
	}
	c.Flags().StringVarP(&q, "query", "q", "", "名称/IP 模糊搜索")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newHostsTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <host-id>",
		Short: "测试主机 SSH 连通性",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			var res struct {
				Reachable bool   `json:"reachable"`
				SSHOK     bool   `json:"ssh_ok"`
				Uname     string `json:"uname"`
				Message   string `json:"message"`
			}
			if err := cli.Do("POST", "/api/v1/hosts/"+args[0]+"/test", nil, &res); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"reachable=%v ssh_ok=%v uname=%s %s\n",
				res.Reachable, res.SSHOK, res.Uname, res.Message)
			return nil
		},
	}
}

type hostItem struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	SSHUser string `json:"ssh_user"`
	Status  string `json:"status"`
}

func writeHostsTable(w io.Writer, list []hostItem) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tIP\tUSER\tSTATUS")
	for _, h := range list {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", h.ID, h.Name, h.IP, h.SSHUser, h.Status)
	}
	return tw.Flush()
}
