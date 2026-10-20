using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Interop;
using System.Windows.Media;
using Microsoft.Win32;

namespace Cubicle.Desktop.Services;

/// <summary>
/// Applies Light, Dark or System appearance: sets the WPF Fluent theme mode for
/// built-in controls, swaps Cubicle's own palette, and colours each window's
/// title bar to match (Windows 11) instead of the system accent colour.
/// </summary>
public static class ThemeService
{
    public const string SystemMode = "System";
    public const string LightMode = "Light";
    public const string DarkMode = "Dark";

    private static readonly Uri LightPalette = new("Themes/Palette.Light.xaml", UriKind.Relative);
    private static readonly Uri DarkPalette = new("Themes/Palette.Dark.xaml", UriKind.Relative);
    private static string _preference = SystemMode;
    private static bool _listening;

    public static bool IsDark { get; private set; }

    public static void Apply(string preference)
    {
        _preference = preference is LightMode or DarkMode ? preference : SystemMode;
        IsDark = _preference == DarkMode || (_preference == SystemMode && SystemPrefersDark());

        var app = Application.Current;
        app.ThemeMode = IsDark ? ThemeMode.Dark : ThemeMode.Light;

        var dictionaries = app.Resources.MergedDictionaries;
        var palette = new ResourceDictionary { Source = IsDark ? DarkPalette : LightPalette };
        var existing = dictionaries.FirstOrDefault(d => d.Source is { } s && s.OriginalString.Contains("Palette."));
        if (existing is null)
            dictionaries.Insert(0, palette);
        else
            dictionaries[dictionaries.IndexOf(existing)] = palette;

        foreach (Window window in app.Windows)
            ApplyTitleBar(window);

        if (!_listening)
        {
            _listening = true;
            SystemEvents.UserPreferenceChanged += (_, e) =>
            {
                if (e.Category == UserPreferenceCategory.General && _preference == SystemMode)
                    app.Dispatcher.BeginInvoke(() => Apply(SystemMode));
            };
        }
    }

    /// <summary>Colours the native title bar to the app background (Windows 11; ignored elsewhere).</summary>
    public static void ApplyTitleBar(Window window)
    {
        var hwnd = new WindowInteropHelper(window).Handle;
        if (hwnd == IntPtr.Zero)
        {
            window.SourceInitialized += (_, _) => ApplyTitleBar(window);
            return;
        }
        var background = (Color)Application.Current.FindResource("AppBackgroundColor");
        var text = IsDark ? Colors.White : Color.FromRgb(0x17, 0x19, 0x2B);
        SetAttribute(hwnd, DwmUseImmersiveDarkMode, IsDark ? 1 : 0);
        SetAttribute(hwnd, DwmCaptionColor, ToColorRef(background));
        SetAttribute(hwnd, DwmTextColor, ToColorRef(text));
    }

    private static bool SystemPrefersDark()
    {
        using var key = Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize");
        return key?.GetValue("AppsUseLightTheme") is int value && value == 0;
    }

    private static int ToColorRef(Color c) => c.R | (c.G << 8) | (c.B << 16);

    private const int DwmUseImmersiveDarkMode = 20;
    private const int DwmCaptionColor = 35;
    private const int DwmTextColor = 36;

    private static void SetAttribute(IntPtr hwnd, int attribute, int value) =>
        _ = DwmSetWindowAttribute(hwnd, attribute, ref value, sizeof(int)); // fails harmlessly on Windows 10

    [DllImport("dwmapi.dll")]
    private static extern int DwmSetWindowAttribute(IntPtr hwnd, int attribute, ref int value, int size);
}
