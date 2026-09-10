// Modified for runtime-core in 2026; original upstream notices are retained.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/uvwt/agentdock/cmd/agentdock/internal/logx"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/desktopcontrol"
	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/httpx"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/nexusbridge"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillbundle "github.com/uvwt/agentdock/internal/skill/bundle"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "agentdock: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	// runtime-core: official binary update helpers are not fork maintenance.
	if len(args) == 1 && args[0] == "--version" {
		printVersion(stdout)
		return nil
	}
	if len(args) > 0 && args[0] == "version" {
		switch {
		case len(args) == 1:
			printVersion(stdout)
			return nil
		case len(args) == 2 && args[1] == "--json":
			return json.NewEncoder(stdout).Encode(buildinfo.Current())
		default:
			return errors.New("用法：agentdock version [--json]")
		}
	}
	if len(args) > 0 && args[0] == "update" {
		if len(args) == 1 || len(args) == 2 && args[1] == "--check" {
			return errors.New("runtime-core does not use the official AgentDock binary updater; inspect upstream-agentdock commits and selectively port source changes")
		}
		return errors.New("用法：agentdock update [--check] (disabled for runtime-core)")
	}
	if len(args) > 0 && args[0] == "service" {
		return runServiceCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "tunnel" {
		return desktopruntime.RunTunnelCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "config" {
		return desktopruntime.RunConfigCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "skill" {
		return runSkillCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "nexus" {
		return runNexusCommand(ctx, args[1:], stdout, stderr)
	}
	return runServer(ctx, args, stderr)
}

func runNexusCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return nexusCommandUsageError()
	}
	switch args[0] {
	case "pair":
		return runNexusPairCommand(ctx, args[1:], stdout, stderr)
	case "status":
		return runNexusStatusCommand(args[1:], stdout, stderr)
	default:
		return nexusCommandUsageError()
	}
}

func runNexusPairCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock nexus pair", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("endpoint", "", "NexusDock public base URL")
	code := flags.String("code", "", "one-time pairing code")
	name := flags.String("name", "", "device display name (defaults to hostname)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("用法：agentdock nexus pair --endpoint <URL> --code <配对码> [--name <名称>]")
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	identity, err := nexusbridge.Pair(ctx, cfg.AgentDockHome, nexusbridge.PairOptions{Endpoint: *endpoint, Code: *code, Name: *name})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "AgentDock 已配对到 NexusDock，node_id=%s；重启 AgentDock 后自动连接。\n", identity.NodeID)
	return nil
}

func runNexusStatusCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock nexus status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "print machine-readable status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return nexusCommandUsageError()
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	status, err := nexusbridge.ReadStatus(cfg.AgentDockHome)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(stdout).Encode(status)
	}
	if !status.Paired {
		_, err = fmt.Fprintln(stdout, "AgentDock 尚未与 NexusDock 配对。")
		return err
	}
	_, err = fmt.Fprintf(stdout, "AgentDock 已配对到 %s，node_id=%s，Device Token 已保存。\n", status.Endpoint, status.NodeID)
	return err
}

func nexusCommandUsageError() error {
	return errors.New("用法：agentdock nexus <pair --endpoint <URL> --code <配对码> [--name <名称>] | status [--json]>")
}

func runServiceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "launch-core" {
		flags := flag.NewFlagSet("agentdock service launch-core", flag.ContinueOnError)
		flags.SetOutput(stderr)
		runtimeRoot := flags.String("runtime-root", desktopruntime.DefaultRuntimeRoot(), "AgentDock 桌面运行目录")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*runtimeRoot) == "" {
			return errors.New("用法：agentdock service launch-core --runtime-root <目录>")
		}
		// launch-core 是桌面安装内部入口：先由原生代码恢复配置与 DPAPI 凭据，
		// 再在当前进程直接运行服务，避免 PowerShell 启动脚本长期参与运行链路。
		if err := desktopruntime.PrepareCoreEnvironment(*runtimeRoot); err != nil {
			return err
		}
		return runServer(ctx, nil, stderr)
	}
	return desktopruntime.RunServiceCommand(ctx, args, stdout, stderr)
}

func runSkillCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "bootstrap" {
		return errors.New("用法：agentdock skill bootstrap --bundle <目录>")
	}
	flags := flag.NewFlagSet("agentdock skill bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bundleDir := flags.String("bundle", "", "Release 随附 Skill Bundle 目录")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*bundleDir) == "" {
		return errors.New("用法：agentdock skill bootstrap --bundle <目录>")
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	stateDir, err := config.SkillStateDir(cfg)
	if err != nil {
		return err
	}
	state, err := skillstate.New(stateDir)
	if err != nil {
		return err
	}
	manager, err := skills.New(state)
	if err != nil {
		return err
	}
	result, err := skillbundle.Bootstrap(ctx, state, manager, *bundleDir)
	if err != nil {
		return err
	}
	for _, item := range result.Skills {
		fmt.Fprintf(stdout, "bundled skill installed: %s %s\n", item.Name, item.Version)
	}
	return nil
}

func printVersion(output io.Writer) {
	info := buildinfo.Current()
	fmt.Fprintf(output, "AgentDock v%s\n", strings.TrimPrefix(info.Version, "v"))
	fmt.Fprintf(output, "commit: %s\n", info.Commit)
	fmt.Fprintf(output, "built: %s\n", info.BuildDate)
	fmt.Fprintf(output, "go: %s\n", info.GoVersion)
	fmt.Fprintf(output, "platform: %s\n", info.Platform)
}

