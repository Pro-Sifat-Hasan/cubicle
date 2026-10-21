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
    private static Color? _accent;
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

        ApplyAccent();
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

    /// <summary>
    /// Replaces Cubicle's amber accent with the main agent's passion colour (<c>#RRGGBB</c>), or restores the
    /// default when <paramref name="hex"/> is empty or invalid.
    /// </summary>
    public static void SetAccent(string? hex)
    {
        _accent = TryParseColor(hex);
        ApplyAccent();
    }

    internal static Color? TryParseColor(string? hex)
    {
        if (hex is not { Length: 7 } || hex[0] != '#' || !hex.Skip(1).All(Uri.IsHexDigit))
            return null;
        return (Color)ColorConverter.ConvertFromString(hex);
    }

    private static void ApplyAccent()
    {
        var resources = Application.Current?.Resources;
        if (resources is null)
            return;
        string[] keys = ["AccentBrush", "AccentHoverBrush", "AccentTextBrush", "AccentSoftBrush", "TextOnAccentBrush"];
        if (_accent is not { } c)
        {
            foreach (var key in keys)
                resources.Remove(key); // fall back to the palette's amber
            return;
        }
        resources["AccentBrush"] = Frozen(c);
        resources["AccentHoverBrush"] = Frozen(Mix(c, Colors.White, 0.18));
        // Accent used as text must stay readable against the page background.
        resources["AccentTextBrush"] = Frozen(IsDark ? Mix(c, Colors.White, 0.25) : Mix(c, Colors.Black, 0.35));
        resources["AccentSoftBrush"] = Frozen(Color.FromArgb(0x2E, c.R, c.G, c.B));
        resources["TextOnAccentBrush"] = Frozen(Luminance(c) > 0.55 ? Color.FromRgb(0x17, 0x19, 0x2B) : Colors.White);
    }

    private static SolidColorBrush Frozen(Color c)
    {
        var brush = new SolidColorBrush(c);
        brush.Freeze();
        return brush;
    }

    private static Color Mix(Color a, Color b, double t) => Color.FromRgb(
        (byte)Math.Round(a.R + (b.R - a.R) * t), (byte)Math.Round(a.G + (b.G - a.G) * t), (byte)Math.Round(a.B + (b.B - a.B) * t));

    /// <summary>Relative luminance (WCAG), 0 = black, 1 = white.</summary>
    internal static double Luminance(Color c)
    {
        static double Channel(byte v)
        {
            var s = v / 255.0;
            return s <= 0.03928 ? s / 12.92 : Math.Pow((s + 0.055) / 1.055, 2.4);
        }
        return 0.2126 * Channel(c.R) + 0.7152 * Channel(c.G) + 0.0722 * Channel(c.B);
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
