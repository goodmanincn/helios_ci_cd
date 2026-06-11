package cmd

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// helios templates — 流水线模板市场 (M8 T8.2.1 配套 CLI)
func newTemplatesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "templates",
		Short: "流水线模板市场",
	}
	c.AddCommand(newTemplatesListCmd(), newTemplatesGetCmd(), newTemplatesCloneCmd())
	return c
}

func newTemplatesListCmd() *cobra.Command {
	var (
		category string
		tag      string
		q        string
		jsonOut  bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出可用模板 (全局 + 当前 org 私有)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			qs := url.Values{}
			if category != "" {
				qs.Set("category", category)
			}
			if tag != "" {
				qs.Set("tag", tag)
			}
			if q != "" {
				qs.Set("q", q)
			}
			path := "/api/v1/pipeline-templates"
			if s := qs.Encode(); s != "" {
				path += "?" + s
			}
			var list []template
			if err := cli.Do("GET", path, nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			return writeTemplatesTable(cmd.OutOrStdout(), list)
		},
	}
	c.Flags().StringVar(&category, "category", "", "按 category 过滤 (build/deploy/release/fullstack)")
	c.Flags().StringVar(&tag, "tag", "", "按 tag 精确过滤")
	c.Flags().StringVarP(&q, "query", "q", "", "在 slug/name/description 中模糊查找")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newTemplatesGetCmd() *cobra.Command {
	var (
		raw     bool
		jsonOut bool
	)
	c := &cobra.Command{
		Use:   "get <slug-or-id>",
		Short: "查看模板详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			id := args[0]
			// 数字按 id 取, 否则当 slug → 走 list+filter (服务端没暴露 GetBySlug 公开端点)
			if _, err := strconv.ParseInt(id, 10, 64); err != nil {
				path := "/api/v1/pipeline-templates?q=" + url.QueryEscape(id)
				var list []template
				if err := cli.Do("GET", path, nil, &list); err != nil {
					return err
				}
				found := false
				for _, t := range list {
					if t.Slug == id {
						id = strconv.FormatInt(t.ID, 10)
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("template %q not found", args[0])
				}
			}
			var t template
			if err := cli.Do("GET", "/api/v1/pipeline-templates/"+id, nil, &t); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), t)
			}
			if raw {
				fmt.Fprint(cmd.OutOrStdout(), t.SpecRaw)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"id          : %d\nslug        : %s\nname        : %s\ndescription : %s\ncategory    : %s\ntags        : %v\nbuiltin     : %v\norg_id      : %v\n\n--- spec_raw ---\n%s",
				t.ID, t.Slug, t.Name, t.Description, t.Category, t.Tags, t.Builtin, t.OrgID, t.SpecRaw)
			return nil
		},
	}
	c.Flags().BoolVar(&raw, "raw", false, "仅输出 spec_raw YAML")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newTemplatesCloneCmd() *cobra.Command {
	var (
		project int64
		name    string
		desc    string
	)
	c := &cobra.Command{
		Use:   "clone <slug-or-id>",
		Short: "从模板克隆出新 pipeline 到指定 project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if project <= 0 {
				return fmt.Errorf("--project required")
			}
			if name == "" {
				return fmt.Errorf("--name required")
			}
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			body := map[string]any{
				"project_id":  project,
				"name":        name,
				"description": desc,
			}
			// 数字 = id, 否则 slug
			if id, err := strconv.ParseInt(args[0], 10, 64); err == nil {
				body["template_id"] = id
			} else {
				body["template_slug"] = args[0]
			}
			var resp struct {
				PipelineID   int64  `json:"pipeline_id"`
				VersionID    int64  `json:"version_id"`
				Version      int    `json:"version"`
				PipelineName string `json:"pipeline_name"`
				TemplateSlug string `json:"template_slug"`
			}
			if err := cli.Do("POST", "/api/v1/pipelines/from-template", body, &resp); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"已从模板 %s 克隆 pipeline %s (id=%d, v=%d)\n",
				resp.TemplateSlug, resp.PipelineName, resp.PipelineID, resp.Version)
			return nil
		},
	}
	c.Flags().Int64VarP(&project, "project", "p", 0, "目标 project_id (必填)")
	c.Flags().StringVarP(&name, "name", "n", "", "新 pipeline 名 (必填)")
	c.Flags().StringVarP(&desc, "description", "d", "", "新 pipeline 描述")
	return c
}

// ===== DTOs =====

func writeTemplatesTable(w io.Writer, list []template) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSLUG\tNAME\tCATEGORY\tTAGS\tBUILTIN")
	for _, t := range list {
		bi := ""
		if t.Builtin {
			bi = "✓"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%v\t%s\n",
			t.ID, t.Slug, truncate(t.Name, 30), t.Category, t.Tags, bi)
	}
	return tw.Flush()
}

type template struct {
	ID          int64    `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Builtin     bool     `json:"builtin"`
	OrgID       *int64   `json:"org_id"`
	SpecRaw     string   `json:"spec_raw"`
}
