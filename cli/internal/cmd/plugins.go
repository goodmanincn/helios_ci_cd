package cmd

import (
	"fmt"
	"net/url"
	"text/tabwriter"
	"os"

	"github.com/spf13/cobra"
)

// helios plugins — 插件市场 (M9).
func newPluginsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "plugins",
		Short: "插件市场",
	}
	c.AddCommand(
		newPluginsListCmd(),
		newPluginsGetCmd(),
		newPluginsInstallCmd(),
		newPluginsUninstallCmd(),
		newPluginsInstalledCmd(),
	)
	return c
}

func newPluginsListCmd() *cobra.Command {
	var (
		category string
		q        string
		verified bool
		jsonOut  bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "列出可用插件",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			qs := url.Values{}
			if category != "" {
				qs.Set("category", category)
			}
			if q != "" {
				qs.Set("q", q)
			}
			if verified {
				qs.Set("verified", "true")
			}
			path := "/api/v1/plugins"
			if s := qs.Encode(); s != "" {
				path += "?" + s
			}
			var list []pluginEntry
			if err := cli.Do("GET", path, nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tCATEGORY\tDOWNLOADS\tVERIFIED")
			for _, p := range list {
				v := ""
				if p.Verified {
					v = "✓"
				}
				fmt.Fprintf(w, "%s/%s\t%s\t%d\t%s\n", p.Namespace, p.Name, p.Category, p.Downloads, v)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVar(&category, "category", "", "按 category 过滤")
	c.Flags().StringVarP(&q, "query", "q", "", "搜索")
	c.Flags().BoolVar(&verified, "verified", false, "只看已验证")
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newPluginsGetCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "get <namespace>/<name>",
		Short: "查看插件详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			slug := args[0]
			var detail pluginDetail
			if err := cli.Do("GET", "/api/v1/plugins/"+slug, nil, &detail); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), detail)
			}
			p := detail.Plugin
			fmt.Printf("名称:   %s/%s\n", p.Namespace, p.Name)
			fmt.Printf("描述:   %s\n", p.Description)
			fmt.Printf("分类:   %s\n", p.Category)
			fmt.Printf("发布者: %s\n", p.Publisher)
			fmt.Printf("验证:   %v\n", p.Verified)
			fmt.Printf("下载:   %d\n", p.Downloads)
			if detail.Installed != nil && *detail.Installed {
				fmt.Printf("已安装: 是 (版本 %s)\n", detail.InstalledVersion)
			} else {
				fmt.Println("已安装: 否")
			}
			fmt.Printf("\n版本 (%d):\n", len(detail.Versions))
			for _, v := range detail.Versions {
				latest := ""
				if v.IsLatest {
					latest = " (latest)"
				}
				fmt.Printf("  %s%s\n", v.Version, latest)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

func newPluginsInstallCmd() *cobra.Command {
	var version string
	c := &cobra.Command{
		Use:   "install <namespace>/<name>",
		Short: "安装插件到当前 org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			slug := args[0]
			body := map[string]string{}
			if version != "" {
				body["version"] = version
			}
			var result installResult
			if err := cli.Do("POST", "/api/v1/plugins/"+slug+"/install", body, &result); err != nil {
				return err
			}
			fmt.Printf("已安装 %s (版本 %s, org_id=%d)\n", slug, result.Version, result.OrgID)
			return nil
		},
	}
	c.Flags().StringVar(&version, "version", "", "指定版本 (默认 latest)")
	return c
}

func newPluginsUninstallCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "uninstall <namespace>/<name>",
		Short: "卸载插件",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			slug := args[0]
			if err := cli.Do("DELETE", "/api/v1/plugins/"+slug+"/install", nil, nil); err != nil {
				return err
			}
			fmt.Printf("已卸载 %s\n", slug)
			return nil
		},
	}
	return c
}

func newPluginsInstalledCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "installed",
		Short: "列出当前 org 已安装的插件",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, _, err := resolveClient()
			if err != nil {
				return err
			}
			var list []installedEntry
			if err := cli.Do("GET", "/api/v1/plugins/installed", nil, &list); err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), list)
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tVERSION\tINSTALLED")
			for _, it := range list {
				slug := it.Plugin.Namespace + "/" + it.Plugin.Name
				fmt.Fprintf(w, "%s\t%s\t%s\n", slug, it.Version.Version, it.Installation.InstalledAt)
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "输出原始 JSON")
	return c
}

// ---- CLI API 类型 (轻量, 只取需要的字段) ----

type pluginEntry struct {
	ID          int64  `json:"id"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Publisher   string `json:"publisher"`
	Verified    bool   `json:"verified"`
	Downloads   int64  `json:"downloads"`
}

type pluginDetail struct {
	Plugin          pluginEntry    `json:"plugin"`
	Versions        []versionEntry `json:"versions"`
	Installed       *bool          `json:"installed,omitempty"`
	InstalledVersion string        `json:"installed_version,omitempty"`
}

type versionEntry struct {
	ID       int64  `json:"id"`
	Version  string `json:"version"`
	IsLatest bool   `json:"is_latest"`
}

type installResult struct {
	PluginID int64  `json:"plugin_id"`
	VersionID int64 `json:"version_id"`
	Version  string `json:"version"`
	OrgID    int64  `json:"org_id"`
}

type installedEntry struct {
	Installation struct {
		ID          int64  `json:"id"`
		InstalledAt string `json:"installed_at"`
	} `json:"installation"`
	Plugin  pluginEntry `json:"plugin"`
	Version versionEntry `json:"version"`
}
