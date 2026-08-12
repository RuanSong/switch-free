package main

import (
	"embed"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"switchfree/config"
	"switchfree/creds"
	"switchfree/freeapi"
	"switchfree/pricing"
	"switchfree/proxy"
	"switchfree/service"
	"switchfree/updater"
	"switchfree/upstream"
	"switchfree/version"
)

// 免费 API 目录（内置，运行时从 GitHub 拉取最新覆盖）
//
//go:embed data/free_apis_catalog.json
var embedFreeCatalog []byte

// Wails 用 embed 包把前端文件嵌入二进制
//
//go:embed all:frontend/dist
var assets embed.FS

// 托盘图标：macOS 用黑白模板（自动适配深浅色菜单栏），其他平台用彩色
//
//go:embed build/tray-icon.png
var trayIconColor []byte

//go:embed build/tray-template.png
var trayIconTemplate []byte

// realQuit 标记：只有托盘「退出」菜单触发时为 true，允许真正退出；
// Dock 右键退出 / Cmd+Q 走 ShouldQuit 钩子时为 false，改为隐藏窗口
var realQuit atomic.Bool

// 全局窗口引用（供托盘菜单使用）
var mainWindow *application.WebviewWindow

// latestUpdate 最新可用更新信息（nil 表示无更新或尚未检查）
// 由 startUpdateCheck 写入，托盘菜单读取显示
var latestUpdate atomic.Pointer[updater.UpdateInfo]

// 全局托盘引用（供动态刷新菜单用）
var systemTray *application.SystemTray

