// Package clusterhealth 集群健康检查定时任务 (T4.1.4)。
package clusterhealth

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/helios-cicd/helios/api/pkg/cluster"
	"github.com/helios-cicd/helios/api/pkg/cluster/selfhosted"
)

// Start 启动每 1 分钟一轮的健康检查循环。
func Start(db *sql.DB) {
	go loop(db)
}

func loop(db *sql.DB) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	// 启动时立即执行一次
	checkAll(db)
	for range ticker.C {
		checkAll(db)
	}
}

func checkAll(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT id, provider, config, status FROM clusters
		WHERE deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("cluster health: query failed: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var provider, status string
		var configRaw []byte
		if err := rows.Scan(&id, &provider, &configRaw, &status); err != nil {
			log.Printf("cluster health: scan failed: %v", err)
			continue
		}
		if provider != "selfhosted" {
			continue // 暂时只支持自建
		}
		var cfgMap map[string]string
		if err := json.Unmarshal(configRaw, &cfgMap); err != nil {
			log.Printf("cluster health: id=%d unmarshal config failed: %v", id, err)
			continue
		}
		kubeconfig := cfgMap["kubeconfig"]
		if kubeconfig == "" {
			log.Printf("cluster health: id=%d empty kubeconfig", id)
			continue
		}

		p, err := selfhosted.New(cluster.ClusterConfig{
			Provider:   provider,
			Kubeconfig: []byte(kubeconfig),
		})
		if err != nil {
			markStatus(db, id, "disconnected")
			continue
		}

		info, err := p.HealthCheck(ctx)
		if err != nil {
			newStatus := degrade(status)
			markStatus(db, id, newStatus)
			log.Printf("cluster health: id=%d unhealthy (%s)", id, err)
			continue
		}

		markHealthy(db, id)
		log.Printf("cluster health: id=%d healthy (version=%s nodes=%d ns=%d)",
			id, info.Version, info.NodeCount, info.NamespaceCount)
	}
}

func degrade(current string) string {
	switch current {
	case "unknown", "healthy":
		return "degraded"
	case "degraded":
		return "unhealthy"
	default:
		return "disconnected"
	}
}

func markStatus(db *sql.DB, id int64, status string) {
	_, err := db.Exec(`
		UPDATE clusters SET status = $1, last_health_check = NOW(), updated_at = NOW()
		WHERE id = $2
	`, status, id)
	if err != nil {
		log.Printf("cluster health: update status id=%d failed: %v", id, err)
	}
}

func markHealthy(db *sql.DB, id int64) {
	_, err := db.Exec(`
		UPDATE clusters SET status = 'healthy', last_health_check = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		log.Printf("cluster health: update healthy id=%d failed: %v", id, err)
	}
}
