using System.Globalization;
using System.Windows;
using System.Windows.Data;
using System.Windows.Media;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Converters;

/// <summary>Maps task/employee status strings to brand colours.</summary>
public sealed class StatusToBrushConverter : IValueConverter
{
    private static Brush Get(string key) => (Brush)Application.Current.Resources[key];

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => (value as string) switch
    {
        "done" or "idle" => Get("MintBrush"),
        "in_progress" or "working" or "delegated" => Get("SkyBrush"),
        "waiting_human" or "waiting" => Get("AmberBrush"),
        "failed" => Get("CoralBrush"),
        _ => Get("SlateBrush"),
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

    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        var any = value is int n && n > 0;
        return any ^ Invert ? Visibility.Visible : Visibility.Collapsed;
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>Collapsed when the value is null/empty (or not null, with Invert).</summary>
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
