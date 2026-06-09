#!/usr/bin/env bash
# 生成 helios JWT 用的 RSA 密钥对 (RS256)。
# 仅用于开发,生产请用 KMS / Vault 管理私钥。
set -euo pipefail

DIR="${1:-.helios}"
mkdir -p "$DIR"

PRIV="$DIR/jwt-private.pem"
PUB="$DIR/jwt-public.pem"

if [[ -f "$PRIV" || -f "$PUB" ]]; then
  echo "✗ $DIR 下已存在 jwt-private.pem 或 jwt-public.pem,拒绝覆盖。"
  echo "  如要重新生成,请先手动删除。"
  exit 1
fi

echo "→ 生成 2048-bit RSA 私钥: $PRIV"
openssl genpkey -algorithm RSA -out "$PRIV" -pkeyopt rsa_keygen_bits:2048 2>/dev/null

echo "→ 派生公钥: $PUB"
openssl rsa -pubout -in "$PRIV" -out "$PUB" 2>/dev/null

chmod 600 "$PRIV"
chmod 644 "$PUB"

echo ""
echo "✓ 完成。请确认 .gitignore 包含 $DIR/"
echo "  helios api 启动时会读 HELIOS_JWT_PRIVATE_KEY_PATH / HELIOS_JWT_PUBLIC_KEY_PATH"
