using System.IO;

namespace Cubicle.Desktop.Services;

/// <summary>
/// Portable mode: when a <c>portable.txt</c> file sits next to Cubicle.exe, all data
/// lives in a <c>data</c> folder beside the app, and the office uses its own port and
/// instance lock so it can run alongside an installed copy. The gateway started by
/// the app inherits these settings through the environment.
/// </summary>
public static class PortableMode
{
    public const int Port = 7879;

    public static bool IsEnabled { get; private set; }

    public static void ApplyIfPresent()
    {
        var dir = AppContext.BaseDirectory;
        if (!File.Exists(Path.Join(dir, "portable.txt")))
            return;

        IsEnabled = true;
        SetDefault("CUBICLE_HOME", Path.Join(dir, "data"));
        SetDefault("CUBICLE_ADDR", $"127.0.0.1:{Port}");
        SetDefault("CUBICLE_GATEWAY_URL", $"http://127.0.0.1:{Port}");
        SetDefault("CUBICLE_INSTANCE", "portable");
    }

    private static void SetDefault(string name, string value)
    {
        if (string.IsNullOrEmpty(Environment.GetEnvironmentVariable(name)))
            Environment.SetEnvironmentVariable(name, value);
    }
}