// 重建托盘菜单的回调（setupSystray 内设置；startUpdateCheck 等地方发现更新时调用）
var rebuildTrayMenu func()

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

	// 5.5 免费 API（独立文件 + 目录 + 健康监控）
	freeAPIMgr, err := freeapi.NewManager("")
	if err != nil {
		log.Printf("⚠️ 免费 API 配置加载失败: %v", err)
	}
	freeapi.SetEmbedCatalog(embedFreeCatalog)
	freeCatalogLoader := &freeapi.CatalogLoader{}
	_ = freeCatalogLoader.LoadCatalog() // 预加载（embed/GitHub/缓存）
	freeMonitor := freeapi.NewMonitor(freeAPIMgr, core)
	// 注入健康查询回调（供 proxy 权重降级用）
	proxy.FreeModelHealth = func(providerID, modelID string) bool {
		return freeMonitor.IsHealthy(providerID, modelID)
	}
	// 注册免费 API 刷新回调：provider 增删/模型变化时重建上游 + 模型列表
	registerFreeAPIRefresh(server, freeAPIMgr, freeMonitor, core)

	// 6. Wails 服务（暴露给前端）
	freeAPISvc := service.NewFreeAPIService(freeAPIMgr, freeCatalogLoader, freeMonitor, core)
	service.SetFreeRefreshCallback(rebuildFreeAPIs)
	proxySvc := service.NewProxyService(core)
	credsSvc := service.NewCredsService(core)
	modelSvc := service.NewModelService(core)
	logSvc := service.NewLogService(core)
	cfgSvc := service.NewConfigServiceWithCore(cfgMgr, core)
	pricingSvc := service.NewPricingService(pricingMgr)
	updaterMgr := updater.NewUpdater(cfgMgr.Get())
	updaterSvc := service.NewUpdaterService(updaterMgr)
	benchmarkSvc := service.NewBenchmarkService(core)

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
			application.NewService(benchmarkSvc),
			application.NewService(freeAPISvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// 拦截退出请求：仅托盘「退出」(realQuit=true) 放行；
		// Dock 右键退出 / Cmd+Q 改为隐藏窗口到托盘
		ShouldQuit: func() bool {
			if realQuit.Load() {
				return true
			}
			if mainWindow != nil {
				mainWindow.Hide()
			}
			return false
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
	// 注意：托盘「退出」时 realQuit=true，必须放行（不能 Cancel），否则 cleanup 里
	// window.Close() 被本 hook 取消，窗口/webview 无法销毁，导致退出卡死
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if realQuit.Load() {
			return // 真正退出，让 Wails 正常销毁窗口
		}
		if mainWindow != nil {
			mainWindow.Hide()
		}
		event.Cancel() // 非退出场景：取消真正关闭，窗口仅隐藏
	})

	// 拦截最小化：改为隐藏到托盘（Windows/Linux 任务栏不再占位，macOS 不缩到 Dock）
	mainWindow.OnWindowEvent(events.Common.WindowMinimise, func(event *application.WindowEvent) {
		if mainWindow != nil {
			mainWindow.Hide()
		}
	})

	// 8. 系统托盘
	setupSystray(app, server, cfgMgr, updaterSvc)

	// 9. 启动代理 + 凭据校验（非致命：失败仅警告，等待客户端登录后自动恢复）
	go startProxyAndCreds(server, jyUp, deUp, ocUp, wbUp, core)

	// 10. 后台定期预检凭据
	go startBackgroundVerify(jyUp, deUp, ocUp, wbUp, core)

	// 10.5 后台周期探测 agent 安装状态（新安装工具时自动校验凭据并推送，无需重启）
	go core.WatchInstalledAgents(app.Context(), 5*time.Second)

	// 10.6 免费 API 模型健康监控（每 5 分钟探测；只监控 free 模型）
	go freeMonitor.Start(app.Context())
	// 首次刷新注册免费上游 + 模型（启动时构建）
	registerFreeAPIRefresh(server, freeAPIMgr, freeMonitor, core)

	// 10.5 启动 3s 后后台检查更新（有新版本发事件给前端弹窗）
	go startUpdateCheck(updaterSvc)

	// 11. 运行
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// startUpdateCheck 启动后延迟 3s 首次检查更新，之后每 6 小时周期检查。
// 发现新版本时推送 update:available 事件给前端（前端按 critical 决定是否可忽略）。
func startUpdateCheck(updaterSvc *service.UpdaterService) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	check := func() {
		info, err := updaterSvc.CheckUpdate()
		if err != nil {
			log.Printf("⚠️ 检查更新失败: %v", err)
			return
		}
		if info != nil {
			kind := "普通更新"
			if info.Critical {
				kind = "强制更新"
			}
			log.Printf("发现新版本 %s（当前 %s，%s）", info.Version, updaterSvc.GetCurrentVersion(), kind)
			latestUpdate.Store(info)
			updaterSvc.EmitUpdateAvailable(info)
			// 刷新托盘菜单（显示"有新版本"）
			if rebuildTrayMenu != nil {
				rebuildTrayMenu()
			}
		}
	}

	time.Sleep(3 * time.Second)
	check()
	for range ticker.C {
		check()
	}
}

// setupSystray 配置系统托盘（跨平台）
// - 单击/双击托盘图标：显示/聚焦主窗口
// - 右键菜单：版本号、服务状态+启停、方案切换、检查更新、打开面板、退出
func setupSystray(app *application.App, server *proxy.Server, cfgMgr *config.Manager, updaterSvc *service.UpdaterService) *application.SystemTray {
	tray := app.SystemTray.New()
	systemTray = tray // 存全局引用，供动态刷新用
	// macOS 用模板图（透明背景 + 黑色主体），系统按菜单栏明暗自动反色；
	// 其他平台用彩色图标
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIconTemplate)
	} else {
		tray.SetIcon(trayIconColor)
	}
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

	// 构建菜单（首次）
	menu := buildTrayMenu(server, cfgMgr, updaterSvc, showWindow, app)
	tray.SetMenu(menu)

	// 重建菜单的公共回调
	rebuild := func() {
		newMenu := buildTrayMenu(server, cfgMgr, updaterSvc, showWindow, app)
		tray.SetMenu(newMenu)
	}
	rebuildTrayMenu = rebuild

	// 配置变化时重建菜单（方案增删改、激活态变化等）
	cfgMgr.SetOnChange(func() {
		// SaveConfig 在哪个 goroutine 触发就在哪个 goroutine 调用；
		// SetMenu 内部用 InvokeSync 切主线程，所以这里可以直接调
		rebuild()
	})

	// 服务状态轮询：每秒检查一次，变化时更新状态菜单项
	// （Wails 没有"菜单即将打开"的回调，用轮询近似实现"打开时最新"的效果）
	go func() {
		lastRunning := server.IsRunning()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			nowRunning := server.IsRunning()
			if nowRunning == lastRunning {
				continue
			}
			lastRunning = nowRunning
			// 只更新状态项，不重建整个菜单（减少闪烁 + 避免关闭中的子菜单状态丢失）
			// 通过重建整份菜单来保证一致性（状态项位置索引可能随方案变化而变）
			rebuild()
		}
	}()

	// 单击/双击托盘图标打开面板（macOS 单击、Windows/Linux 通常双击）
	tray.OnClick(func() {
		showWindow()
	})
	tray.OnDoubleClick(func() {
		showWindow()
	})

	return tray
}

