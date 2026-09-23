#!/usr/bin/env bash
# build-release.sh <version> <outdir>
#
# 交叉编译全部发布平台并打包。
# 目标平台矩阵：Termux 同时提供原生 Android arm64 与 Linux arm64。
# Android 目标使用 CGO_ENABLED=0，因此 release runner 不需要 Android NDK。
#   linux:   amd64, arm64, arm(7), 386
#   darwin:  amd64, arm64
#   windows: amd64, arm64
set -euo pipefail

VERSION="${1:?用法: build-release.sh <version> <outdir>}"
OUTDIR="${2:?用法: build-release.sh <version> <outdir>}"
MODULE="github.com/RobiNexy/de-llm"
PKG="./cmd"

mkdir -p "$OUTDIR"
cd "$(dirname "$0")/.."

LDFLAGS="-s -w -X main.version=${VERSION}"

build() {
    local goos="$1" goarch="$2" ext="$3" archiver="$4"
    local name="dellm_${goos}_${goarch}"
    echo "==> ${goos}/${goarch}"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$OUTDIR/$name/dellm$ext" "$PKG"
    (cd "$OUTDIR" && $archiver "$name")
    rm -rf "$OUTDIR/$name"
}

# tar.gz：unix 平台；zip：windows。
tarball() { tar -czf "$1.tar.gz" "$1"; }
zipit()   { zip -qr "$1.zip" "$1"; }

for target in \
    "linux amd64" "linux arm64" "linux arm" "linux 386" \
    "android arm64" \
    "darwin amd64" "darwin arm64" \
    "windows amd64" "windows arm64"
do
    set -- $target
    goos="$1" goarch="$2"
    ext=""
    packer=tarball
    if [ "$goos" = "windows" ]; then
        ext=".exe"
        packer=zipit
    fi
    name="dellm_${goos}_${goarch}"
    echo "==> ${goos}/${goarch}"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$OUTDIR/$name/dellm$ext" "$PKG"
    (cd "$OUTDIR" && $packer "$name")
    rm -rf "$OUTDIR/$name"
done

echo "完成。产物："
ls -l "$OUTDIR"
