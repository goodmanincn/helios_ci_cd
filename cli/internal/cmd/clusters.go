package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newClustersCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "clusters",
		Short: "K8s 集群管理",
	}
	c.AddCommand(newClustersListCmd(), newClustersTestCmd())
	return c
}

func newClustersListCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "list",
		Short: "列出已接入集群",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			var list []clusterItem
			if err := cli.Do("GET", "/api/v1/clusters", nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			return writeClustersTable(cmd.OutOrStdout(), list)
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newClustersTestCmd() *cobra.Command {
	var (
		provider   string
		kubeconfig string
		cloudFile  string
	)
	c := &cobra.Command{
		Use:   "test",
		Short: "测试集群连通性 (凭据不入库)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			if provider == "" {
				return fmt.Errorf("--provider required (selfhosted/tke/ack)")
			}
			body := map[string]any{"provider": provider}
			if kubeconfig != "" {
				b, err := os.ReadFile(kubeconfig)
				if err != nil {
					return fmt.Errorf("read kubeconfig: %w", err)
				}
				body["kubeconfig"] = string(b)
			}
			if cloudFile != "" {
				b, err := os.ReadFile(cloudFile)
				if err != nil {
					return fmt.Errorf("read cloud creds: %w", err)
				}
				body["cloud"] = json.RawMessage(b)
			}
			var info struct {
				Version         string `json:"version"`
				NodeCount       int    `json:"node_count"`
				NamespaceCount  int    `json:"namespace_count"`
				Healthy         bool   `json:"healthy"`
			}
			if err := cli.Do("POST", "/api/v1/clusters/test", body, &info); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"healthy=%v version=%s nodes=%d namespaces=%d\n",
				info.Healthy, info.Version, info.NodeCount, info.NamespaceCount)
			return nil
		},
	}
	c.Flags().StringVar(&provider, "provider", "", "集群类型: selfhosted / tke / ack")
	c.Flags().StringVar(&kubeconfig, "kubeconfig", "", "kubeconfig 文件路径 (selfhosted)")
	c.Flags().StringVar(&cloudFile, "cloud-file", "", "云凭据 JSON 文件 (tke/ack)")
	return c
}

type clusterItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	Status   string `json:"status"`
}

func writeClustersTable(w io.Writer, list []clusterItem) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPROVIDER\tREGION\tSTATUS")
	for _, c := range list {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", c.ID, c.Name, c.Provider, c.Region, c.Status)
	}
	return tw.Flush()
}
