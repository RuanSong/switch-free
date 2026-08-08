package main

import (
	"embed"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"switchfree/config"
	"switchfree/creds"
	"switchfree/pricing"
	"switchfree/proxy"
	"switchfree/service"
	"switchfree/updater"
	"switchfree/upstream"
)

// Wails 用 embed 包把前端文件嵌入二进制
//
//go:embed all:frontend/dist
var assets embed.FS

// 托盘图标（32px，方案3 中转节点）
//
//go:embed build/tray-icon.png
var trayIcon []byte

// 全局窗口引用（供托盘菜单使用）
var mainWindow *application.WebviewWindow

func main() {
	// 1. 凭据管理器
	jyMgr := creds.NewJoyCodeCredManager(creds.DefaultJoyCodeConfig())
	deMgr := creds.NewDevEcoCredManager(creds.DefaultDevEcoConfig())
	ocMgr := creds.NewOpenCodeCredManager(creds.DefaultOpenCodeConfig())
	wbMgr := creds.NewWorkBuddyCredManager(creds.DefaultWorkBuddyConfig())

	// 2. 上游适配器
	jyUp := upstream.NewJoyCodeUpstream(jyMgr)
	deUp := upstream.NewDevEcoUpstream(deMgr)
	ocUp := upstream.NewOpenCodeUpstream(ocMgr)
	wbUp := upstream.NewWorkBuddyUpstream(wbMgr)

	// 3. 配置管理器
	cfgMgr, err := config.NewManager("")
	if err != nil {
		log.Printf("⚠️ 配置加载失败: %v，使用默认配置", err)
	}

	// 4. 代理服务（端口从配置读取）
	serverPort := cfgMgr.Get().Port
	if serverPort == 0 {
		serverPort = config.DefaultPort
	}
	server := proxy.NewServer(jyUp, deUp, ocUp, wbUp, "127.0.0.1", serverPort)
	server.ConfigResolver = cfgMgr // 注入配置解析器

	// 4.5 费率管理器（本地无费率文件时从内置硬编码还原）
	pricingMgr := pricing.NewManager()
	if imported, err := pricingMgr.Load(); err != nil {
		log.Printf("⚠️ 费率库加载: %v", err)
	} else if imported {
		log.Printf("✓ 已从内置费率还原 %d 条到自有库", pricingMgr.Count())
	}
	server.Pricing = pricingMgr

	// 5. Core（共享状态）
	core := service.NewCore()
	core.Setup(jyMgr, deMgr, ocMgr, wbMgr, jyUp, deUp, ocUp, wbUp, server)

	// 6. Wails 服务（暴露给前端）
	proxySvc := service.NewProxyService(core)
	credsSvc := service.NewCredsService(core)
	modelSvc := service.NewModelService(core)
	logSvc := service.NewLogService(core)
	cfgSvc := service.NewConfigServiceWithCore(cfgMgr, core)
	pricingSvc := service.NewPricingService(pricingMgr)
	updaterMgr := updater.NewUpdater(cfgMgr.Get())
	updaterSvc := service.NewUpdaterService(updaterMgr)

	// 7. 创建 Wails 应用
	app := application.New(application.Options{
		Name:        "Switch Free",
		Description: "多上游 AI 模型代理管理器",
		Services: []application.Service{
			application.NewService(proxySvc),
			application.NewService(credsSvc),
			application.NewService(modelSvc),
			application.NewService(logSvc),
			application.NewService(cfgSvc),
			application.NewService(pricingSvc),
			application.NewService(updaterSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false, // 关窗口留托盘
		},
	})

	// 7. 主窗口
	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Switch Free",
		Width:            960,
		Height:           680,
		MinWidth:         800,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(15, 23, 42), // 深色背景
		URL:              "/",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	})

	// 拦截窗口关闭：改为隐藏到托盘（不真正退出）
	// 跨平台：macOS 关红叉、Windows/Linux 关闭按钮都触发 WindowClosing
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if mainWindow != nil {
			mainWindow.Hide()
		}
		event.Cancel() // 取消真正的关闭，窗口仅隐藏
	})

	// 拦截最小化：改为隐藏到托盘（Windows/Linux 任务栏不再占位，macOS 不缩到 Dock）
	mainWindow.OnWindowEvent(events.Common.WindowMinimise, func(event *application.WindowEvent) {
		if mainWindow != nil {
			mainWindow.Hide()
		}
	})

	// 8. 系统托盘
	setupSystray(app, server)

	// 9. 启动代理 + 凭据校验（非致命：失败仅警告，等待客户端登录后自动恢复）
	go startProxyAndCreds(server, jyUp, deUp, ocUp, wbUp, core)

	// 10. 后台定期预检凭据
	go startBackgroundVerify(jyUp, deUp, ocUp, wbUp, core)

	// 10.5 启动 3s 后后台检查更新（有新版本发事件给前端弹窗）
	go startUpdateCheck(updaterSvc)

	// 11. 运行
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// startUpdateCheck 启动后延迟 3s 后台检查更新，有新版本推送 update:available 事件
func startUpdateCheck(updaterSvc *service.UpdaterService) {
	time.Sleep(3 * time.Second)
	info, err := updaterSvc.CheckUpdate()
	if err != nil {
		log.Printf("⚠️ 检查更新失败: %v", err)
		return
	}
	if info != nil {
		log.Printf("发现新版本 %s（当前 %s）", info.Version, updaterSvc.GetCurrentVersion())
		updaterSvc.EmitUpdateAvailable(info)
	}
}

