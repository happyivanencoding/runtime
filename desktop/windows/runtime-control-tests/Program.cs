using Runtime.Control.Models;

static void Check(bool condition, string message)
{
    if (!condition) throw new Exception(message);
}

var online = ConnectionHealth.FromJson("""{"status":"connected","local_mcp_ready":true,"public_mcp_ready":true,"live_checked_at":"2026-09-20T08:00:00Z"}""");
Check(online.PublicReady, "A successful authenticated MCP probe must show ready.");
Check(online.AppStatus(false, true, "connected") == "repair_required", "A missing executable must override cached connected state.");
Check(online.AppStatus(true, false, "") == "unconfigured", "A healthy server is not proof of ChatGPT authorization.");
Check(online.AppStatus(true, true, "pending") == "configured", "An unfinished OAuth binding must not become Connected.");
Check(online.AppStatus(true, true, "blocked") == "blocked", "A live server must not erase account-level blocking.");
var offline = ConnectionHealth.FromJson("""{"status":"stopped","recorded_status":"connected","local_mcp_ready":false,"public_mcp_ready":false}""");
Check(offline.AppStatus(true, true, "connected") == "offline", "Persisted Connected must not override a failed live check.");
var tunnelDown = ConnectionHealth.FromJson("""{"status":"degraded","local_mcp_ready":true,"public_mcp_ready":false}""");
Check(tunnelDown.LocalReady && !tunnelDown.PublicReady, "Local and public readiness must remain separate.");
var unknown = ConnectionHealth.FromJson("""{"status":"unknown","local_mcp_ready":null,"public_mcp_ready":null}""");
Check(!unknown.PublicReady && unknown.AppStatus(true, true, "connected") == "unknown", "Access denied or probe timeout must not become Connected.");
Console.WriteLine("PASS: 8 connection health regressions");
