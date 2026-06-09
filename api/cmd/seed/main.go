// Package main 是 helios seed 命令 —— 灌入开发用的默认 org / 用户 / demo 项目。
// 幂等:重复跑不报错,不创建重复数据。
package main

import (
	"errors"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/model"
)

const (
	defaultOrgSlug     = "acme"
	defaultOrgName     = "Acme Inc."
	defaultAdminUser   = "admin"
	defaultAdminEmail  = "admin@helios.local"
	defaultAdminPasswd = "helios..." // 仅 dev,生产 seed 必须改
	demoProjectSlug    = "api-gateway"
	demoProjectRepoURL = "https://github.com/helios-cicd/demo-api.git"
)

func main() {
	dsn := os.Getenv("HELIOS_DB_DSN")
	if dsn == "" {
		log.Fatal("seed: HELIOS_DB_DSN 未设置")
	}

	log.Println("seed: 确保 schema 最新...")
	if err := db.Migrate(dsn); err != nil {
		log.Fatalf("seed: migrate 失败: %v", err)
	}

	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("seed: open db: %v", err)
	}

	// 1. admin user
	user, err := upsertUser(gdb)
	if err != nil {
		log.Fatalf("seed: user: %v", err)
	}
	log.Printf("✓ user: id=%d username=%s", user.ID, user.Username)

	// 2. default org + owner membership
	org, err := upsertOrg(gdb, user.ID)
	if err != nil {
		log.Fatalf("seed: org: %v", err)
	}
	log.Printf("✓ org: id=%d slug=%s", org.ID, org.Slug)

	if err := upsertMembership(gdb, org.ID, user.ID, "owner"); err != nil {
		log.Fatalf("seed: membership: %v", err)
	}
	log.Println("✓ membership: admin → owner of acme")

	// 3. demo project
	proj, err := upsertProject(gdb, org.ID, user.ID)
	if err != nil {
		log.Fatalf("seed: project: %v", err)
	}
	log.Printf("✓ project: id=%d slug=%s repo=%s", proj.ID, proj.Slug, proj.RepoURL)

	log.Println()
	log.Println("=== seed 完成 ===")
	log.Printf("登录: %s / %s", defaultAdminUser, defaultAdminPasswd)
	log.Printf("组织: %s (slug=%s)", org.Name, org.Slug)
	log.Printf("项目: %s/%s", org.Slug, proj.Slug)
}

func upsertUser(gdb *gorm.DB) (*model.User, error) {
	var u model.User
	err := gdb.Where("username = ?", defaultAdminUser).First(&u).Error
	if err == nil {
		return &u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPasswd), 12)
	if err != nil {
		return nil, err
	}
	u = model.User{
		Username:     defaultAdminUser,
		Email:        defaultAdminEmail,
		PasswordHash: string(hash),
		DisplayName:  "Administrator",
		IsActive:     true,
	}
	return &u, gdb.Create(&u).Error
}

func upsertOrg(gdb *gorm.DB, ownerID int64) (*model.Organization, error) {
	var org model.Organization
	err := gdb.Where("slug = ?", defaultOrgSlug).First(&org).Error
	if err == nil {
		return &org, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	org = model.Organization{
		Name:        defaultOrgName,
		Slug:        defaultOrgSlug,
		OwnerID:     &ownerID,
		Description: "Default dev organization",
	}
	return &org, gdb.Create(&org).Error
}

func upsertMembership(gdb *gorm.DB, orgID, userID int64, role string) error {
	var m model.OrgMember
	err := gdb.Where("org_id = ? AND user_id = ?", orgID, userID).First(&m).Error
	if err == nil {
		return nil // 已存在
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return gdb.Create(&model.OrgMember{OrgID: orgID, UserID: userID, Role: role}).Error
}

func upsertProject(gdb *gorm.DB, orgID, ownerID int64) (*model.Project, error) {
	var p model.Project
	err := gdb.Where("org_id = ? AND slug = ?", orgID, demoProjectSlug).First(&p).Error
	if err == nil {
		return &p, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	p = model.Project{
		OrgID:         orgID,
		Name:          "API Gateway (demo)",
		Slug:          demoProjectSlug,
		Description:   "Demo project for development",
		RepoURL:       demoProjectRepoURL,
		RepoType:      "github",
		DefaultBranch: "main",
		Visibility:    "private",
		Config:        []byte(`{}`),
		CreatedBy:     &ownerID,
	}
	return &p, gdb.Create(&p).Error
}