// setupSystray 配置系统托盘
// setupSystray 配置系统托盘（跨平台）
// - 单击托盘图标：显示/聚焦主窗口
// - 右键菜单：打开面板 / 退出（唯一真正退出途径）
func setupSystray(app *application.App, server *proxy.Server) {
	tray := app.SystemTray.New()
	// 用方案3中转节点图标（彩色）
	tray.SetIcon(trayIcon)
	tray.SetTooltip("Switch Free - 双击打开")

	// 显示并聚焦主窗口的公共方法
	showWindow := func() {
		if mainWindow == nil {
			return
		}
		mainWindow.Show()
		mainWindow.UnMinimise() // 从最小化恢复（若被系统最小化）
		mainWindow.Focus()
	}

	menu := application.NewMenu()
	menu.Add("打开面板").OnClick(func(*application.Context) {
		showWindow()
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		_ = server.Stop()
		app.Quit()
	})
	tray.SetMenu(menu)

	// 单击/双击托盘图标打开面板（macOS 单击、Windows/Linux 通常双击）
	tray.OnClick(func() {
		showWindow()
	})
	tray.OnDoubleClick(func() {
		showWindow()
	})
}

// startProxyAndCreds 启动时校验凭据（非致命）+ 启动代理
func startProxyAndCreds(server *proxy.Server, jy *upstream.JoyCodeUpstream, de *upstream.DevEcoUpstream, oc *upstream.OpenCodeUpstream, wb *upstream.WorkBuddyUpstream, core *service.Core) {
	// 启动时校验四上游凭据（失败仅警告）
	if err := jy.EnsureCreds(nil); err != nil {
		log.Printf("⚠️ JoyCode 凭据不可用: %v（JoyCode 作为 auto 降级兜底，缺失不影响 DevEco 直连）", err)
	}
	if err := de.EnsureCreds(nil); err != nil {
		log.Printf("⚠️ DevEco 凭据不可用: %v（auto 模式将降级到 JoyCode）", err)
	}
	if err := oc.EnsureCreds(nil); err != nil {
		log.Printf("⚠️ OpenCode 凭据不可用: %v（仅显式选 *-free 模型时使用）", err)
	}
	if err := wb.EnsureCreds(nil); err != nil {
		log.Printf("⚠️ WorkBuddy 凭据不可用: %v（仅显式选 wb/* 模型时使用）", err)
	}
	core.EmitEvent("cred:change", core.GetCredStatus())

	// 启动代理
	if err := server.Start(); err != nil {
		log.Printf("✗ 代理启动失败: %v", err)
		return
	}
	log.Printf("✓ 代理已启动，监听 http://127.0.0.1:%d", server.Port)
}

// startBackgroundVerify 后台定期预检凭据
func startBackgroundVerify(jy *upstream.JoyCodeUpstream, de *upstream.DevEcoUpstream, oc *upstream.OpenCodeUpstream, wb *upstream.WorkBuddyUpstream, core *service.Core) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	verify := func(u upstream.Upstream) {
		result, err := u.VerifyCreds(nil)
		if err != nil {
			return
		}
		if !result.Valid {
			u.InvalidateCreds()
			core.EmitEvent("cred:change", core.GetCredStatus())
		}
	}

	// 启动后先等一会再开始周期校验
	<-ticker.C
	for range ticker.C {
		go verify(jy)
		go verify(de)
		go verify(oc)
		go verify(wb)
	}
}