// buildTrayMenu 构建托盘右键菜单
// 菜单项：版本号 / 服务状态（带图标，点击切换启停） / 方案 / 检查更新 / 打开面板 / 退出
func buildTrayMenu(server *proxy.Server, cfgMgr *config.Manager, updaterSvc *service.UpdaterService, showWindow func(), app *application.App) *application.Menu {
	menu := application.NewMenu()
	running := server.IsRunning()

	// ── 版本号（禁用，仅展示） ──
	menu.Add("Switch Free v"+version.GetVersion()).SetEnabled(false)

	menu.AddSeparator()

	// ── GitHub Star 引导 ──
	menu.Add("⭐ 去 GitHub 点个 Star").OnClick(func(*application.Context) {
		_ = app.Browser.OpenURL("https://github.com/RuanSong/switch-free")
	})

	menu.AddSeparator()

	// ── 服务状态（纯展示，禁用；每次打开菜单时由 buildTrayMenu 读取最新状态） ──
	statusLabel := "服务：已停止"
	if running {
		statusLabel = "服务：运行中"
	}
	menu.Add(statusLabel).SetEnabled(false)

	menu.AddSeparator()

	// ── 方案切换 ──
	cfg := cfgMgr.Get()
	if len(cfg.Presets) > 0 {
		// 有方案：显示子菜单（radio 形式，激活的打勾）
		presetMenu := menu.AddSubmenu("方案")
		for _, p := range cfg.Presets {
			name := p.Name
			checked := cfg.ActivePreset == name
			presetMenu.AddRadio(name, checked).OnClick(func(*application.Context) {
				// 应用方案；成功后会触发 onChange 回调重建菜单
				_ = cfgMgr.ApplyPreset(name)
			})
		}
	} else {
		// 无方案：显示"保存方案..."入口，点击跳设置页
		menu.Add("保存方案...").OnClick(func(*application.Context) {
			showWindow()
			app.Event.Emit("navigate:settings", nil)
		})
	}

	menu.AddSeparator()

	// ── 打开面板 ──
	menu.Add("打开面板").OnClick(func(*application.Context) {
		showWindow()
	})

	menu.AddSeparator()

	// ── 检查更新 / 有新版本 ──
	updateInfo := latestUpdate.Load()
	if updateInfo != nil {
		// 有新版本：显示带箭头的提示，点击打开窗口并弹更新面板
		updateLabel := "有新版本 v" + updateInfo.Version + " ↗"
		if updateInfo.Critical {
			updateLabel = "有新版本 v" + updateInfo.Version + "（强制） ↗"
		}
		menu.Add(updateLabel).OnClick(func(*application.Context) {
			showWindow()
			// 推送 update:available 事件，前端 UpdatePanel 会弹窗
			updaterSvc.EmitUpdateAvailable(updateInfo)
		})
	} else {
		checkItem := menu.Add("检查更新")
		checkItem.OnClick(func(*application.Context) {
			// 先置为检查中状态（点击后菜单会关闭，但状态已更新，下次打开可见）
			checkItem.SetLabel("检查中...")
			checkItem.SetEnabled(false)
			// 异步检查
			go func() {
				info, err := updaterSvc.CheckUpdate()
				if err != nil {
					log.Printf("⚠️ 检查更新失败: %v", err)
					checkItem.SetLabel("检查失败")
					checkItem.SetEnabled(true)
					// 3 秒后恢复
					time.AfterFunc(3*time.Second, func() {
						checkItem.SetLabel("检查更新")
						checkItem.SetEnabled(true)
					})
					return
				}
				if info != nil {
					latestUpdate.Store(info)
					// 有新版本：重建整个菜单（显示"有新版本"入口），并弹窗口
					if rebuildTrayMenu != nil {
						rebuildTrayMenu()
					}
					showWindow()
					updaterSvc.EmitUpdateAvailable(info)
				} else {
					// 无新版本：显示"已是最新版本"，3 秒后恢复
					checkItem.SetLabel("已是最新版本")
					checkItem.SetEnabled(true)
					time.AfterFunc(3*time.Second, func() {
						checkItem.SetLabel("检查更新")
						checkItem.SetEnabled(true)
					})
				}
			}()
		})
	}

	menu.AddSeparator()

	// ── 退出 ──
	menu.Add("退出").OnClick(func(*application.Context) {
		// 异步执行退出：macOS 托盘菜单回调在主线程，同步调用 app.Quit() 会
		// 触发 NSApp.terminate，而 terminate 需要 runloop 迭代才能完成，
		// 此时主线程卡在菜单回调未返回 -> 死锁卡死。放到 goroutine 让回调立即返回。
		go func() {
			_ = server.StopQuiet() // 先关端口服务（不推事件，避免 Event.Emit 在退出时卡死）
			realQuit.Store(true)   // 标记允许真正退出（ShouldQuit 放行）
			app.Quit()
		}()
	})

	return menu
}

