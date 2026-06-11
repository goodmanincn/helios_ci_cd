// ssh_deploy.go — helios/ssh-deploy@v1 builtin step (E6.3)。
package builtin

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/helios-cicd/helios/api/pkg/sshrunner"
)

// SSHDeployStep ssh-deploy 内置 step。
type SSHDeployStep struct{}

func init() { Register(&SSHDeployStep{}) }

func (s *SSHDeployStep) Name() string { return "helios/ssh-deploy@v1" }

func (s *SSHDeployStep) Run(ec *ExecContext, inputs map[string]any) (map[string]any, error) {
	// 解析参数
	hosts, err := StringList(inputs, "hosts")
	if err != nil {
		return nil, fmt.Errorf("ssh-deploy: %w", err)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("ssh-deploy: hosts is required")
	}

	user, err := MustString(s.Name(), inputs, "user")
	if err != nil {
		return nil, err
	}
	portStr := OptString(inputs, "port", "22")
	port, _ := strconv.Atoi(portStr)
	if port <= 0 {
		port = 22
	}

	source, _ := MustString(s.Name(), inputs, "source")
	dest, _ := MustString(s.Name(), inputs, "dest")

	beforeScript := OptString(inputs, "before_script", "")
	afterScript := OptString(inputs, "after_script", "")
	restartCmd := OptString(inputs, "restart_command", "")

	// 认证: 优先私钥, 其次密码, 再次 ExecContext.Secrets
	authCfg := sshrunner.AuthConfig{}
	if pk, ok := inputs["private_key"]; ok {
		if s, ok := pk.(string); ok && s != "" {
			authCfg.PrivateKey = s
		}
	}
	if pw, ok := inputs["password"]; ok {
		if s, ok := pw.(string); ok && s != "" {
			authCfg.Password = s
		}
	}
	if authCfg.PrivateKey == "" && authCfg.Password == "" && ec.Secrets != nil {
		// 尝试从 secrets 中找 SSH_PRIVATE_KEY 或 SSH_PASSWORD
		if v, ok := ec.Secrets["SSH_PRIVATE_KEY"]; ok {
			authCfg.PrivateKey = v
		} else if v, ok := ec.Secrets["SSH_PASSWORD"]; ok {
			authCfg.Password = v
		}
	}

	var successHosts, failedHosts []string

	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		dialCfg := sshrunner.DialConfig{
			Host:    host,
			Port:    port,
			User:    user,
			Auth:    authCfg,
			Timeout: 0, // 使用默认
		}

		client, err := sshrunner.Dial(dialCfg)
		if err != nil {
			fmt.Fprintf(ec.Log, "[ssh-deploy] %s: dial failed: %v\n", host, err)
			failedHosts = append(failedHosts, host)
			continue
		}

		// 文件传输
		if source != "" && dest != "" {
			sftpCli, err := sshrunner.NewSFTP(client)
			if err != nil {
				fmt.Fprintf(ec.Log, "[ssh-deploy] %s: sftp init failed: %v\n", host, err)
				_ = client.Close()
				failedHosts = append(failedHosts, host)
				continue
			}

			srcPath := filepath.Join(ec.WorkDir, source)

			// 简单判断文件/目录: 传单个文件
			if err := sftpCli.UploadFile(srcPath, dest); err != nil {
				// 可能是目录,尝试递归
				if dirErr := sftpCli.UploadDir(srcPath, dest); dirErr != nil {
					fmt.Fprintf(ec.Log, "[ssh-deploy] %s: upload failed: %v\n", host, err)
					_ = sftpCli.Close()
					_ = client.Close()
					failedHosts = append(failedHosts, host)
					continue
				}
			}
			_ = sftpCli.Close()
		}

		// before_script
		if beforeScript != "" {
			if err := runSSHCmd(client, beforeScript, ec.Log); err != nil {
				fmt.Fprintf(ec.Log, "[ssh-deploy] %s: before_script failed: %v\n", host, err)
				_ = client.Close()
				failedHosts = append(failedHosts, host)
				continue
			}
		}

		// restart_command
		if restartCmd != "" {
			if err := runSSHCmd(client, restartCmd, ec.Log); err != nil {
				fmt.Fprintf(ec.Log, "[ssh-deploy] %s: restart_command failed: %v\n", host, err)
				_ = client.Close()
				failedHosts = append(failedHosts, host)
				continue
			}
		}

		// after_script
		if afterScript != "" {
			if err := runSSHCmd(client, afterScript, ec.Log); err != nil {
				fmt.Fprintf(ec.Log, "[ssh-deploy] %s: after_script failed: %v\n", host, err)
				_ = client.Close()
				failedHosts = append(failedHosts, host)
				continue
			}
		}

		_ = client.Close()
		successHosts = append(successHosts, host)
		fmt.Fprintf(ec.Log, "[ssh-deploy] %s: done\n", host)
	}

	outputs := map[string]any{
		"success_hosts": successHosts,
		"failed_hosts":  failedHosts,
	}
	if len(failedHosts) > 0 {
		return outputs, fmt.Errorf("ssh-deploy: %d/%d hosts failed", len(failedHosts), len(hosts))
	}
	return outputs, nil
}

func runSSHCmd(client *sshrunner.Client, cmd string, log io.Writer) error {
	exec := sshrunner.NewExecutor(client)
	res, err := exec.Exec(context.Background(), sshrunner.ExecSpec{Command: cmd})
	if err != nil {
		return err
	}
	if len(res.Stdout) > 0 {
		fmt.Fprintf(log, "%s", res.Stdout)
	}
	if len(res.Stderr) > 0 {
		fmt.Fprintf(log, "%s", res.Stderr)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("exit code %d", res.ExitCode)
	}
	return nil
}
