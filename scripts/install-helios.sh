#!/usr/bin/env bash
# Helios CLI 一键安装脚本 (M8 T8.3.5)
# 用法: curl -sSL https://get.helios.io | bash
# 或: curl -sSL https://raw.githubusercontent.com/helios-cicd/helios/main/scripts/install-helios.sh | bash
set -euo pipefail

REPO="helios-cicd/helios"
INSTALL_DIR="${HELIOS_INSTALL_DIR:-/usr/local/bin}"
VERSION="${HELIOS_VERSION:-latest}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

if [ "$os" = "darwin" ]; then os=darwin; fi
if [ "$os" = "linux" ]; then os=linux; fi

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/helios_${VERSION}_${os}_${arch}.tar.gz"
  # goreleaser 实际文件名含版本号; latest 重定向由 GitHub 处理
  url="https://github.com/${REPO}/releases/latest/download/helios_${os}_${arch}.tar.gz"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "→ 下载 helios (${os}/${arch})..."
if ! curl -fsSL "$url" -o "$tmp/helios.tgz"; then
  echo "下载失败。可手动从 https://github.com/${REPO}/releases 获取二进制。" >&2
  exit 1
fi

tar -xzf "$tmp/helios.tgz" -C "$tmp"
mkdir -p "$INSTALL_DIR"
install -m 755 "$tmp/helios" "$INSTALL_DIR/helios"
echo "✓ helios 已安装到 $INSTALL_DIR/helios"
helios --version || true
