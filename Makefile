# Switch Free - 构建 / 发布 / 推码
#
# 用法:
#   make build          # 构建本机 macOS 桌面版（bin/switch-free）
#   make build-server   # 构建本机 server 版（无 GUI，bin/switch-free-server）
#   make test           # 运行 Go 测试
#   make fmt            # 格式化 Go 代码
#   make version        # 显示当前版本号
#   make tag            # 打 git tag（如 make tag v=0.1.0）
#   make release        # 创建 GitHub Release（含全部平台资产，需 CI 先构建上传）
#   make push           # 推送代码 + tag 到远程
#   make deploy         # 推码 + 打 tag + 发 release（一键发布）
#   make clean          # 清理产物

# 仓库信息（GitHub Actions 构建时由 release 触发）
REPO        ?= RuanSong/switch-free
# 版本号（默认从 build/config.yml 读）
V           ?= $(shell grep -oE 'version: "[0-9]+\.[0-9]+\.[0-9]+"' build/config.yml | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
TAG         ?= v$(V)
# 二进制名
APP         := switch-free
# 产物目录
DIST        := dist

.PHONY: build build-server test fmt version tag release push deploy clean

## 构建本机 macOS 桌面版（wails3 完整 GUI）
build:
	wails3 build
	@echo "✅ 构建完成: bin/$(APP)"

## 构建本机 server 版（无 GUI 纯 HTTP 代理）
build-server:
	wails3 task build:server
	@echo "✅ server 构建完成: bin/$(APP)-server"

## 运行 Go 测试
test:
	go test ./...

## 格式化 Go 代码
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './frontend/node_modules/*' -not -path '*/bindings/*')
	@echo "✅ Go 代码已格式化"

## 显示当前版本号
version:
	@echo "版本: $(V) | tag: $(TAG)"

## 打 git tag
tag:
	@test -n "$(V)" || (echo "❌ 版本号为空" && exit 1)
	git tag -a $(TAG) -m "Release $(TAG)"
	@echo "✅ 已打 tag: $(TAG)"

## 创建 GitHub Release
# 说明: GitHub Actions 的 release 工作流会在 tag 触发时构建三平台二进制并上传资产。
# 本命令只创建 release 骨架（含版本说明），等 CI 完成构建后资产会自动附上。
# 如果资产未自动上传，可运行: gh release upload $(TAG) dist/*.tar.gz dist/*.exe
release:
	@test -n "$(V)" || (echo "❌ 版本号为空" && exit 1)
	@test -n "$$(gh auth status 2>&1 | grep -o 'Logged in')" || (echo "❌ 请先运行 gh auth login" && exit 1)
	@echo "🔄 创建 Release $(TAG) 到 $(REPO)..."
	gh release create $(TAG) \
		--repo $(REPO) \
		--title "Switch Free $(TAG)" \
		--notes "Switch Free $(TAG)" \
		|| echo "⚠️ Release 可能已存在，尝试 upload 资产"
	@echo "✅ Release $(TAG) 已创建（CI 构建后资产会自动上传）"

## 推送代码 + tag 到远程
push:
	git push origin main
	@echo "✅ 代码已推送"
	git push origin $(TAG) 2>/dev/null || echo "ℹ️ tag $(TAG) 已存在或无新 tag 可推"

## 一键发布：推码 + 打 tag + 发 release
deploy: push tag release
	@echo "🎉 发布流程完成: https://github.com/$(REPO)/releases/tag/$(TAG)"
	@echo "💡 等待 GitHub Actions 构建完成后，资产会自动上传到 release"

## 清理产物
clean:
	rm -rf bin $(DIST)
	rm -f $(APP)
	@echo "✅ 已清理"
