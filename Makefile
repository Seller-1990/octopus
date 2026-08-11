# Octopus 构建封装
#
# 根因（2026-08-11）：static/out/ 被 .gitignore 忽略（不进 git），裸 `go build`
# 会内嵌本地 static/out 的旧内容——前端代码改了但没重新 `pnpm build` 时，
# 编译出的二进制携带过期前端，部署到 NAS 后 UI 不更新。
#
# `make build` / `make deploy` 强制按序执行「前端构建 → 同步 static/out → go build」，
# 避免漏掉前端构建步骤。

.PHONY: build frontend build-go test deploy

# 默认目标：完整构建（前端 + 后端），产物 build/bin/octopus
build: frontend build-go

# 仅重建前端并同步到 static/out（Go embed 的目标目录）
frontend:
	cd web && pnpm install --frozen-lockfile
	cd web && CI=true pnpm build
	rm -rf static/out
	mv web/out static/out
	@echo "frontend -> static/out synced"

# 仅编译当前平台二进制（内嵌最新 static/out）
build-go:
	mkdir -p build/bin
	go build -o build/bin/octopus .

# 全量测试
test:
	go test ./...

# 交叉编译 Linux amd64（NAS 部署用；用法：make deploy VERSION=v1.4.0 COMMIT=<sha>）
deploy:
	@test -n "$(VERSION)" || (echo "usage: make deploy VERSION=v1.4.0 [COMMIT=<sha>]"; exit 1)
	rm -rf static/out
	cd web && pnpm install --frozen-lockfile
	cd web && CI=true pnpm build
	mv web/out static/out
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/octopus-linux-amd64 \
		-ldflags="-X 'github.com/bestruirui/octopus/internal/conf.Version=$(VERSION)' \
		          -X 'github.com/bestruirui/octopus/internal/conf.Commit=$(COMMIT)' \
		          -s -w" .
	@echo "deploy binary: build/bin/octopus-linux-amd64 (frontend embedded)"
