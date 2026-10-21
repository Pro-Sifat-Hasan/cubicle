using System.IO;
using System.Text.Json;
using Cubicle.Client;

namespace Cubicle.Desktop.Services;

/// <summary>
/// Per-user desktop preferences, stored next to the office data: appearance, where the window was,
/// whether desktop notifications are on, and whether the "still running in the tray" hint was shown.
/// </summary>
public sealed record UiPreferences(string Theme, string? Placement = null, bool Notifications = true, bool TrayHintShown = false)
{
    private static readonly object Gate = new();
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

    /// <summary>Changes some preferences and keeps the rest (each setting is saved from a different place).</summary>
    public static void Update(Func<UiPreferences, UiPreferences> change)
    {
        lock (Gate)
            change(Load()).Save();
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
