package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newPipelinesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pipelines",
		Short: "流水线管理",
		Aliases: []string{"pipeline"},
	}
	c.AddCommand(
		newPipelinesListCmd(),
		newPipelinesGetCmd(),
		newPipelinesValidateCmd(),
		newPipelinesApplyCmd(),
	)
	return c
}

func newPipelinesListCmd() *cobra.Command {
	var (
		project int64
		jsonOut bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出流水线 (?project_id 过滤)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			path := "/api/v1/pipelines"
			if project > 0 {
				path += "?project_id=" + strconv.FormatInt(project, 10)
			}
			var list []pipelineSummary
			if err := cli.Do("GET", path, nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			return writePipelineTable(cmd.OutOrStdout(), list)
		},
	}
	c.Flags().Int64VarP(&project, "project", "p", 0, "按 project_id 过滤")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newPipelinesGetCmd() *cobra.Command {
	var (
		raw     bool
		jsonOut bool
	)
	c := &cobra.Command{
		Use:   "get <pipeline-id>",
		Short: "查看流水线详情 (含当前 spec_raw)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			var p pipelineDetail
			if err := cli.Do("GET", "/api/v1/pipelines/"+args[0], nil, &p); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), p)
			}
			if raw {
				_, err := cmd.OutOrStdout().Write([]byte(p.SpecRaw))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"id          : %d\nproject_id  : %d\nname        : %s\nversion     : %d\nenabled     : %v\n\n--- spec_raw ---\n%s",
				p.ID, p.ProjectID, p.Name, p.Version, p.Enabled, p.SpecRaw)
			return nil
		},
	}
	c.Flags().BoolVar(&raw, "raw", false, "仅输出 spec_raw YAML")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newPipelinesValidateCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "validate [file]",
		Short: "校验流水线 YAML (默认 stdin 或 -f 文件)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			raw, err := readYAMLInput(file, args)
			if err != nil {
				return err
			}
			var resp validateResult
			if err := cli.Do("POST", "/api/v1/pipelines/validate",
				map[string]string{"spec_raw": raw}, &resp); err != nil {
				return err
			}
			if resp.Valid {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ valid (%d stages)\n", resp.Summary.StageCount)
				return nil
			}
			for _, e := range resp.Errors {
				line := ""
				if e.Line > 0 {
					line = fmt.Sprintf(":%d", e.Line)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%s %s\n", e.Kind, line, e.Message)
			}
			return fmt.Errorf("validation failed (%d errors)", len(resp.Errors))
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "YAML 文件路径")
	return c
}

func newPipelinesApplyCmd() *cobra.Command {
	var (
		file    string
		message string
	)
	c := &cobra.Command{
		Use:   "apply <pipeline-id> [file]",
		Short: "应用 YAML 到流水线 (创建新版本)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			id := args[0]
			fileArgs := args[1:]
			raw, err := readYAMLInput(file, fileArgs)
			if err != nil {
				return err
			}
			body := map[string]string{"spec_raw": raw}
			if message != "" {
				body["message"] = message
			}
			var pv struct {
				ID      int64 `json:"id"`
				Version int   `json:"version"`
			}
			if err := cli.Do("PUT", "/api/v1/pipelines/"+id+"/spec", body, &pv); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已保存 pipeline %s → v%d (version_id=%d)\n", id, pv.Version, pv.ID)
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "YAML 文件路径")
	c.Flags().StringVarP(&message, "message", "m", "", "版本备注")
	return c
}

type pipelineSummary struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
}

type pipelineDetail struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Enabled   bool   `json:"enabled"`
	SpecRaw   string `json:"spec_raw"`
}

type validateResult struct {
	Valid   bool `json:"valid"`
	Errors  []struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Line    int    `json:"line"`
	} `json:"errors"`
	Summary struct {
		StageCount int `json:"stage_count"`
	} `json:"summary"`
}

func writePipelineTable(w io.Writer, list []pipelineSummary) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPROJECT\tNAME\tENABLED")
	for _, p := range list {
		en := "yes"
		if !p.Enabled {
			en = "no"
		}
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\n", p.ID, p.ProjectID, truncate(p.Name, 30), en)
	}
	return tw.Flush()
}

func readYAMLInput(flagFile string, args []string) (string, error) {
	if flagFile != "" {
		b, err := os.ReadFile(flagFile)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		return string(b), nil
	}
	if len(args) > 0 {
		b, err := os.ReadFile(args[0])
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		return string(b), nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return "", fmt.Errorf("no YAML input (use -f, file arg, or stdin)")
	}
	return string(b), nil
}
