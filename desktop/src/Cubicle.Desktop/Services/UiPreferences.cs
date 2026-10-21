using System.IO;
using System.Text.Json;
using Cubicle.Client;

namespace Cubicle.Desktop.Services;

/// <summary>Per-user desktop preferences (appearance), stored next to the office data.</summary>
public sealed record UiPreferences(string Theme)
{
    private static string FilePath => Path.Join(CubiclePaths.Home, "desktop.json");

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
            System.Diagnostics.Debug.WriteLine($"could not read preferences: {e.Message}");
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
            // A preference that cannot be saved is not worth interrupting the owner for.
            System.Diagnostics.Debug.WriteLine($"could not save preferences: {e.Message}");
        }
    }

    private static UiPreferences Default => new(ThemeService.SystemMode);
}
