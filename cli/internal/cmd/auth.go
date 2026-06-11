package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/helios-cicd/helios/cli/internal/apiclient"
	"github.com/helios-cicd/helios/cli/internal/config"
)

// helios login — 当前实现用户名/密码 (T8.3.2 OAuth 浏览器流后续接).
func newLoginCmd() *cobra.Command {
	var (
		server   string
		username string
		profile  string
		orgID    int64
	)
	c := &cobra.Command{
		Use:   "login",
		Short: "登录 Helios 平台并保存 token",
		Long: `用用户名/密码登录, 把 token 持久化到 ~/.helios/credentials.yaml.

示例:
  helios login --server https://helios.example.com
  helios login --profile staging --server https://staging.helios.example.com --username alice`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if server == "" {
				return fmt.Errorf("--server is required")
			}
			if profile == "" {
				profile = "default"
			}
			if username == "" {
				fmt.Print("Username: ")
				r := bufio.NewReader(os.Stdin)
				line, _ := r.ReadString('\n')
				username = strings.TrimSpace(line)
				if username == "" {
					return fmt.Errorf("username is required")
				}
			}
			fmt.Print("Password: ")
			pwBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password := string(pwBytes)
			if password == "" {
				return fmt.Errorf("password cannot be empty")
			}

			// 调 /api/v1/auth/login. server URL 末尾允许带/不带斜杠.
			base := strings.TrimRight(server, "/")
			tmpClient := apiclient.New(base, "", 0)
			var resp loginResp
			if err := tmpClient.Do("POST", "/api/v1/auth/login",
				map[string]string{"username": username, "password": password},
				&resp); err != nil {
				return fmt.Errorf("login: %w", err)
			}
			if resp.AccessToken == "" {
				return fmt.Errorf("login: server returned no access_token")
			}

			// 写 config + credentials
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			cfg.Profiles[profile] = config.Profile{Server: base, OrgID: orgID}
			if cfg.DefaultProfile == "" {
				cfg.DefaultProfile = profile
			}
			if err := config.SaveConfig(cfg); err != nil {
				return err
			}
			creds, err := config.LoadCredentials()
			if err != nil {
				return err
			}
			creds.Tokens[profile] = config.Token{
				AccessToken:  resp.AccessToken,
				RefreshToken: resp.RefreshToken,
				Username:     username,
			}
			if err := config.SaveCredentials(creds); err != nil {
				return err
			}

			d, _ := config.Dir()
			fmt.Fprintf(cmd.OutOrStdout(),
				"已登录 %s (profile=%s, server=%s)\n凭据写入 %s/credentials.yaml\n",
				username, profile, base, d)
			return nil
		},
	}
	c.Flags().StringVar(&server, "server", "", "API server URL, e.g. https://helios.example.com")
	c.Flags().StringVarP(&username, "username", "u", "", "用户名 (留空则交互输入)")
	c.Flags().StringVar(&profile, "profile", "default", "profile 名 (多 server 切换用)")
	c.Flags().Int64Var(&orgID, "org-id", 0, "默认 org_id (写入 X-Org-ID, 不指定走 token 第一个)")
	return c
}

type loginResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "清除当前 profile 的 token (不删 profile 本身)",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _, _, err := config.Active(profileFlag)
			if err != nil {
				return err
			}
			creds, err := config.LoadCredentials()
			if err != nil {
				return err
			}
			delete(creds.Tokens, name)
			if err := config.SaveCredentials(creds); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已清除 profile %q 的 token\n", name)
			return nil
		},
	}
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "显示当前 profile + 登录用户",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, name, err := resolveClient()
			if err != nil {
				return err
			}
			var me struct {
				User struct {
					ID          int64  `json:"id"`
					Username    string `json:"username"`
					Email       string `json:"email"`
					DisplayName string `json:"display_name"`
				} `json:"user"`
				Orgs []int64 `json:"orgs"`
			}
			if err := cli.Do("GET", "/api/v1/auth/me", nil, &me); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"profile : %s\nserver  : %s\nuser    : %s (id=%d, email=%s)\norgs    : %v\n",
				name, cli.BaseURL, me.User.Username, me.User.ID, me.User.Email, me.Orgs)
			return nil
		},
	}
}
