using System.ComponentModel;
using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;
using Runtime.Control.Models;
using Runtime.Control.Services;
using Forms = System.Windows.Forms;
using Brush = System.Windows.Media.Brush;

namespace Runtime.Control;

public partial class MainWindow : Window
{
    private readonly RuntimeStateService _service = new();
    private readonly DispatcherTimer _refreshTimer;
    private readonly Forms.NotifyIcon _trayIcon;
    private RuntimeState? _state;
    private bool _exitRequested;
    private bool _refreshing;

    private static readonly Brush Success = new SolidColorBrush(ColorFromHex("#4ED8A0"));
    private static readonly Brush Warning = new SolidColorBrush(ColorFromHex("#FFCB70"));
    private static readonly Brush Danger = new SolidColorBrush(ColorFromHex("#FF6B7A"));
    private static readonly Brush Muted = new SolidColorBrush(ColorFromHex("#667085"));
    private static readonly Brush Accent = new SolidColorBrush(ColorFromHex("#5DE4FF"));

    public MainWindow()
    {
        InitializeComponent();
        // The sidebar's Checked event fires before MainTabs exists during XAML loading.
        MainTabs.SelectedIndex = 0;

        _trayIcon = new Forms.NotifyIcon
        {
            Icon = System.Drawing.Icon.ExtractAssociatedIcon(Environment.ProcessPath!) ?? System.Drawing.SystemIcons.Application,
            Text = "Runtime Control",
            Visible = true
        };
        _trayIcon.DoubleClick += (_, _) => RestoreFromTray();
        _trayIcon.ContextMenuStrip = BuildTrayMenu();

        _refreshTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(5) };
        _refreshTimer.Tick += async (_, _) => await RefreshAsync(silent: true);

