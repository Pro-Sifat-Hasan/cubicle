using System.IO;
using System.Text.Json;
using Cubicle.Client;

namespace Cubicle.Desktop.Services;

/// <summary>Per-user desktop preferences (appearance), stored next to the office data.</summary>
public sealed record UiPreferences(string Theme)
{
    private static string FilePath => Path.Combine(CubiclePaths.Home, "desktop.json");

    public static UiPreferences Load()
    {
        try
        {
            if (File.Exists(FilePath))
                return JsonSerializer.Deserialize<UiPreferences>(File.ReadAllText(FilePath)) ?? Default;
        }
        catch (Exception e) when (e is IOException or JsonException or UnauthorizedAccessException)
        {
            // Unreadable preferences are not worth failing startup for.
        }
        return Default;
    }

    public void Save()
    {
        try
        {
            Directory.CreateDirectory(CubiclePaths.Home);
            File.WriteAllText(FilePath, JsonSerializer.Serialize(this));
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
        }
    }

    private static UiPreferences Default => new(ThemeService.SystemMode);
}
