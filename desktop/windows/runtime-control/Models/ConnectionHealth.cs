using System.Text.Json;

namespace Runtime.Control.Models;

public sealed record ConnectionHealth(string Status, bool? LocalMcpReady, bool? PublicMcpReady, DateTimeOffset? CheckedAt)
{
    public bool LocalReady => LocalMcpReady == true;
    public bool PublicReady => LocalReady && PublicMcpReady == true && Status == "connected";

    public string AppStatus(bool binaryExists, bool configured, string bindingStatus)
    {
        if (!configured) return "unconfigured";
        if (!binaryExists) return "repair_required";
        if (bindingStatus.Equals("blocked", StringComparison.OrdinalIgnoreCase)) return "blocked";
        if (PublicReady) return bindingStatus.ToLowerInvariant() is "connected" or "ready" or "healthy" or "pass" ? "connected" : "configured";
        return Status == "unknown" ? "unknown" : "offline";
    }

    public static ConnectionHealth FromJson(string json)
    {
        using var document = JsonDocument.Parse(json);
        var root = document.RootElement;
        bool? ReadBool(string name) => root.TryGetProperty(name, out var value) &&
            value.ValueKind is JsonValueKind.True or JsonValueKind.False ? value.GetBoolean() : null;
        var status = root.TryGetProperty("status", out var statusValue) ? statusValue.GetString() ?? "unknown" : "unknown";
        DateTimeOffset? checkedAt = root.TryGetProperty("live_checked_at", out var checkedValue) &&
            DateTimeOffset.TryParse(checkedValue.GetString(), out var parsed) ? parsed : null;
        return new(status, ReadBool("local_mcp_ready"), ReadBool("public_mcp_ready"), checkedAt);
    }
}