func runServer(ctx context.Context, args []string, stderr io.Writer) error {
	// 旧 Nexus 环境凭据已被配对身份取代。启动前主动清除，避免废弃密钥
	// 继续留在进程环境并被后续启动的本地工具继承。
	_ = os.Unsetenv("AGENTDOCK_NEXUS_ENDPOINT")
	_ = os.Unsetenv("AGENTDOCK_NEXUS_TOKEN")
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("agentdock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法：")
		fmt.Fprintln(stderr, "  agentdock [服务参数]")
		fmt.Fprintln(stderr, "  agentdock --version")
		fmt.Fprintln(stderr, "  agentdock version [--json]")
		fmt.Fprintln(stderr, "  agentdock update [--check] (disabled in this fork; use source maintenance)")
		fmt.Fprintln(stderr, "  agentdock service <status|start|stop|restart|autostart> --runtime-root <目录>")
		fmt.Fprintln(stderr, "  agentdock tunnel <status|start|stop|restart|regenerate|configure|autostart> --runtime-root <目录>")
		fmt.Fprintln(stderr, "  agentdock skill bootstrap --bundle <目录>")
		fmt.Fprintln(stderr, "  agentdock nexus pair --endpoint <URL> --code <配对码> [--name <名称>]")
		fmt.Fprintln(stderr, "  agentdock nexus status [--json]")
		fmt.Fprintln(stderr, "\n服务参数：")
		flags.PrintDefaults()
	}
	flags.StringVar(&cfg.Host, "host", cfg.Host, "HTTP bind host")
	flags.IntVar(&cfg.Port, "port", cfg.Port, "HTTP bind port")
	flags.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flags.BoolVar(&cfg.BrowserEnabled, "browser-enabled", cfg.BrowserEnabled, "expose optional browser automation tools")
	flags.StringVar(&cfg.BrowserExecutablePath, "browser-executable-path", cfg.BrowserExecutablePath, "optional absolute Chrome, Chromium, or Edge executable path")
	flags.StringVar(&cfg.BrowserCDPURL, "browser-cdp-url", cfg.BrowserCDPURL, "optional existing Chromium CDP endpoint to attach")
	flags.BoolVar(&cfg.BrowserReuseExistingCDP, "browser-reuse-existing-cdp", cfg.BrowserReuseExistingCDP, "discover and reuse a unique local existing CDP browser before launching one")
	flags.BoolVar(&cfg.Stdio, "stdio", cfg.Stdio, "serve JSON-RPC over stdio")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知命令或参数：%s", flags.Arg(0))
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := cfg.ValidateAuth(); err != nil {
		return err
	}
	identity, identityErr := nexusbridge.Load(cfg.AgentDockHome)
	if identityErr == nil {
		// Nexus 地址和 Device Token 只来自配对身份，避免环境变量、控制面板与
		// 配对文件形成多套凭据来源和不明确的优先级。
		cfg.NexusEndpoint = identity.Endpoint
		cfg.NexusDeviceToken = identity.DeviceToken
	} else if identityErr != nil && !errors.Is(identityErr, os.ErrNotExist) {
		slog.Error("NexusDock device identity ignored", "error", identityErr)
	}
	logx.Setup(cfg.LogLevel)
	// runtime-core: do not repair/replace an official desktop installation at startup.

	slog.Info("server starting", "agentdock_home", cfg.AgentDockHome, "agentdock_default_dir", cfg.AgentDockDefaultDir, "path_model", config.PathModel, "host", cfg.Host, "port", cfg.Port, "stdio", cfg.Stdio, "log_level", cfg.LogLevel, "recall_enabled", cfg.NexusEndpoint != "", "nexus_enabled", cfg.NexusEndpoint != "", "browser_enabled", cfg.BrowserEnabled)
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			slog.Warn("runtime close failed", "error", err)
		}
	}()
	server := mcp.NewServer(runtime, cfg)
	if cfg.Stdio {
		return serveStdio(ctx, server)
	}
	nexusStatus := &nexusbridge.ConnectionState{}
	if identityErr == nil {
		go nexusbridge.NewClient(identity, server, nexusStatus).Run(ctx)
	}
	runtimeRoot := strings.TrimSpace(os.Getenv("AGENTDOCK_RUNTIME_ROOT"))
	if runtimeRoot == "" {
		return httpx.Serve(ctx, server, cfg)
	}

	// 桌面控制端点与 HTTP/MCP 服务共享同一生命周期；任一端点异常退出时，
	// 取消另一个端点并返回明确错误，避免后台只剩半套控制面。
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- httpx.Serve(runtimeCtx, server, cfg) }()
	go func() {
		done <- desktopcontrol.Serve(runtimeCtx, runtimeRoot, func(controlCtx context.Context, request desktopcontrol.Request) (any, error) {
			return desktopruntime.DispatchControlRequest(
				controlCtx,
				request,
				desktopruntime.ControlRuntimeStatus{NexusConnected: nexusStatus.Connected()},
			)
		})
	}()
	err = <-done
	cancel()
	<-done
	return err
}

func serveStdio(ctx context.Context, server *mcp.Server) error {
	done := make(chan error, 1)
	go func() { done <- server.ServeStdio(os.Stdin, os.Stdout) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return nil
	}
}