// rebuildFreeAPIs 重建免费上游 + 模型列表（provider 增删/模型变化时调用，包级供 FreeAPIService 用）
var rebuildFreeAPIs func()

// registerFreeAPIRefresh 初始化免费 API 上游注册：
// 遍历 free_apis.json 里 verified 的 provider，为每个创建 FreeAPIUpstream 注册到 server，
// 并填充 proxy.FreeModels 供模型列表/降级链使用。
func registerFreeAPIRefresh(server *proxy.Server, mgr *freeapi.Manager, monitor *freeapi.Monitor, core *service.Core) {
	rebuild := func() {
		// 清空旧的免费上游
		for pid := range server.GetFreeAPIs() {
			server.RemoveFreeAPI(pid)
		}
		// 重建模型列表 + 上游
		var freeModels []proxy.FreeModel
		for pid, p := range mgr.GetProviders() {
			if !p.Verified {
				continue
			}
			// 为 provider 创建上游（闭包捕获 baseURL/apiKey，保证 rebuild 时读到最新）
			baseURL, apiKey := p.BaseURL, p.APIKey
			up := upstream.NewFreeAPIUpstream(pid, func() string { return baseURL }, func() string { return apiKey })
			up.SetDisplayName(p.Name)
			server.RegisterFreeAPI(pid, up)

			// 收集 verified 模型
			for _, mo := range p.Models {
				if !mo.Verified {
					continue
				}
				freeModels = append(freeModels, proxy.FreeModel{
					InternalID: p.InternalID(mo.ID),
					ProviderID: pid,
					ModelID:    mo.ID,
					Label:      mo.ID,
					Context:    mo.Context,
				})
			}
		}
		proxy.SetFreeModels(freeModels)

		// 推送状态刷新（前端凭据页/模型页）
		if core != nil {
			core.EmitEvent("cred:change", core.GetCredStatus())
			core.EmitEvent("freeapi:change", mgr.GetProviders())
		}
	}
	rebuildFreeAPIs = rebuild

	// 首次构建
	rebuild()
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
