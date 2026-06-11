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
			Description: "pnpm install → test → 镜像构建 → staging 部署 → 审批 → 生产 rolling 部署",
			Category:    "fullstack",
			Tags:        []string{"node", "docker", "k8s", "approval"},
			SpecRaw:     nodeDockerK8sYAML,
		},
		{
			Slug:        "go-multi-platform",
			Name:        "Go 多平台二进制 + GitHub Release",
			Description: "go test → 矩阵构建 (linux/darwin/windows × amd64/arm64) → GitHub Release 上传 6 个 binary",
			Category:    "release",
			Tags:        []string{"go", "release", "matrix"},
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
		{
			Slug:        "python-pypi",
			Name:        "Python + PyPI 发布",
			Description: "pytest 单元测试 → build + twine 发布到 PyPI",
			Category:    "release",
			Tags:        []string{"python", "pypi", "test"},
			SpecRaw:     pythonPyPIYAML,
		},
		{
			Slug:        "multi-cloud-tke-ack",
			Name:        "多云容器部署 (TKE + ACK)",
			Description: "镜像构建 → TKE / ACK 矩阵并行 rolling 部署",
			Category:    "deploy",
			Tags:        []string{"k8s", "tke", "ack", "matrix", "multi-cloud"},
			SpecRaw:     multiCloudYAML,
		},
	}
}

// 五个内置模板的 YAML — 都用 spec/04 § 4 的语法, 跑 dsl.ValidateRaw 必过。
const nodeDockerK8sYAML = `version: "1"
name: "node-docker-k8s"
description: "Node.js 项目: 装依赖 / 测试 / 构镜像 / 审批后部署到 K8s"

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

  - id: deploy-staging
    name: "Staging 部署"
    needs: [build-image]
    uses: "helios/k8s-deploy@v1"
    with:
      cluster: staging-k8s
      namespace: app
      image: "${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}"
      strategy: rolling

  - id: approval
    name: "生产部署审批"
    needs: [deploy-staging]
    type: approval
    approvers: [admin, ops]
    mode: any
    timeout: 24h

  - id: deploy-prod
    name: "生产 Rolling 部署"
    needs: [approval]
    uses: "helios/k8s-deploy@v1"
    with:
      cluster: prod-k8s
      namespace: app
      image: "${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}"
      strategy: rolling
`

const goMultiPlatformYAML = `version: "1"
name: "go-multi-platform"
description: "Go 项目: 测试 + 多平台二进制 + GitHub Release"

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
      os: [linux, darwin, windows]
      arch: [amd64, arm64]
    steps:
      - run: |
          ext=""
          if [ "${{ matrix.os }}" = "windows" ]; then ext=".exe"; fi
          GOOS=${{ matrix.os }} GOARCH=${{ matrix.arch }} \
            go build -o bin/app-${{ matrix.os }}-${{ matrix.arch }}${ext} ./cmd/app
      - uses: "helios/upload-artifact@v1"
        with:
          name: bin-${{ matrix.os }}-${{ matrix.arch }}
          path: bin/*

  - id: release
    name: "GitHub Release"
    needs: [build]
    runs-on: { type: container, image: "golang:1.22" }
    steps:
      - uses: "helios/download-artifact@v1"
        with:
          name: bin-linux-amd64
          path: release/
      - run: |
          gh release upload ${{ github.ref_name }} release/* \
            --repo ${{ github.repo_url }} \
            --clobber
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
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

const pythonPyPIYAML = `version: "1"
name: "python-pypi"
description: "Python 项目: pytest + PyPI 发布"

triggers:
  - on: push
    tags: ["v*"]

stages:
  - id: test
    name: "单元测试"
    runs-on: { type: container, image: "python:3.12-slim" }
    steps:
      - run: |
          pip install -r requirements-dev.txt
          pytest -v --cov=.

  - id: publish
    name: "发布到 PyPI"
    needs: [test]
    runs-on: { type: container, image: "python:3.12-slim" }
    steps:
      - run: |
          pip install build twine
          python -m build
          twine upload dist/* -u __token__ -p ${{ secrets.PYPI_TOKEN }}
`

const multiCloudYAML = `version: "1"
name: "multi-cloud-tke-ack"
description: "多云矩阵: TKE + ACK 并行 rolling 部署"

triggers:
  - on: push
    branches: [main]

env:
  REGISTRY: ccr.example.com/acme
  IMAGE_NAME: app

stages:
  - id: build-image
    name: "构建镜像"
    runs-on: { type: container, image: "gcr.io/kaniko-project/executor:latest" }
    steps:
      - run: |
          /kaniko/executor \
            --dockerfile=Dockerfile \
            --destination=${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}

  - id: deploy
    name: "多云矩阵部署"
    needs: [build-image]
    matrix:
      cluster: [prod-tke, prod-ack]
    uses: "helios/k8s-deploy@v1"
    with:
      cluster: ${{ matrix.cluster }}
      namespace: app
      image: "${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}"
      strategy: rolling
`
