package cmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newSecretsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "secrets",
		Short: "密钥管理",
	}
	c.AddCommand(newSecretsListCmd(), newSecretsSetCmd(), newSecretsRmCmd())
	return c
}

func newSecretsListCmd() *cobra.Command {
	var (
		typ     string
		q       string
		jsonOut bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出 org 范围密钥 (不含 value)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			qs := url.Values{}
			if typ != "" {
				qs.Set("type", typ)
			}
			if q != "" {
				qs.Set("q", q)
			}
			path := "/api/v1/secrets"
			if s := qs.Encode(); s != "" {
				path += "?" + s
			}
			var resp struct {
				Items []secretItem `json:"items"`
				Total int64        `json:"total"`
			}
			if err := cli.Do("GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), resp)
			}
			return writeSecretsTable(cmd.OutOrStdout(), resp.Items)
		},
	}
	c.Flags().StringVar(&typ, "type", "", "按类型过滤")
	c.Flags().StringVarP(&q, "query", "q", "", "名称/描述模糊搜索")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newSecretsSetCmd() *cobra.Command {
	var (
		scope   string
		scopeID int64
		typ     string
		desc    string
		value   string
		fromFile string
	)
	c := &cobra.Command{
		Use:   "set <name>",
		Short: "创建或更新密钥 (POST /secrets)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			if scopeID <= 0 {
				return fmt.Errorf("--scope-id required")
			}
			val := value
			if fromFile != "" {
				b, err := os.ReadFile(fromFile)
				if err != nil {
					return fmt.Errorf("read value file: %w", err)
				}
				val = string(b)
			}
			if val == "" {
				return fmt.Errorf("value required (--value or --from-file)")
			}
			body := map[string]any{
				"scope":    scope,
				"scope_id": scopeID,
				"name":     args[0],
				"type":     typ,
				"value":    val,
			}
			if desc != "" {
				body["description"] = desc
			}
			var s secretItem
			if err := cli.Do("POST", "/api/v1/secrets", body, &s); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已创建密钥 %q (id=%d, type=%s)\n", s.Name, s.ID, s.Type)
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "org", "scope (org/project/pipeline)")
	c.Flags().Int64Var(&scopeID, "scope-id", 0, "scope_id (org 时填 org_id)")
	c.Flags().StringVar(&typ, "type", "text", "密钥类型")
	c.Flags().StringVarP(&desc, "description", "d", "", "描述")
	c.Flags().StringVar(&value, "value", "", "密钥值 (明文, 优先于 --from-file)")
	c.Flags().StringVar(&fromFile, "from-file", "", "从文件读取密钥值")
	return c
}

func newSecretsRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <secret-id>",
		Short: "删除密钥",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			if err := cli.Do("DELETE", "/api/v1/secrets/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已删除密钥 %s\n", args[0])
			return nil
		},
	}
}

type secretItem struct {
	ID          int64  `json:"id"`
	Scope       string `json:"scope"`
	ScopeID     int64  `json:"scope_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

func writeSecretsTable(w io.Writer, items []secretItem) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSCOPE\tNAME\tTYPE")
	for _, s := range items {
		fmt.Fprintf(tw, "%d\t%s:%d\t%s\t%s\n", s.ID, s.Scope, s.ScopeID, s.Name, s.Type)
	}
	return tw.Flush()
}
