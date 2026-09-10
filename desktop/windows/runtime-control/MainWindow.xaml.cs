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

        _trayIcon = new Forms.NotifyIcon
        {
            Icon = System.Drawing.SystemIcons.Application,
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

        McpDot.Fill = runtimeUsable ? Success : Danger;
        McpStatusText.Text = runtimeUsable ? "本地可用" : "不可用";
        McpDetailText.Text = runtimeUsable ? "stdio · direct profile ready" : "runtime-core.exe missing";
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
            ? JoinNonEmpty(" · ", state.ChatGptAppName, state.ChatGptTransport, state.ChatGptTunnel)
            : "等待 ChatGPT Direct MCP";
        ChatGptConnectionSummary.Text = state.ChatGptConfigured
            ? $"{FriendlyConnectionStatus(state.ChatGptStatus)} · {state.ChatGptTransport}"
            : "尚未配置 ChatGPT 直接 MCP";
        ChatGptAppNameText.Text = ValueOrDash(state.ChatGptAppName);
        ChatGptTransportText.Text = ValueOrDash(state.ChatGptTransport);
        ChatGptTunnelText.Text = ValueOrDash(state.ChatGptTunnel);
        ChatGptBadge.Background = new SolidColorBrush(ColorFromHex(state.ChatGptConfigured ? "#123026" : "#2A2312"));
        ChatGptBadgeText.Text = state.ChatGptConfigured ? FriendlyConnectionStatus(state.ChatGptStatus).ToUpperInvariant() : "NOT CONFIGURED";
        ChatGptBadgeText.Foreground = state.ChatGptConfigured ? Success : Warning;

        AgentDockDot.Fill = state.AgentDockRunning ? Success : Muted;
        AgentDockStatusText.Text = state.AgentDockRunning ? "Available" : "未运行";
        AgentDockConnectionText.Text = state.AgentDockRunning ? "AgentDock 正在运行" : "AgentDock 当前未运行";
        AgentDockConnectionText.Foreground = state.AgentDockRunning ? Success : (Brush)FindResource("MutedTextBrush");

        AcpDetailText.Text = $"默认关闭 · Codex {(state.CodexAvailable ? "available" : "not found")}";
        ApplyAdapterState(CodexDot, CodexStatusText, state.CodexAvailable);
        ApplyAdapterState(ClaudeDot, ClaudeStatusText, state.ClaudeAvailable);
        ApplyAdapterState(GrokDot, GrokStatusText, state.GrokAvailable);

        UiaDot.Fill = Success;
        UiaStatusText.Text = "Ready";
        UiaDetailText.Text = state.IsElevated ? "当前客户端为管理员权限" : "普通用户权限（推荐）";
        ComputerPermissionText.Text = state.IsElevated
            ? "管理员权限 · Runtime 通常不需要永久提升"
            : "普通用户权限 · Ready";
        ComputerPermissionBadgeText.Text = state.IsElevated ? "ELEVATED" : "READY";
        ComputerPermissionBadgeText.Foreground = state.IsElevated ? Warning : Success;
        ComputerPermissionBadge.Background = new SolidColorBrush(ColorFromHex(state.IsElevated ? "#2A2312" : "#113025"));

        StartupCheckBox.IsChecked = state.StartupEnabled;
        StartChatGptButton.IsEnabled = File.Exists(_service.StartChatGptScript);
        StopChatGptButton.IsEnabled = File.Exists(_service.StopChatGptScript);
        ConnectionActionText.Text = state.ChatGptConfigured
            ? "连接控制由本机脚本执行；按钮不会显示或输出 tunnel secret。"
            : "连接脚本将在 Codex 完成 ChatGPT 配置后自动接入这里。";

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
            StartChatGptButton.IsEnabled = File.Exists(_service.StartChatGptScript);
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
        if (normalized is "error" or "failed" or "blocked")
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
            "stopped" => "Paused",
            "blocked" => "Blocked",
            "failed" => "Failed",
            "error" => "Error",
            _ => status.Trim()
        };
    }

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
