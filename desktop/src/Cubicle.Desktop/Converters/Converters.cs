using System.Globalization;
using System.Windows;
using System.Windows.Data;
using System.Windows.Media;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Converters;

/// <summary>Maps task/employee status strings to palette colours.</summary>
public sealed class StatusToBrushConverter : IValueConverter
{
    private static Brush Get(string key) => (Brush)Application.Current.FindResource(key);

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => (value as string) switch
    {
        "done" or "idle" => Get("SuccessBrush"),
        "in_progress" or "working" or "delegated" => Get("InfoBrush"),
        "waiting_human" or "waiting" or "proposed" => Get("WarningBrush"),
        "approved" => Get("InfoBrush"),
        "failed" => Get("DangerBrush"),
        _ => Get("NeutralBrush"),
    };

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class StatusToLabelConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        value is string s ? StatusText.For(s) : "";

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>Visible when the count is above zero (or zero, with Invert).</summary>
public sealed class CountToVisibilityConverter : IValueConverter
{
    public bool Invert { get; set; }

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        (value is int n && n > 0) ^ Invert ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>Collapsed when the value is null/empty (or present, with Invert).</summary>
public sealed class NullToVisibilityConverter : IValueConverter
{
    public bool Invert { get; set; }

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        var present = value is string s ? !string.IsNullOrWhiteSpace(s) : value is not null;
        return present ^ Invert ? Visibility.Visible : Visibility.Collapsed;
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class BoolToVisibilityConverter : IValueConverter
{
    public bool Invert { get; set; }

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        (value is true) ^ Invert ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>A stable, pleasant colour per employee id for avatar circles.</summary>
public sealed class AvatarBrushConverter : IValueConverter
{
    private static readonly Brush[] Palette =
    [
        Freeze("#F59E0B"), Freeze("#3B82F6"), Freeze("#10B981"), Freeze("#8B5CF6"),
        Freeze("#EF4444"), Freeze("#06B6D4"), Freeze("#EC4899"), Freeze("#84CC16"),
    ];

    private static Brush Freeze(string hex)
    {
        var brush = new SolidColorBrush((Color)ColorConverter.ConvertFromString(hex));
        brush.Freeze();
        return brush;
    }

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        var key = value as string ?? "";
        var hash = 17;
        foreach (var c in key)
            hash = unchecked(hash * 31 + c);
        return Palette[(hash & int.MaxValue) % Palette.Length];
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class InitialConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        value is string { Length: > 0 } s ? char.ToUpperInvariant(s.Trim()[0]).ToString() : "?";

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>Two-way "value equals parameter", for binding RadioButtons to a string property.</summary>
/// <summary>Visible when the bound value equals the parameter ("log", "messages"), otherwise collapsed.</summary>
public sealed class VisibleWhenConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        string.Equals(value?.ToString(), parameter?.ToString(), StringComparison.Ordinal) ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class EqualsConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        string.Equals(value?.ToString(), parameter?.ToString(), StringComparison.Ordinal);

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        value is true ? parameter! : Binding.DoNothing;
}