        Loaded += async (_, _) =>
        {
            await RefreshAsync(silent: false);
            RefreshLogs();
            _refreshTimer.Start();
            if (Environment.GetCommandLineArgs().Any(arg => string.Equals(arg, "--tray", StringComparison.OrdinalIgnoreCase)))
            {
                Hide();
            }
        };
        Closing += MainWindow_Closing;
        StateChanged += (_, _) => { };
    }

    private Forms.ContextMenuStrip BuildTrayMenu()
    {
        var menu = new Forms.ContextMenuStrip();
        var open = new Forms.ToolStripMenuItem("打开 Runtime Control");
        open.Click += (_, _) => RestoreFromTray();
        var refresh = new Forms.ToolStripMenuItem("刷新状态");
        refresh.Click += async (_, _) => await Dispatcher.InvokeAsync(async () => await RefreshAsync(silent: false));
        var separator = new Forms.ToolStripSeparator();
        var exit = new Forms.ToolStripMenuItem("退出");
        exit.Click += (_, _) => Dispatcher.Invoke(CloseForExit);
        menu.Items.Add(open);
        menu.Items.Add(refresh);
        menu.Items.Add(separator);
        menu.Items.Add(exit);
        return menu;
    }

    private void RestoreFromTray()
    {
        Dispatcher.Invoke(() =>
        {
            Show();
            if (WindowState == WindowState.Minimized)
            {
                WindowState = WindowState.Normal;
            }
            Activate();
            Topmost = true;
            Topmost = false;
            Focus();
        });
    }

    private void CloseForExit()
    {
        _exitRequested = true;
        _refreshTimer.Stop();
        _trayIcon.Visible = false;
        _trayIcon.Dispose();
        Close();
    }

    private void MainWindow_Closing(object? sender, CancelEventArgs e)
    {
        if (_exitRequested)
        {
            return;
        }
        e.Cancel = true;
        Hide();
        _trayIcon.Text = "Runtime Control · 已最小化到托盘";
    }

    private async Task RefreshAsync(bool silent)
    {
        if (_refreshing)
        {
            return;
        }
        _refreshing = true;
        try
        {
            if (!silent)
            {
                FooterText.Text = "正在刷新 Runtime 状态…";
            }
            var state = await _service.ReadAsync();
            _state = state;
            ApplyState(state);
            FooterText.Text = "状态已更新";
            FooterTimeText.Text = state.RefreshedAt.ToLocalTime().ToString("HH:mm:ss");
        }
        catch (Exception ex)
        {
            FooterText.Text = $"刷新失败：{ex.Message}";
            SidebarStatusDot.Fill = Danger;
            SidebarStatusText.Text = "状态异常";
        }
        finally
        {
            _refreshing = false;
        }
    }

    private void ApplyState(RuntimeState state)
    {
        var runtimeUsable = state.BinaryExists;
        var runtimeActive = state.RuntimeProcessCount > 0;

        SidebarStatusDot.Fill = runtimeUsable ? (runtimeActive ? Success : Accent) : Danger;
        SidebarStatusText.Text = runtimeUsable ? (runtimeActive ? "运行中" : "已安装") : "未安装";
        SidebarVersionText.Text = state.Version;

        CoreHeroDot.Fill = runtimeUsable ? (runtimeActive ? Success : Accent) : Danger;
        CoreHeroTitle.Text = runtimeUsable ? "Runtime Core" : "Runtime Core 未找到";
        CoreHeroSubtitle.Text = runtimeUsable
            ? $"{state.Version}{FormatCommit(state.Commit)} · {state.BinaryPath}"
            : $"未找到 {state.BinaryPath}";
        CoreProcessText.Text = $"{state.RuntimeProcessCount} PROCESS{(state.RuntimeProcessCount == 1 ? string.Empty : "ES")}";

        McpDot.Fill = state.Health.LocalReady ? Success : runtimeUsable ? Warning : Danger;
        McpStatusText.Text = state.Health.LocalReady ? "已验证" : runtimeUsable ? "未就绪" : "不可用";
        McpDetailText.Text = runtimeUsable
            ? JoinNonEmpty(" · ", state.PublicLocalMcpUrl, HealthCheckTime(state.Health))
            : "主程序缺失，请修复安装";
        McpCommandTextBox.Text = $"\"{state.BinaryPath}\" --stdio";

        BrowserDot.Fill = state.BridgeConnected ? Success : Warning;
        BrowserStatusText.Text = state.BridgeConnected ? "Connected" : "等待连接";
        BrowserDetailText.Text = state.BridgeConnected
            ? "Chrome Native Messaging bridge online"
            : state.NativeHostRegistered ? "Native Host 已注册，等待 Chrome Bridge" : "Native Host 未注册";
        BrowserHeroDot.Fill = BrowserDot.Fill;
        BrowserHeroText.Text = state.BridgeConnected ? "Chrome Bridge · Connected" : "Chrome Bridge · Waiting";
        BrowserHeroDetail.Text = state.BridgeConnected
            ? "Runtime Chrome Bridge 已连接；可使用 extension transport 控制真实 Chrome tab。"
            : "插件未连接到 Native Messaging Host；可在 Chrome 扩展页检查。";
        ExtensionIdText.Text = string.IsNullOrWhiteSpace(state.ExtensionId) ? "—" : state.ExtensionId;
        NativeHostText.Text = state.NativeHostRegistered ? "Registered · com.runtime.browser_bridge" : "Not registered";
        BridgePidText.Text = state.BridgePid?.ToString() ?? "—";
        BridgeStartedText.Text = state.BridgeStartedAt?.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss") ?? "—";

        ChatGptDot.Fill = state.ChatGptConfigured ? ParseConnectionBrush(state.ChatGptStatus) : Warning;
        ChatGptStatusText.Text = state.ChatGptConfigured ? FriendlyConnectionStatus(state.ChatGptStatus) : "未配置";
        ChatGptDetailText.Text = state.ChatGptConfigured
            ? JoinNonEmpty(" · ", state.ChatGptAppName, state.ChatGptEndpoint)
            : ValueOrDash(state.PublicMcpUrl);
        ChatGptConnectionSummary.Text = string.IsNullOrWhiteSpace(state.PublicMcpUrl)
            ? "固定目标：runtime.thegreatnovel.com/mcp → Cloudflare Tunnel → Runtime"
            : $"{FriendlyConnectionStatus(state.CloudflareStatus)} · {state.PublicMcpUrl}";
        ChatGptAppNameText.Text = ValueOrDash(state.ChatGptAppName);
        ChatGptTransportText.Text = ValueOrDash(state.PublicMcpUrl);
        LocalPublicMcpText.Text = ValueOrDash(state.PublicLocalMcpUrl);
        CloudflareTunnelText.Text = JoinNonEmpty(" · ", ValueOrDash(state.CloudflareTunnelName), ValueOrDash(state.CloudflareTunnelId));
        AuthStatusText.Text = $"{ValueOrDash(state.AuthMode)} · {(state.RuntimeAuthCredentialsImported ? "credentials ready" : "credentials missing")} · {(state.RuntimeTunnelTokenConfigured ? "Runtime tunnel token ready" : state.ImportedAgentDockTunnelTokenAvailable ? "AgentDock tunnel token imported as reference only" : "Runtime tunnel token pending")}";
        ChatGptEndpointText.Text = state.ChatGptConfigured
            ? JoinNonEmpty(" · ", FriendlyConnectionStatus(state.ChatGptStatus), state.ChatGptEndpoint)
            : "未绑定 ChatGPT App";
        var cloudflareReady = state.CloudflareStatus.Trim().ToLowerInvariant() is "connected" or "healthy" or "ready" or "pass";
        ChatGptBadge.Background = new SolidColorBrush(ColorFromHex(cloudflareReady && state.ChatGptConfigured ? "#123026" : "#2A2312"));
        ChatGptBadgeText.Text = cloudflareReady
            ? state.ChatGptConfigured ? FriendlyConnectionStatus(state.ChatGptStatus).ToUpperInvariant() : "APP PENDING"
            : state.RuntimeTunnelTokenConfigured ? "TUNNEL OFFLINE" : "TUNNEL PENDING";
        ChatGptBadgeText.Foreground = cloudflareReady && state.ChatGptConfigured ? Success : Warning;

        AgentDockDot.Fill = !runtimeUsable || !state.AgentDockHealthy ? Danger : Success;
        AgentDockStatusText.Text = !runtimeUsable ? "Runtime 入口不可用" : state.AgentDockHealthy ? "AgentDock 后端在线" : "后端不可用";
        AgentDockConnectionText.Text = state.AgentDockHealthy ? "AgentDock /healthz 已验证；Runtime 转发需单独验收" : "AgentDock 健康检查失败；托盘在线不代表后端可用";
        AgentDockConnectionText.Foreground = state.AgentDockHealthy ? Success : Warning;

        AcpDetailText.Text = $"默认关闭 · Codex {(state.CodexAvailable ? "available" : "not found")}";
        ApplyAdapterState(CodexDot, CodexStatusText, state.CodexAvailable);
        ApplyAdapterState(ClaudeDot, ClaudeStatusText, state.ClaudeAvailable);
        ApplyAdapterState(GrokDot, GrokStatusText, state.GrokAvailable);

        UiaDot.Fill = state.Health.LocalReady ? Success : Warning;
        UiaStatusText.Text = state.Health.LocalReady ? "Ready" : "执行器未就绪";
        UiaDetailText.Text = state.IsElevated ? "当前客户端为管理员权限" : "普通用户权限（推荐）";
        ComputerPermissionText.Text = state.IsElevated
            ? "管理员权限 · Runtime 通常不需要永久提升"
            : "普通用户权限 · Ready";
        ComputerPermissionBadgeText.Text = state.IsElevated ? "ELEVATED" : "READY";
        ComputerPermissionBadgeText.Foreground = state.IsElevated ? Warning : Success;
        ComputerPermissionBadge.Background = new SolidColorBrush(ColorFromHex(state.IsElevated ? "#2A2312" : "#113025"));

        StartupCheckBox.IsChecked = state.StartupEnabled;
        StartChatGptButton.IsEnabled = runtimeUsable && File.Exists(_service.StartChatGptScript) && state.RuntimeAuthCredentialsImported && state.RuntimeTunnelTokenConfigured;
        StopChatGptButton.IsEnabled = File.Exists(_service.StopChatGptScript);
        ConnectionActionText.Text = state.RuntimeTunnelTokenConfigured
            ? "公网链路由 Runtime 独立 Cloudflare Tunnel 管理；客户端不会显示或输出任何 secret。"
            : "认证凭据可从 AgentDock 自动迁移；独立 Runtime Cloudflare Tunnel token 仍需为 Runtime tunnel 单独生成。";

        _trayIcon.Text = runtimeActive
            ? state.BridgeConnected ? "Runtime · Core + Chrome Bridge" : "Runtime · Core active"
            : "Runtime · installed";
    }

    private static void ApplyAdapterState(System.Windows.Shapes.Ellipse dot, TextBlock text, bool available)
    {
        dot.Fill = available ? Success : Muted;
        text.Text = available ? "Available" : "Not found";
        text.Foreground = available ? Success : Muted;
    }

    private void RefreshLogs()
    {
        if (_state is null)
        {
            LogsTextBox.Text = "等待 Runtime 状态加载…";
            return;
        }
        LogsTextBox.Text = _service.ReadRecentLogs(_state.RuntimeHome);
        LogsTextBox.ScrollToEnd();
    }

    private async void Refresh_Click(object sender, RoutedEventArgs e) => await RefreshAsync(silent: false);

    private void RefreshLogs_Click(object sender, RoutedEventArgs e) => RefreshLogs();

    private void Navigation_Click(object sender, RoutedEventArgs e)
    {
        if (MainTabs is null)
        {
            return;
        }
        if (sender is System.Windows.Controls.RadioButton { Tag: string tag } && int.TryParse(tag, out var index) &&
            index >= 0 && index < MainTabs.Items.Count)
        {
            MainTabs.SelectedIndex = index;
            if (index == 5)
            {
                RefreshLogs();
            }
        }
    }

    private void TitleBar_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        if (e.ClickCount == 2)
        {
            ToggleMaximize();
            return;
        }
        if (e.ButtonState == MouseButtonState.Pressed)
        {
            DragMove();
        }
    }

    private void MinimizeButton_Click(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;
    private void MaximizeButton_Click(object sender, RoutedEventArgs e) => ToggleMaximize();
    private void CloseButton_Click(object sender, RoutedEventArgs e) => Close();

    private void ToggleMaximize()
    {
        WindowState = WindowState == WindowState.Maximized ? WindowState.Normal : WindowState.Maximized;
    }

    private void OpenRuntimeHome_Click(object sender, RoutedEventArgs e)
    {
        if (_state is not null)
        {
            RuntimeStateService.OpenDirectory(_state.RuntimeHome);
        }
    }

    private void OpenInstallDir_Click(object sender, RoutedEventArgs e) => RuntimeStateService.OpenDirectory(_service.InstallDirectory);

    private void OpenExtensionDir_Click(object sender, RoutedEventArgs e)
    {
        var path = _state?.ExtensionPath;
        if (!string.IsNullOrWhiteSpace(path))
        {
            RuntimeStateService.OpenDirectory(path);
        }
    }

    private void OpenLogs_Click(object sender, RoutedEventArgs e)
    {
        var path = _state is null ? Path.Combine(_service.InstallDirectory, "logs") : Path.Combine(_state.RuntimeHome, "logs");
        RuntimeStateService.OpenDirectory(path);
    }

    private void OpenChromeExtensions_Click(object sender, RoutedEventArgs e) => RuntimeStateService.OpenUrl("chrome://extensions/");

    private void CopyMcpCommand_Click(object sender, RoutedEventArgs e)
    {
        System.Windows.Clipboard.SetText(McpCommandTextBox.Text);
        FooterText.Text = "已复制 Runtime MCP stdio 命令";
    }

    private async void StartChatGpt_Click(object sender, RoutedEventArgs e) => await RunChatGptActionAsync("start");
    private async void StopChatGpt_Click(object sender, RoutedEventArgs e) => await RunChatGptActionAsync("stop");

    private async Task RunChatGptActionAsync(string action)
    {
        try
        {
            StartChatGptButton.IsEnabled = false;
            StopChatGptButton.IsEnabled = false;
            ConnectionActionText.Text = action == "start" ? "正在启动 ChatGPT → Runtime 链路…" : "正在暂停 ChatGPT → Runtime 链路…";
            var result = await _service.RunChatGptActionAsync(action);
            ConnectionActionText.Text = result;
            await Task.Delay(350);
            await RefreshAsync(silent: true);
        }
        catch (Exception ex)
        {
            ConnectionActionText.Text = ex.Message;
            FooterText.Text = $"操作失败：{ex.Message}";
        }
        finally
        {
            StartChatGptButton.IsEnabled = File.Exists(_service.StartChatGptScript) && _state?.RuntimeAuthCredentialsImported == true && _state.RuntimeTunnelTokenConfigured;
            StopChatGptButton.IsEnabled = File.Exists(_service.StopChatGptScript);
        }
    }

    private async void StartupCheckBox_Click(object sender, RoutedEventArgs e)
    {
        try
        {
            var enabled = StartupCheckBox.IsChecked == true;
            _service.SetStartup(enabled);
            FooterText.Text = enabled ? "已启用 Runtime Control 登录后启动" : "已关闭 Runtime Control 登录后启动";
            await RefreshAsync(silent: true);
        }
        catch (Exception ex)
        {
            StartupCheckBox.IsChecked = !StartupCheckBox.IsChecked;
            FooterText.Text = $"修改启动项失败：{ex.Message}";
        }
    }

    private static Brush ParseConnectionBrush(string status)
    {
        var normalized = status.Trim().ToLowerInvariant();
        if (normalized is "connected" or "healthy" or "ready" or "pass")
        {
            return Success;
        }
        if (normalized is "error" or "failed" or "blocked" or "repair_required" or "offline")
        {
            return Danger;
        }
        return Warning;
    }

    private static string FriendlyConnectionStatus(string status)
    {
        if (string.IsNullOrWhiteSpace(status))
        {
            return "Configured";
        }
        return status.Trim().ToLowerInvariant() switch
        {
            "connected" => "Connected",
            "healthy" => "Healthy",
            "ready" => "Ready",
            "pass" => "Ready",
            "tunnel_pending" => "Tunnel pending",
            "offline" => "Offline",
            "stopped" => "Paused",
            "blocked" => "Blocked",
            "failed" => "Failed",
            "error" => "Error",
            "repair_required" => "需修复安装",
            "unknown" => "未验证",
            "unconfigured" => "未配置",
            "configured" => "已配置，待授权验证",
            _ => status.Trim()
        };
    }

    private static string HealthCheckTime(ConnectionHealth health) => health.CheckedAt is { } time
        ? $"实测 {time.ToLocalTime():HH:mm:ss}"
        : "尚无实时检测结果";

    private static string JoinNonEmpty(string separator, params string[] values)
    {
        var items = values.Where(value => !string.IsNullOrWhiteSpace(value)).ToArray();
        return items.Length == 0 ? "已配置" : string.Join(separator, items);
    }

    private static string ValueOrDash(string value) => string.IsNullOrWhiteSpace(value) ? "—" : value;

    private static string FormatCommit(string commit)
    {
        if (string.IsNullOrWhiteSpace(commit))
        {
            return string.Empty;
        }
        return $" · {commit[..Math.Min(commit.Length, 12)]}";
    }

    private static System.Windows.Media.Color ColorFromHex(string hex) =>
        (System.Windows.Media.Color)System.Windows.Media.ColorConverter.ConvertFromString(hex)!;
}
