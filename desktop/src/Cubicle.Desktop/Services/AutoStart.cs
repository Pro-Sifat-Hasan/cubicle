using Microsoft.Win32;

namespace Cubicle.Desktop.Services;

/// <summary>Per-user "start Cubicle when I sign in" (HKCU Run key; no admin rights needed).</summary>
public static class AutoStart
{
    private const string RunKey = @"Software\Microsoft\Windows\CurrentVersion\Run";
    private const string ValueName = "Cubicle";

    public static bool IsEnabled
    {
        get
        {
            using var key = Registry.CurrentUser.OpenSubKey(RunKey);
            return key?.GetValue(ValueName) is string;
        }
    }

    public static void Set(bool enabled)
    {
        using var key = Registry.CurrentUser.CreateSubKey(RunKey);
        if (enabled)
            key.SetValue(ValueName, $"\"{Environment.ProcessPath}\" --background");
        else
            key.DeleteValue(ValueName, throwOnMissingValue: false);
    }
}
