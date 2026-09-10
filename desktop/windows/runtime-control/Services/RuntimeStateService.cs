using System.Diagnostics;
using System.IO;
using System.Security.Principal;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
using Microsoft.Win32;
using Runtime.Control.Models;

namespace Runtime.Control.Services;

public sealed class RuntimeStateService
{
    private const string NativeHostRegistryPath = @"Software\Google\Chrome\NativeMessagingHosts\com.runtime.browser_bridge";
    private const string RunRegistryPath = @"Software\Microsoft\Windows\CurrentVersion\Run";
    private const string StartupValueName = "RuntimeControl";

    private static readonly Regex SensitiveLine = new(
        @"(?i)(authorization|bearer|cookie|token|password|secret|api[_-]?key)\s*[:=]",
        RegexOptions.Compiled);

    public RuntimeStateService()
    {
        InstallDirectory = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
            "RuntimeCore");
    }

    public string InstallDirectory { get; }
    public string InstallManifestPath => Path.Combine(InstallDirectory, "install.json");
    public string ChatGptConnectionPath => Path.Combine(InstallDirectory, "chatgpt-connection.json");
    public string PublicConnectionPath => Path.Combine(InstallDirectory, "public-connection.json");
    public string SecretsDirectory => Path.Combine(InstallDirectory, "secrets");
    public string ScriptsDirectory => Path.Combine(InstallDirectory, "scripts");
    public string StartChatGptScript => Path.Combine(ScriptsDirectory, "Start-RuntimeForChatGPT.ps1");
    public string StopChatGptScript => Path.Combine(ScriptsDirectory, "Stop-RuntimeForChatGPT.ps1");
    public string StatusChatGptScript => Path.Combine(ScriptsDirectory, "Status-RuntimeForChatGPT.ps1");

    public async Task<RuntimeState> ReadAsync(CancellationToken cancellationToken = default)
    {
        var install = await ReadJsonAsync(InstallManifestPath, cancellationToken);
        var runtimeHome = ReadString(install, "runtime_home");
        if (string.IsNullOrWhiteSpace(runtimeHome))
        {
            runtimeHome = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".runtime-core");
        }

        var binaryPath = ReadString(install, "binary");
        if (string.IsNullOrWhiteSpace(binaryPath))
        {
            binaryPath = Path.Combine(InstallDirectory, "bin", "runtime-core.exe");
        }

        var version = ReadNestedString(install, "build", "version");
        var commit = ReadNestedString(install, "build", "commit");
        var extensionId = ReadString(install, "chrome_extension_id");
        var extensionPath = ReadString(install, "chrome_extension_path");
        if (string.IsNullOrWhiteSpace(extensionPath))
        {
            extensionPath = Path.Combine(InstallDirectory, "chrome-extension");
        }

        var runtimeProcessCount = CountProcessesAtPath("runtime-core", binaryPath);
        var (bridgeConnected, bridgePid, bridgeStartedAt) = await ReadBridgeStateAsync(runtimeHome, cancellationToken);
        var nativeHostRegistered = IsNativeHostRegistered();

        var chatGpt = await ReadJsonAsync(ChatGptConnectionPath, cancellationToken);
        var chatGptConfigured = chatGpt is not null;
        var chatGptAppName = ReadString(chatGpt, "app_name");
        var chatGptEndpoint = ReadString(chatGpt, "endpoint");
        var chatGptStatus = ReadString(chatGpt, "status");

        var publicConnection = await ReadJsonAsync(PublicConnectionPath, cancellationToken);
        var publicLocalMcpUrl = ReadString(publicConnection, "local_mcp_url");
        var publicMcpUrl = ReadString(publicConnection, "public_mcp_url");
        var cloudflareTunnelName = ReadString(publicConnection, "cloudflare_tunnel_name");
        var cloudflareTunnelId = ReadString(publicConnection, "cloudflare_tunnel_id");
        var cloudflareStatus = ReadString(publicConnection, "status");
        var authMode = ReadString(publicConnection, "auth_mode");
        var runtimeAuthCredentialsImported =
            File.Exists(Path.Combine(SecretsDirectory, "auth-token.dpapi")) &&
            File.Exists(Path.Combine(SecretsDirectory, "oauth-password.dpapi")) &&
            File.Exists(Path.Combine(SecretsDirectory, "oauth-token-secret.dpapi"));
        var runtimeTunnelTokenConfigured = File.Exists(Path.Combine(SecretsDirectory, "cloudflared-token.dpapi"));
        var importedAgentDockTunnelTokenAvailable = File.Exists(Path.Combine(SecretsDirectory, "imported-agentdock-cloudflared-token.dpapi"));

        var codexAvailable = FindExecutable("codex-acp.cmd", "codex-acp.exe", "codex.cmd", "codex.exe") is not null ||
                             File.Exists(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "npm", "node_modules", "@agentclientprotocol", "codex-acp", "dist", "index.js"));
        var claudeAvailable = FindExecutable("claude-agent-acp.cmd", "claude-acp.cmd", "claude.cmd", "claude.exe") is not null;
        var grokAvailable = FindExecutable("grok-build-acp.cmd", "grok-acp.cmd", "grok.cmd", "grok.exe") is not null;

        return new RuntimeState(
            InstallDirectory,
            runtimeHome,
            binaryPath,
            string.IsNullOrWhiteSpace(version) ? "unknown" : version,
            commit,
            File.Exists(binaryPath),
            runtimeProcessCount,
            bridgeConnected,
            bridgePid,
            bridgeStartedAt,
            extensionId,
            extensionPath,
            nativeHostRegistered,
            chatGptConfigured,
            chatGptAppName,
            chatGptEndpoint,
            chatGptStatus,
            publicLocalMcpUrl,
            publicMcpUrl,
            cloudflareTunnelName,
            cloudflareTunnelId,
            cloudflareStatus,
            authMode,
            runtimeAuthCredentialsImported,
            runtimeTunnelTokenConfigured,
            importedAgentDockTunnelTokenAvailable,
            IsAnyProcessRunning("agentdock", "agentdock-tray"),
            codexAvailable,
            claudeAvailable,
            grokAvailable,
            IsCurrentProcessElevated(),
            IsStartupEnabled(),
            DateTimeOffset.Now);
    }

    public async Task<string> RunChatGptActionAsync(string action, CancellationToken cancellationToken = default)
    {
        var script = action switch
        {
            "start" => StartChatGptScript,
            "stop" => StopChatGptScript,
            "status" => StatusChatGptScript,
            _ => throw new ArgumentOutOfRangeException(nameof(action), action, null)
        };

        if (!File.Exists(script))
        {
            throw new FileNotFoundException(
                "ChatGPT 直接 MCP 尚未配置，因此对应的本地控制脚本还不存在。",
                script);
        }

        var startInfo = new ProcessStartInfo
        {
            FileName = "powershell.exe",
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8
        };
        startInfo.ArgumentList.Add("-NoLogo");
        startInfo.ArgumentList.Add("-NoProfile");
        startInfo.ArgumentList.Add("-ExecutionPolicy");
        startInfo.ArgumentList.Add("Bypass");
        startInfo.ArgumentList.Add("-File");
        startInfo.ArgumentList.Add(script);

        using var process = Process.Start(startInfo) ?? throw new InvalidOperationException("无法启动 Runtime ChatGPT 控制脚本。");
        var stdoutTask = process.StandardOutput.ReadToEndAsync(cancellationToken);
        var stderrTask = process.StandardError.ReadToEndAsync(cancellationToken);
        await process.WaitForExitAsync(cancellationToken);
        var stdout = (await stdoutTask).Trim();
        var stderr = (await stderrTask).Trim();
        if (process.ExitCode != 0)
        {
            throw new InvalidOperationException(string.IsNullOrWhiteSpace(stderr) ? stdout : stderr);
        }
        return string.IsNullOrWhiteSpace(stdout) ? "完成" : RedactText(stdout);
    }

    public void SetStartup(bool enabled)
    {
        using var key = Registry.CurrentUser.CreateSubKey(RunRegistryPath, writable: true)
            ?? throw new InvalidOperationException("无法打开 Windows 启动项注册表。");
        if (!enabled)
        {
            key.DeleteValue(StartupValueName, throwOnMissingValue: false);
            return;
        }

        var executable = Environment.ProcessPath;
        if (string.IsNullOrWhiteSpace(executable) || !File.Exists(executable))
        {
            throw new InvalidOperationException("无法确定 Runtime Control 可执行文件路径。");
        }
        key.SetValue(StartupValueName, $"\"{executable}\" --tray", RegistryValueKind.String);
    }

    public bool IsStartupEnabled()
    {
        using var key = Registry.CurrentUser.OpenSubKey(RunRegistryPath, writable: false);
        return key?.GetValue(StartupValueName) is string value && !string.IsNullOrWhiteSpace(value);
    }

    public string ReadRecentLogs(string runtimeHome, int maxLines = 180)
    {
        var candidates = new List<FileInfo>();
        foreach (var directory in new[]
                 {
                     Path.Combine(runtimeHome, "logs"),
                     Path.Combine(InstallDirectory, "logs")
                 })
        {
            if (!Directory.Exists(directory))
            {
                continue;
            }
            try
            {
                candidates.AddRange(new DirectoryInfo(directory)
                    .EnumerateFiles("*.log", SearchOption.TopDirectoryOnly)
                    .OrderByDescending(file => file.LastWriteTimeUtc)
                    .Take(6));
            }
            catch (IOException)
            {
            }
            catch (UnauthorizedAccessException)
            {
            }
        }

        if (candidates.Count == 0)
        {
            return "当前没有可显示的 Runtime 日志。\r\n敏感凭据不会由 Runtime Control 读取或展示。";
        }

        var builder = new StringBuilder();
        foreach (var file in candidates
                     .DistinctBy(file => file.FullName, StringComparer.OrdinalIgnoreCase)
                     .OrderByDescending(file => file.LastWriteTimeUtc)
                     .Take(4))
        {
            builder.AppendLine($"--- {file.Name} · {file.LastWriteTime:yyyy-MM-dd HH:mm:ss} ---");
            try
            {
                foreach (var line in File.ReadLines(file.FullName).TakeLast(maxLines / 4))
                {
                    builder.AppendLine(RedactLine(line));
                }
            }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
            {
                builder.AppendLine($"[无法读取：{ex.Message}]");
            }
            builder.AppendLine();
        }
        return builder.ToString().TrimEnd();
    }

    public static void OpenDirectory(string path)
    {
        Directory.CreateDirectory(path);
        Process.Start(new ProcessStartInfo("explorer.exe", $"\"{path}\"") { UseShellExecute = true });
    }

    public static void OpenUrl(string url)
    {
        Process.Start(new ProcessStartInfo(url) { UseShellExecute = true });
    }

    private static async Task<JsonElement?> ReadJsonAsync(string path, CancellationToken cancellationToken)
    {
        if (!File.Exists(path))
        {
            return null;
        }
        try
        {
            await using var stream = File.Open(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete);
            using var document = await JsonDocument.ParseAsync(stream, cancellationToken: cancellationToken);
            return document.RootElement.Clone();
        }
        catch (Exception ex) when (ex is IOException or JsonException or UnauthorizedAccessException)
        {
            return null;
        }
    }

    private static string ReadString(JsonElement? element, string property)
    {
        if (element is not { } value || value.ValueKind != JsonValueKind.Object ||
            !value.TryGetProperty(property, out var propertyValue) || propertyValue.ValueKind != JsonValueKind.String)
        {
            return string.Empty;
        }
        return propertyValue.GetString()?.Trim() ?? string.Empty;
    }

    private static string ReadNestedString(JsonElement? element, string objectProperty, string property)
    {
        if (element is not { } value || value.ValueKind != JsonValueKind.Object ||
            !value.TryGetProperty(objectProperty, out var nested) || nested.ValueKind != JsonValueKind.Object ||
            !nested.TryGetProperty(property, out var propertyValue) || propertyValue.ValueKind != JsonValueKind.String)
        {
            return string.Empty;
        }
        return propertyValue.GetString()?.Trim() ?? string.Empty;
    }

    private static async Task<(bool Connected, int? Pid, DateTimeOffset? StartedAt)> ReadBridgeStateAsync(
        string runtimeHome,
        CancellationToken cancellationToken)
    {
        var path = Path.Combine(runtimeHome, "browser-extension", "bridge.json");
        var bridge = await ReadJsonAsync(path, cancellationToken);
        if (bridge is not { } value || value.ValueKind != JsonValueKind.Object)
        {
            return (false, null, null);
        }

        int? pid = null;
        if (value.TryGetProperty("pid", out var pidElement) && pidElement.TryGetInt32(out var parsedPid))
        {
            pid = parsedPid;
        }

        DateTimeOffset? startedAt = null;
        if (value.TryGetProperty("started_at", out var startedElement) && startedElement.ValueKind == JsonValueKind.String)
        {
            var startedText = startedElement.GetString();
            if (!string.IsNullOrWhiteSpace(startedText) && DateTimeOffset.TryParse(startedText, out var parsedStartedAt))
            {
                startedAt = parsedStartedAt;
            }
        }

        if (pid is null)
        {
            return (false, null, startedAt);
        }

        try
        {
            using var process = Process.GetProcessById(pid.Value);
            return (!process.HasExited, pid, startedAt);
        }
        catch (Exception ex) when (ex is ArgumentException or InvalidOperationException)
        {
            return (false, pid, startedAt);
        }
    }

    private static bool IsNativeHostRegistered()
    {
        using var key = Registry.CurrentUser.OpenSubKey(NativeHostRegistryPath, writable: false);
        var path = key?.GetValue(null) as string;
        return !string.IsNullOrWhiteSpace(path) && File.Exists(path);
    }

    private static int CountProcessesAtPath(string processName, string binaryPath)
    {
        if (string.IsNullOrWhiteSpace(binaryPath))
        {
            return 0;
        }

        var count = 0;
        foreach (var process in Process.GetProcessesByName(processName))
        {
            using (process)
            {
                try
                {
                    if (string.Equals(process.MainModule?.FileName, binaryPath, StringComparison.OrdinalIgnoreCase))
                    {
                        count++;
                    }
                }
                catch (Exception ex) when (ex is System.ComponentModel.Win32Exception or InvalidOperationException or NotSupportedException)
                {
                }
            }
        }
        return count;
    }

    private static bool IsAnyProcessRunning(params string[] processNames)
    {
        foreach (var name in processNames)
        {
            var processes = Process.GetProcessesByName(name);
            if (processes.Length > 0)
            {
                foreach (var process in processes)
                {
                    process.Dispose();
                }
                return true;
            }
        }
        return false;
    }

    private static string? FindExecutable(params string[] names)
    {
        var path = Environment.GetEnvironmentVariable("PATH") ?? string.Empty;
        foreach (var directory in path.Split(Path.PathSeparator, StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
        {
            foreach (var name in names)
            {
                try
                {
                    var candidate = Path.Combine(directory.Trim('"'), name);
                    if (File.Exists(candidate))
                    {
                        return candidate;
                    }
                }
                catch (Exception ex) when (ex is ArgumentException or NotSupportedException or PathTooLongException)
                {
                }
            }
        }
        return null;
    }

    private static bool IsCurrentProcessElevated()
    {
        using var identity = WindowsIdentity.GetCurrent();
        return new WindowsPrincipal(identity).IsInRole(WindowsBuiltInRole.Administrator);
    }

    private static string RedactText(string value)
    {
        return string.Join(Environment.NewLine, value.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries).Select(RedactLine));
    }

    private static string RedactLine(string line)
    {
        return SensitiveLine.IsMatch(line) ? "[敏感字段已隐藏]" : line;
    }
}
