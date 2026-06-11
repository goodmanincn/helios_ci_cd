package cmd

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newRunsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "runs",
		Short: "执行记录管理",
	}
	c.AddCommand(newRunsListCmd(), newRunsGetCmd(), newRunsCancelCmd(), newRunsLogsCmd())
	return c
}

func newRunsListCmd() *cobra.Command {
	var (
		project   int64
		status    string
		limit     int
		jsonOut   bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出 run (按 project/status 过滤)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			if project > 0 {
				q.Set("project_id", strconv.FormatInt(project, 10))
			}
			if status != "" {
				q.Set("status", status)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			path := "/api/v1/runs"
			if s := q.Encode(); s != "" {
				path += "?" + s
			}
			var list []runSummary
			if err := cli.Do("GET", path, nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			return writeRunsTable(cmd.OutOrStdout(), list)
		},
	}
	c.Flags().Int64VarP(&project, "project", "p", 0, "按 project_id 过滤")
	c.Flags().StringVar(&status, "status", "", "按状态过滤 (pending/running/success/failed/canceled)")
	c.Flags().IntVarP(&limit, "limit", "n", 20, "返回条数")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newRunsGetCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "get <run-id>",
		Short: "查看 run 详情 (含 stages/steps)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			var raw []byte // 详情 schema 大, 直接展示原始 JSON 更省事
			if err := cli.Do("GET", "/api/v1/runs/"+args[0], nil, &raw); err != nil {
				return err
			}
			_ = jsonOut // 当前总是 JSON, 留 flag 给未来表格视图
			_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
			return err
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", true, "输出原始 JSON")
	return c
}

func newRunsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "取消 run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			if err := cli.Do("POST", "/api/v1/runs/"+args[0]+"/cancel", nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已请求取消 run %s\n", args[0])
			return nil
		},
	}
}

func newRunsLogsCmd() *cobra.Command {
	var (
		count int
	)
	c := &cobra.Command{
		Use:   "logs <run-id>",
		Short: "拉取 run 的历史日志 (NDJSON, 一行一条)",
		Long: `从 /api/v1/runs/:id/logs?source=auto 拉取已落档/最新日志.
M1 阶段不接 SSE 实时流, 跑完后再 dump 比 CLI 流式更可靠.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			q.Set("source", "auto")
			if count > 0 {
				q.Set("count", strconv.Itoa(count))
			}
			var raw []byte
			if err := cli.Do("GET",
				"/api/v1/runs/"+args[0]+"/logs?"+q.Encode(),
				nil, &raw); err != nil {
				return err
			}
			body := string(raw)
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			_, err = cmd.OutOrStdout().Write([]byte(body))
			return err
		},
	}
	c.Flags().IntVarP(&count, "count", "n", 0, "最多返回行数 (0=全部)")
	return c
}

func writeRunsTable(w io.Writer, list []runSummary) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPROJECT\tPIPELINE\tBRANCH\tCREATED")
	for _, r := range list {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t%s\t%s\n",
			r.ID, r.Status, r.ProjectID, r.PipelineID, r.Branch, r.CreatedAt)
	}
	return tw.Flush()
}

// ===== DTOs =====

type runSummary struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	ProjectID  int64  `json:"project_id"`
	PipelineID int64  `json:"pipeline_id"`
	Branch     string `json:"branch"`
	CreatedAt  string `json:"created_at"`
}
