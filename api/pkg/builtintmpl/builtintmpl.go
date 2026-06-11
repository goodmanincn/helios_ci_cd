// Package builtintmpl — M8 T8.2.1 内置流水线模板 seed。
//
// 启动时由 cmd/api/main.go 调一次, INSERT ... ON CONFLICT DO NOTHING.
// 之后用户在 UI/CLI 看到的"模板市场"就有现成的可克隆。
//
// 模板按 slug 唯一; 若已存在但 spec 变化, 当前不自动升级 (留给后续 spec_raw 比对 + 二次确认)。
package builtintmpl

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// Seed 把内置模板写入 pipeline_templates, slug 冲突跳过。
func Seed(db *gorm.DB) (int, error) {
	tmpls := builtins()
	added := 0
	for _, t := range tmpls {
		// 校验 spec, 内置模板必须能通过
		parsed, errs := dsl.ValidateRaw([]byte(t.SpecRaw))
		if len(errs) > 0 {
			return added, fmt.Errorf("builtin template %q invalid spec: %v", t.Slug, errs)
		}
		specJSON, _ := json.Marshal(parsed)
		t.Spec = datatypes.JSON(specJSON)
		t.Builtin = true

		var existing model.PipelineTemplate
		err := db.Where("slug = ?", t.Slug).First(&existing).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return added, err
		}
		if err := db.Create(&t).Error; err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func builtins() []model.PipelineTemplate {
	return []model.PipelineTemplate{
		{
			Slug:        "node-docker-k8s",
			Name:        "Node.js + Docker + K8s",
			Description: "pnpm install → test → 镜像构建推送 → K8s rolling 部署",
			Category:    "fullstack",
			Tags:        []string{"node", "docker", "k8s"},
			SpecRaw:     nodeDockerK8sYAML,
		},
		{
			Slug:        "go-multi-platform",
			Name:        "Go 多平台二进制",
			Description: "go test → 矩阵构建 (linux/darwin × amd64/arm64)",
			Category:    "release",
			Tags:        []string{"go", "release"},
			SpecRaw:     goMultiPlatformYAML,
		},
		{
			Slug:        "static-site-s3",
			Name:        "静态站点发布",
			Description: "next build → next export → 上传到 OSS / S3",
			Category:    "deploy",
			Tags:        []string{"node", "static", "s3"},
			SpecRaw:     staticSiteYAML,
		},
	}
}

// 三个内置模板的 YAML — 都用 spec/04 § 4 的语法, 跑 dsl.ValidateRaw 必过。
const nodeDockerK8sYAML = `version: "1"
name: "node-docker-k8s"
description: "Node.js 项目: 装依赖 / 测试 / 构镜像 / 部署到 K8s"

triggers:
  - on: push
    branches: [main]

env:
  REGISTRY: ccr.example.com/acme
  IMAGE_NAME: app

stages:
  - id: install
    name: "安装依赖"
    runs-on: { type: container, image: "node:20-alpine" }
    steps:
      - run: |
          corepack enable
          pnpm install --frozen-lockfile

  - id: test
    name: "单元测试"
    needs: [install]
    runs-on: { type: container, image: "node:20-alpine" }
    steps:
      - run: pnpm test

  - id: build-image
    name: "构建镜像"
    needs: [test]
    runs-on: { type: container, image: "gcr.io/kaniko-project/executor:latest" }
    steps:
      - run: |
          /kaniko/executor \
            --dockerfile=Dockerfile \
            --destination=${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}

  - id: deploy
    name: "K8s 部署"
    needs: [build-image]
    runs-on: { type: container, image: "bitnami/kubectl:latest" }
    steps:
      - run: |
          kubectl set image deployment/app app=${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}
          kubectl rollout status deployment/app
`

const goMultiPlatformYAML = `version: "1"
name: "go-multi-platform"
description: "Go 项目: 测试 + 多平台二进制"

triggers:
  - on: push
    tags: ["v*"]

env:
  GO_VERSION: "1.22"

stages:
  - id: test
    name: "单元测试"
    runs-on: { type: container, image: "golang:1.22" }
    steps:
      - run: go test ./... -race -cover

  - id: build
    name: "矩阵构建"
    needs: [test]
    runs-on: { type: container, image: "golang:1.22" }
    matrix:
      os: [linux, darwin]
      arch: [amd64, arm64]
    steps:
      - run: |
          GOOS=${{ matrix.os }} GOARCH=${{ matrix.arch }} \
            go build -o bin/app-${{ matrix.os }}-${{ matrix.arch }} ./cmd/app
`

const staticSiteYAML = `version: "1"
name: "static-site-s3"
description: "Next.js 静态站点: build + export + 上传 OSS"

triggers:
  - on: push
    branches: [main]

stages:
  - id: build
    name: "构建静态资产"
    runs-on: { type: container, image: "node:20-alpine" }
    steps:
      - run: |
          corepack enable
          pnpm install --frozen-lockfile
          pnpm build
          pnpm export

  - id: upload
    name: "上传到 OSS"
    needs: [build]
    runs-on: { type: container, image: "amazon/aws-cli:latest" }
    steps:
      - run: aws s3 sync out/ s3://my-bucket/ --delete
`
