// ssh_deploy.go — helios/ssh-deploy@v1 builtin step (E6.3 + E6.4)。
package builtin

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"

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

	// 并发策略
	strategy := sshrunner.Strategy(OptString(inputs, "strategy", "serial"))
	batchSize, _ := strconv.Atoi(OptString(inputs, "batch_size", "1"))
	if batchSize <= 0 {
		batchSize = 1
	}
	intervalSec, _ := strconv.Atoi(OptString(inputs, "interval", "0"))
	maxFailures, _ := strconv.Atoi(OptString(inputs, "max_failures", "0"))
	maxConcurrency, _ := strconv.Atoi(OptString(inputs, "max_concurrency", "5"))
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}

	// 认证
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
		if v, ok := ec.Secrets["SSH_PRIVATE_KEY"]; ok {
			authCfg.PrivateKey = v
		} else if v, ok := ec.Secrets["SSH_PASSWORD"]; ok {
			authCfg.Password = v
		}
	}

	be := sshrunner.NewBatchExecutor(sshrunner.BatchOpts{
		Strategy:       strategy,
		BatchSize:      batchSize,
		Interval:       time.Duration(intervalSec) * time.Second,
		MaxFailures:    maxFailures,
		MaxConcurrency: maxConcurrency,
	})

	deployedHosts := []string{} // 记录已成功部署的主机,用于回滚
	mu := &sync.Mutex{}         // 保护 deployedHosts

	results := be.Run(ec.Ctx, hosts, func(ctx context.Context, host string) error {
		return s.deployHost(ctx, ec, host, port, user, authCfg, source, dest, beforeScript, restartCmd, afterScript, mu, &deployedHosts)
	})

	var successHosts, failedHosts []string
	anyFailed := false
	for _, r := range results {
		if r.Error != nil {
			failedHosts = append(failedHosts, r.Host)
			anyFailed = true
		} else {
			successHosts = append(successHosts, r.Host)
		}
	}

	// 如果存在失败,对已部署的主机回滚
	if anyFailed && len(deployedHosts) > 0 {
		fmt.Fprintf(ec.Log, "[ssh-deploy] rolling back %d deployed hosts…\n", len(deployedHosts))
		for _, host := range deployedHosts {
			if err := s.rollbackHost(ec, host, port, user, authCfg, dest); err != nil {
				fmt.Fprintf(ec.Log, "[ssh-deploy] rollback %s failed: %v\n", host, err)
			} else {
				fmt.Fprintf(ec.Log, "[ssh-deploy] rollback %s done\n", host)
			}
		}
	}

	outputs := map[string]any{
		"success_hosts": successHosts,
		"failed_hosts":  failedHosts,
	}
	if anyFailed {
		return outputs, fmt.Errorf("ssh-deploy: %d/%d hosts failed", len(failedHosts), len(hosts))
	}
	return outputs, nil
}

func (s *SSHDeployStep) deployHost(
	ctx context.Context,
	ec *ExecContext,
	host string,
	port int,
	user string,
	authCfg sshrunner.AuthConfig,
	source, dest, beforeScript, restartCmd, afterScript string,
	mu *sync.Mutex,
	deployedHosts *[]string,
) error {
	dialCfg := sshrunner.DialConfig{
		Host:    host,
		Port:    port,
		User:    user,
		Auth:    authCfg,
		Timeout: 0,
	}

	client, err := sshrunner.Dial(dialCfg)
	if err != nil {
		fmt.Fprintf(ec.Log, "[ssh-deploy] %s: dial failed: %v\n", host, err)
		return err
	}
	defer client.Close()

	// snapshot (备份旧目录)
	backupTag := fmt.Sprintf("%d", time.Now().Unix())
	if dest != "" {
		backupCmd := fmt.Sprintf("if [ -e %q ]; then cp -r %q %q; fi", dest, dest, dest+".helios-bak-"+backupTag)
		_ = runSSHCmd(client, backupCmd, ec.Log) // 忽略备份失败
	}

	// 文件传输
	if source != "" && dest != "" {
		sftpCli, err := sshrunner.NewSFTP(client)
		if err != nil {
			fmt.Fprintf(ec.Log, "[ssh-deploy] %s: sftp init failed: %v\n", host, err)
			return err
		}
		srcPath := filepath.Join(ec.WorkDir, source)
		if err := sftpCli.UploadFile(srcPath, dest); err != nil {
			if dirErr := sftpCli.UploadDir(srcPath, dest); dirErr != nil {
				fmt.Fprintf(ec.Log, "[ssh-deploy] %s: upload failed: %v\n", host, err)
				_ = sftpCli.Close()
				return err
			}
		}
		_ = sftpCli.Close()
	}

	// before_script
	if beforeScript != "" {
		if err := runSSHCmd(client, beforeScript, ec.Log); err != nil {
			fmt.Fprintf(ec.Log, "[ssh-deploy] %s: before_script failed: %v\n", host, err)
			return err
		}
	}

	// restart_command
	if restartCmd != "" {
		if err := runSSHCmd(client, restartCmd, ec.Log); err != nil {
			fmt.Fprintf(ec.Log, "[ssh-deploy] %s: restart_command failed: %v\n", host, err)
			return err
		}
	}

	// after_script
	if afterScript != "" {
		if err := runSSHCmd(client, afterScript, ec.Log); err != nil {
			fmt.Fprintf(ec.Log, "[ssh-deploy] %s: after_script failed: %v\n", host, err)
			return err
		}
	}

	fmt.Fprintf(ec.Log, "[ssh-deploy] %s: deployed\n", host)
	mu.Lock()
	*deployedHosts = append(*deployedHosts, host)
	mu.Unlock()
	return nil
}

func (s *SSHDeployStep) rollbackHost(
	ec *ExecContext,
	host string,
	port int,
	user string,
	authCfg sshrunner.AuthConfig,
	dest string,
) error {
	if dest == "" {
		return nil
	}
	dialCfg := sshrunner.DialConfig{
		Host:    host,
		Port:    port,
		User:    user,
		Auth:    authCfg,
		Timeout: 0,
	}
	client, err := sshrunner.Dial(dialCfg)
	if err != nil {
		return err
	}
	defer client.Close()

	// 恢复最新的 helios-bak 备份
	restoreCmd := fmt.Sprintf(
		`bak=$(ls -d %q.helios-bak-* 2>/dev/null | sort | tail -1); if [ -n "$bak" ]; then rm -rf %q && cp -r "$bak" %q && rm -rf "$bak"; fi`,
		dest, dest, dest,
	)
	return runSSHCmd(client, restoreCmd, ec.Log)
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
