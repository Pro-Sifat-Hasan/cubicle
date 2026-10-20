namespace Cubicle.Client;

/// <summary>Locations shared with the gateway (see gateway/internal/config).</summary>
public static class CubiclePaths
{
    public const string DefaultGatewayUrl = "http://127.0.0.1:7878";

    public static string Home
    {
        get
        {
            var env = Environment.GetEnvironmentVariable("CUBICLE_HOME");
            if (!string.IsNullOrWhiteSpace(env))
                return env;
            return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Cubicle");
        }
    }

    public static string AdminTokenFile => Path.Combine(Home, "admin.token");

    public static string LogsDirectory => Path.Combine(Home, "logs");

    /// <summary>Files the staff produce, one folder per assignment id.</summary>
    public static string WorkspaceDirectory => Path.Combine(Home, "workspace");

    /// <summary>Reads the owner token the gateway generated on first start, or null if it does not exist yet.</summary>
    public static string? TryReadAdminToken(string? path = null)
    {
        var file = path ?? AdminTokenFile;
        if (!File.Exists(file))
            return null;
        var token = File.ReadAllText(file).Trim();
        return token.Length >= 32 ? token : null;
    }
}
