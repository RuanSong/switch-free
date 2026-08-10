# Switch Free - 构建 / 打包 / 发布
#
# 用法:
#   make build          # 构建本机 macOS 桌面版（bin/switch-free）
#   make package        # 打包 .app（含图标 + plist，macOS）
#   make dmg            # 打包 macOS DMG 安装镜像（含 .app + Applications 快捷方式）
#   make windows        # 交叉编译 Windows .exe（含图标嵌入）
#   make nsis           # 打包 Windows NSIS 安装程序（需 makensis）
#   make dist           # 一键打包所有平台安装包（DMG + NSIS）
#   make build-server   # 构建本机 server 版（无 GUI，bin/switch-free-server）
#   make build-binaries # 构建全平台裸二进制到 dist/（自动更新用）
#   make test           # 运行 Go 测试
#   make fmt            # 格式化 Go 代码
#   make version        # 显示当前版本号
#   make tag            # 打 git tag（如 make tag v=0.1.0）
#   make release        # 创建 GitHub Release
#   make upload         # 上传 dist/ 构建产物到 GitHub Release
#   make push           # 推送代码 + tag 到远程
#   make deploy         # 构建 + 推码 + 打 tag + 发 release + 上传产物（一键发布）
#   make clean          # 清理产物

# 仓库信息
REPO        ?= RuanSong/switch-free
# 版本号（默认从 build/config.yml 读）
V           ?= $(shell grep -oE 'version: "[0-9]+\.[0-9]+\.[0-9]+"' build/config.yml | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
TAG         ?= v$(V)
# 二进制名
APP         := switch-free
# 产物目录
DIST        := dist
# 架构
ARCH        ?= amd64

.PHONY: build package dmg windows nsis dist build-server build-frontend build-binaries build-darwin-arm64 build-darwin-amd64 build-windows-amd64 test fmt version tag release upload push deploy clean sync-version

## 同步版本号：build/config.yml -> version/config.yml（version 包 embed 读取的唯一来源）
sync-version:
	@perl -i -pe 's/version: "[0-9]+\.[0-9]+\.[0-9]+"/version: "$(V)"/' version/config.yml
	@echo "✅ 同步版本号 $(V) -> version/config.yml"

## 构建本机 macOS 桌面版（wails3 完整 GUI）
build: sync-version
	wails3 build
	@echo "✅ 构建完成: bin/$(APP)"

## 打包 .app（含图标 + plist + codesign，仅 macOS）
package: build
	@mkdir -p bin/$(APP).app/Contents/MacOS
	@mkdir -p bin/$(APP).app/Contents/Resources
	@cp build/darwin/icons.icns bin/$(APP).app/Contents/Resources/
	@if [ -f build/darwin/Assets.car ]; then cp build/darwin/Assets.car bin/$(APP).app/Contents/Resources/; fi
	@cp bin/$(APP) bin/$(APP).app/Contents/MacOS/
	@cp build/darwin/Info.plist bin/$(APP).app/Contents/
	@codesign --force --deep --sign - bin/$(APP).app
	@echo "✅ 打包完成: bin/$(APP).app"

## 打包 macOS DMG 安装镜像
dmg: package
	@mkdir -p $(DIST)
	@rm -f $(DIST)/$(APP)-darwin-$(ARCH).dmg
	@# 清理上次失败可能残留的挂载与临时 DMG（hdiutil create 不覆盖已存在文件）
	@hdiutil detach "/Volumes/Switch Free" 2>/dev/null || true
	@hdiutil detach "/Volumes/Switch Free 1" 2>/dev/null || true
	@rm -f /tmp/switch-free_rw.dmg
	@# 创建可写 DMG
	hdiutil create -size 50m -volname "Switch Free" \
		-fs HFS+ -fsargs "-c c=64,a=16,e=16" /tmp/switch-free_rw.dmg
	@# 挂载
	hdiutil attach /tmp/switch-free_rw.dmg -readwrite -noverify -noautoopen
	@# 复制 .app + Applications 快捷方式 + 卷图标
	cp -R bin/$(APP).app "/Volumes/Switch Free/Switch Free.app"
	ln -sf /Applications "/Volumes/Switch Free/Applications"
	cp build/darwin/icons.icns "/Volumes/Switch Free/.VolumeIcon.icns"
	SetFile -c 'icnC' "/Volumes/Switch Free/.VolumeIcon.icns"
	SetFile -a C "/Volumes/Switch Free"
	@# 设置窗口布局（图标 96px，左侧 .app 右侧 Applications）
	osascript -e 'tell application "Finder"' \
		-e 'set dmg to disk "Switch Free"' \
		-e 'open dmg' \
		-e 'set dmgWin to container window of dmg' \
		-e 'set current view of dmgWin to icon view' \
		-e 'set icon size of icon view options of dmgWin to 96' \
		-e 'set arrangement of icon view options of dmgWin to not arranged' \
		-e 'set label position of icon view options of dmgWin to bottom' \
		-e 'set bounds of dmgWin to {100, 100, 620, 460}' \
		-e 'set position of item "Switch Free.app" of dmgWin to {175, 190}' \
		-e 'set position of item "Applications" of dmgWin to {435, 190}' \
		-e 'close dmgWin' \
		-e 'end tell'
	@sync
	@sleep 2
	@# 卸载并压缩为只读 DMG
	hdiutil detach "/Volumes/Switch Free"
	hdiutil convert /tmp/switch-free_rw.dmg -format UDZO -o $(DIST)/$(APP)-darwin-$(ARCH).dmg
	@rm -f /tmp/switch-free_rw.dmg
	@echo "✅ DMG 打包完成: $(DIST)/$(APP)-darwin-$(ARCH).dmg"

## 交叉编译 Windows .exe（含图标嵌入）
windows: sync-version
	@mkdir -p bin
	@# 生成 syso（将图标嵌入 .exe）
	wails3 generate syso -arch $(ARCH) \
		-icon build/windows/icon.ico \
		-manifest build/windows/wails.exe.manifest \
		-info build/windows/info.json \
		-out wails_windows_$(ARCH).syso
	@# 交叉编译
	GOOS=windows CGO_ENABLED=0 GOARCH=$(ARCH) \
		go build -tags production -trimpath -buildvcs=false \
		-ldflags="-w -s -H windowsgui" -o bin/$(APP).exe .
	@rm -f wails_windows_$(ARCH).syso
	@echo "✅ Windows 构建完成: bin/$(APP).exe"

## 打包 Windows NSIS 安装程序（需 makensis）
nsis: windows
	@mkdir -p $(DIST)
	@# 生成 WebView2 引导程序
	wails3 generate webview2bootstrapper -dir build/windows/nsis
	@# 构建 NSIS 安装包
	makensis -DARG_WAILS_$(shell echo $(ARCH) | tr 'a-z' 'A-Z')_BINARY="$(shell pwd)/bin/$(APP).exe" \
		"$(shell pwd)/build/windows/nsis/project.nsi"
	@# 复制到 dist
	cp bin/$(APP)-$(ARCH)-installer.exe $(DIST)/$(APP)-windows-$(ARCH)-installer.exe
	@echo "✅ NSIS 安装包完成: $(DIST)/$(APP)-windows-$(ARCH)-installer.exe"

## 一键打包所有平台安装包（DMG + NSIS）
dist: dmg nsis
	@echo ""
	@echo "🎉 全部安装包打包完成:"
	@ls -lh $(DIST)/*

## 构建本机 server 版（无 GUI 纯 HTTP 代理）
build-server: sync-version
	wails3 task build:server
	@echo "✅ server 构建完成: bin/$(APP)-server"

## ===== 裸二进制构建（自动更新用，产物名须匹配 updater/github.go 的 assetName()）=====

## 构建前端（多平台/多架构共用，避免重复构建）
build-frontend:
	cd frontend && npm run build
	@echo "✅ 前端构建完成"

## 构建 macOS arm64 裸二进制
build-darwin-arm64: sync-version build-frontend
	@mkdir -p $(DIST)
	GOOS=darwin CGO_ENABLED=1 GOARCH=arm64 \
		CGO_CFLAGS="-mmacosx-version-min=10.15" \
		CGO_LDFLAGS="-mmacosx-version-min=10.15" \
		MACOSX_DEPLOYMENT_TARGET="10.15" \
		go build -tags production -trimpath -buildvcs=false \
		-ldflags="-w -s" -o $(DIST)/switch-free-darwin-arm64 .
	@echo "✅ macOS arm64: $(DIST)/switch-free-darwin-arm64"

## 构建 macOS amd64 裸二进制
build-darwin-amd64: sync-version build-frontend
	@mkdir -p $(DIST)
	GOOS=darwin CGO_ENABLED=1 GOARCH=amd64 \
		CGO_CFLAGS="-mmacosx-version-min=10.15" \
		CGO_LDFLAGS="-mmacosx-version-min=10.15" \
		MACOSX_DEPLOYMENT_TARGET="10.15" \
		go build -tags production -trimpath -buildvcs=false \
		-ldflags="-w -s" -o $(DIST)/switch-free-darwin-amd64 .
	@echo "✅ macOS amd64: $(DIST)/switch-free-darwin-amd64"

## 交叉编译 Windows amd64 裸二进制（CGO_ENABLED=0，含图标嵌入）
build-windows-amd64: sync-version build-frontend
	@mkdir -p $(DIST)
	wails3 generate syso -arch amd64 \
		-icon build/windows/icon.ico \
		-manifest build/windows/wails.exe.manifest \
		-info build/windows/info.json \
		-out wails_windows_amd64.syso
	GOOS=windows CGO_ENABLED=0 GOARCH=amd64 \
		go build -tags production -trimpath -buildvcs=false \
		-ldflags="-w -s -H windowsgui" -o $(DIST)/switch-free-windows-amd64.exe .
	@rm -f wails_windows_amd64.syso
	@echo "✅ Windows amd64: $(DIST)/switch-free-windows-amd64.exe"

## 构建全平台裸二进制到 dist/（自动更新检测的资产）
build-binaries: build-darwin-arm64 build-darwin-amd64 build-windows-amd64
	@echo ""
	@echo "🎉 全部裸二进制构建完成:"
	@ls -lh $(DIST)/switch-free-*

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

## 打 git tag（已存在则跳过）
tag:
	@test -n "$(V)" || (echo "❌ 版本号为空" && exit 1)
	@if git rev-parse $(TAG) >/dev/null 2>&1; then \
		echo "ℹ️ tag $(TAG) 已存在，跳过"; \
	else \
		git tag -a $(TAG) -m "Release $(TAG)"; \
		echo "✅ 已打 tag: $(TAG)"; \
	fi

## 创建 GitHub Release
release:
	@test -n "$(V)" || (echo "❌ 版本号为空" && exit 1)
	@test -n "$$(gh auth status 2>&1 | grep -o 'Logged in')" || (echo "❌ 请先运行 gh auth login" && exit 1)
	@echo "🔄 创建 Release $(TAG) 到 $(REPO)..."
	gh release create $(TAG) \
		--repo $(REPO) \
		--title "Switch Free $(TAG)" \
		--notes "Switch Free $(TAG)" \
		|| echo "⚠️ Release 可能已存在，尝试 upload 资产"
	@echo "✅ Release $(TAG) 已创建"

## 上传 dist/ 产物到 GitHub Release：
##   - 裸二进制（自动更新用，名称须匹配 updater/github.go 的 assetName()，必须存在）
##   - 安装包 DMG/NSIS（用户手动下载用，存在则上传，缺失跳过并提示）
upload:
	@test -n "$(V)" || (echo "❌ 版本号为空" && exit 1)
	@test -n "$$(gh auth status 2>&1 | grep -o 'Logged in')" || (echo "❌ 请先运行 gh auth login" && exit 1)
	@echo "🔄 上传 dist/ 产物到 Release $(TAG)..."
	@required="$(DIST)/switch-free-darwin-arm64 $(DIST)/switch-free-darwin-amd64 $(DIST)/switch-free-windows-amd64.exe"; \
	missing=""; \
	for f in $$required; do [ -f "$$f" ] || missing="$$missing $$f"; done; \
	if [ -n "$$missing" ]; then echo "❌ 缺少裸二进制:$$missing（先 make build-binaries）"; exit 1; fi; \
	optional="$(DIST)/switch-free-darwin-amd64.dmg $(DIST)/switch-free-windows-amd64-installer.exe"; \
	toUpload="$$required"; \
	for f in $$optional; do [ -f "$$f" ] && toUpload="$$toUpload $$f" || echo "ℹ️ 跳过可选安装包（不存在）: $$f"; done; \
	gh release upload $(TAG) $$toUpload --repo $(REPO) --clobber; \
	echo "✅ 已上传:$$toUpload"

## 推送代码 + tag 到远程
push:
	git push origin main
	@echo "✅ 代码已推送"
	git push origin $(TAG) 2>/dev/null || echo "ℹ️ tag $(TAG) 已存在或无新 tag 可推"

## 一键发布：构建裸二进制 + 安装包（DMG/NSIS）-> 推码 -> 打 tag -> 发 release -> 上传全部产物
deploy: build-binaries dist push tag release upload
	@echo "🎉 发布流程完成: https://github.com/$(REPO)/releases/tag/$(TAG)"

## 清理产物
clean:
	rm -rf bin $(DIST)
	rm -f $(APP)
	@echo "✅ 已清理"