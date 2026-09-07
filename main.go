package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"mcp-gateway/internal/gateway"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

//go:embed all:frontend/dist
var assets embed.FS

type GatewayService struct {
	manager *gateway.Manager
	app     *application.App
}

func (s *GatewayService) Request(ctx context.Context, method, params string) (any, error) {
	if method == "copyGatewayAddress" {
		if !s.app.Clipboard.SetText(s.manager.Address()) {
			return nil, errors.New("复制地址失败")
		}
		return nil, nil
	}
	if method == "saveSettings" {
		var settings gateway.Settings
		if err := json.Unmarshal([]byte(params), &settings); err != nil {
			return nil, err
		}
		previous := s.manager.Preferences()
		if settings.LaunchAtLogin != previous.LaunchAtLogin {
			var err error
			if settings.LaunchAtLogin {
				err = s.app.Autostart.Enable()
			} else {
				err = s.app.Autostart.Disable()
			}
			if err != nil {
				return nil, err
			}
		}
		result, err := s.manager.Request(ctx, method, params)
		if err != nil && settings.LaunchAtLogin != previous.LaunchAtLogin {
			if previous.LaunchAtLogin {
				_ = s.app.Autostart.Enable()
			} else {
				_ = s.app.Autostart.Disable()
			}
		}
		return result, err
	}
	result, err := s.manager.Request(ctx, method, params)
	if err == nil && method == "exportDiagnostics" {
		if data, ok := result.(map[string]any); ok {
			if path, ok := data["path"].(string); ok {
				// The file is already saved; failure to reveal it must not hide its path.
				_ = s.app.Browser.OpenFile(filepath.Dir(path))
			}
		}
	}
	if err == nil && method == "startOAuth" {
		if data, ok := result.(map[string]any); ok {
			opened, _ := data["browserOpened"].(bool)
			authURL, _ := data["authURL"].(string)
			if !opened && authURL != "" {
				if err := s.app.Browser.OpenURL(authURL); err != nil {
					return nil, fmt.Errorf("打开授权页面失败: %w", err)
				}
			}
			delete(data, "authURL")
			delete(data, "browserOpened")
		}
	}
	return result, err
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "connect" {
		flags := flag.NewFlagSet("connect", flag.ContinueOnError)
		clientID := flags.String("client", "", "客户端 ID")
		dir := flags.String("data-dir", "", "MCP Gateway 数据目录")
		if err := flags.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		if *dir == "" {
			base, err := os.UserConfigDir()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			*dir = filepath.Join(base, "MCP Gateway")
		}
		manager, err := gateway.New(*dir, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		if err := manager.Connect(ctx, *clientID, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	dir := os.Getenv("MCP_GATEWAY_DATA_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		dir = filepath.Join(base, "MCP Gateway")
	}
	binary := os.Getenv("MCP_GATEWAY_CORE")
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		binary = filepath.Join(filepath.Dir(exe), "mcpproxy")
	}
	manager, err := gateway.New(dir, binary)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	service := &GatewayService{manager: manager}
	var window *application.WebviewWindow
	var app *application.App
	var quitAllowed, quitting, forceQuit atomic.Bool
	show := func() {
		if window != nil {
			window.Show()
			window.Restore()
			window.Focus()
		}
	}
	quit := func(force bool) {
		if force {
			forceQuit.Store(true)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
				defer cancel()
				if err := manager.Stop(ctx, true); err != nil {
					forceQuit.Store(false)
					quitting.Store(false)
					app.Dialog.Error().SetMessage(err.Error()).Show()
					return
				}
				quitAllowed.Store(true)
				app.Quit()
			}()
			return
		}
		if !quitting.CompareAndSwap(false, true) {
			return
		}
		go func() {
			if err := manager.Drain(context.Background()); err != nil {
				if forceQuit.Load() {
					return
				}
				quitting.Store(false)
				app.Dialog.Error().SetTitle("暂时无法安全退出").SetMessage(err.Error() + "。可重试，或从菜单选择立即停止并退出。").Show()
				return
			}
			if forceQuit.Load() {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
			defer cancel()
			if err := manager.Stop(ctx, false); err != nil {
				quitting.Store(false)
				app.Dialog.Error().SetMessage(err.Error()).Show()
				return
			}
			quitAllowed.Store(true)
			app.Quit()
		}()
	}
	app = application.New(application.Options{
		Name: "MCP Gateway", Description: "个人 MCP 服务与本机网关管理器", LogLevel: slog.LevelWarn,
		Services:       []application.Service{application.NewService(service)},
		Assets:         application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Mac:            application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "com.mcp-gateway.desktop", EncryptionKey: sha256.Sum256([]byte("com.mcp-gateway.desktop.activation")), OnSecondInstanceLaunch: func(application.SecondInstanceData) { show() }},
		ShouldQuit: func() bool {
			if quitAllowed.Load() {
				return true
			}
			quit(false)
			return false
		},
		OnShutdown: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
			defer cancel()
			if err := manager.Stop(ctx, forceQuit.Load()); err != nil {
				slog.Error("停止网关失败", "error", err)
			}
		},
	})
	service.app = app
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "manager", Title: "MCP Gateway", Width: 1180, Height: 780, MinWidth: 760, MinHeight: 560, URL: "/"})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) { window.Hide(); event.Cancel() })
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) { show() })
	appMenu := app.NewMenu()
	appSub := appMenu.AddSubmenu("MCP Gateway")
	appSub.Add("打开管理器").OnClick(func(*application.Context) { show() })
	appSub.Add("退出并停止网关").SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) { quit(false) })
	appMenu.AddRole(application.EditMenu)
	app.Menu.SetApplicationMenu(appMenu)
	tray := app.SystemTray.New()
	tray.SetTemplateIcon(icons.SystrayMacTemplate)
	tray.SetTooltip("MCP Gateway")
	menu := app.NewMenu()
	statusItem := menu.Add("网关正在启动…").SetEnabled(false)
	pendingItem := menu.Add("检查待处理事项…").SetEnabled(false)
	menu.AddSeparator()
	menu.Add("打开管理器").OnClick(func(*application.Context) { show() })
	servicesMenu := menu.AddSubmenu("服务")
	pauseItem := menu.AddCheckbox("暂停新调用", false)
	pauseItem.OnClick(func(*application.Context) {
		go func() {
			_, err := manager.API(context.Background(), http.MethodPost, "/api/v1/gateway", gateway.Object{"paused": pauseItem.Checked()})
			if err != nil {
				app.Dialog.Error().SetMessage(err.Error()).Show()
			}
		}()
	})
	menu.Add("重连异常服务").OnClick(func(*application.Context) {
		go func() {
			snapshot, err := manager.Snapshot(context.Background())
			if err != nil {
				return
			}
			data := snapshot.(map[string]any)
			for _, v := range data["services"].([]any) {
				s := v.(map[string]any)
				if s["status"] != "ready" && s["enabled"] == true {
					params, _ := json.Marshal(gateway.Object{"serviceId": s["id"]})
					_, _ = manager.Request(context.Background(), "reconnectService", string(params))
				}
			}
		}()
	})
	menu.Add("重新启动网关").OnClick(func(*application.Context) {
		go func() {
			_, err := manager.Request(context.Background(), "restartGateway", "{}")
			if err != nil {
				app.Dialog.Error().SetMessage(err.Error()).Show()
			}
		}()
	})
	menu.Add("复制网关地址").OnClick(func(*application.Context) { app.Clipboard.SetText(manager.Address()) })
	menu.Add("设置…").OnClick(func(*application.Context) {
		show()
		window.EmitEvent("gateway:navigate", gateway.Object{"page": "settings"})
	})
	menu.AddSeparator()
	menu.Add("等待调用完成后退出").OnClick(func(*application.Context) { quit(false) })
	menu.Add("立即停止并退出").OnClick(func(*application.Context) { quit(true) })
	tray.SetMenu(menu)
	go func() {
		_ = manager.Start(context.Background())
		lastServices := ""
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			snapshot, err := manager.Snapshot(ctx)
			cancel()
			if err != nil {
				statusItem.SetLabel("网关连接异常")
				continue
			}
			data := snapshot.(map[string]any)
			g := data["gateway"].(map[string]any)
			status := fmt.Sprint(g["status"])
			label := "网关 · " + map[string]string{"running": "运行中", "starting": "启动中", "stopped": "已停止", "error": "异常"}[status]
			if g["paused"] == true {
				label = "网关 · 已暂停新调用"
			}
			if quitting.Load() {
				label = "等待调用结束…"
			}
			statusItem.SetLabel(label)
			pauseItem.SetChecked(g["paused"] == true)
			pending := 0
			rows := []gateway.Object{}
			for _, v := range data["services"].([]any) {
				s := v.(map[string]any)
				if s["enabled"] == true && s["status"] != "ready" {
					pending++
				}
				rows = append(rows, gateway.Object{"id": s["id"], "name": s["name"], "enabled": s["enabled"]})
			}
			slices.SortFunc(rows, func(a, b gateway.Object) int { return strings.Compare(fmt.Sprint(a["id"]), fmt.Sprint(b["id"])) })
			signature, _ := json.Marshal(rows)
			if string(signature) != lastServices {
				servicesMenu.Clear()
				if len(rows) == 0 {
					servicesMenu.Add("尚未添加服务").SetEnabled(false)
				}
				for _, s := range rows {
					id := fmt.Sprint(s["id"])
					item := servicesMenu.AddCheckbox(fmt.Sprint(s["name"]), s["enabled"] == true)
					item.OnClick(func(*application.Context) {
						go func() {
							p, _ := json.Marshal(gateway.Object{"serviceId": id, "enabled": item.Checked()})
							_, err := manager.Request(context.Background(), "setServiceEnabled", string(p))
							if err != nil {
								app.Dialog.Error().SetMessage(err.Error()).Show()
							}
						}()
					})
				}
				servicesMenu.Update()
				lastServices = string(signature)
			}
			pendingItem.SetLabel(fmt.Sprintf("待处理服务：%d · 执行中：%v", pending, g["activeCalls"]))
		}
	}()
	termination := make(chan os.Signal, 1)
	signal.Notify(termination, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(termination)
	go func() {
		<-termination
		quit(false)
	}()
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
