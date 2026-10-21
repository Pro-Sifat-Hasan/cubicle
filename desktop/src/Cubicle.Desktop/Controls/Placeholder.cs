using System.Globalization;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Media;

namespace Cubicle.Desktop.Controls;

/// <summary>
/// Placeholder ("hint") text for TextBox, drawn in an adorner while the box is empty:
/// <c>&lt;TextBox controls:Placeholder.Text="Describe the outcome you want…" /&gt;</c>.
/// Works with the Fluent TextBox template because it never replaces it.
/// </summary>
public static class Placeholder
{
    public static readonly DependencyProperty TextProperty = DependencyProperty.RegisterAttached(
        "Text", typeof(string), typeof(Placeholder), new PropertyMetadata(null, OnTextChanged));

    public static string? GetText(DependencyObject d) => (string?)d.GetValue(TextProperty);

    public static void SetText(DependencyObject d, string? value) => d.SetValue(TextProperty, value);

    private static void OnTextChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        if (d is not TextBox box)
            return;
        box.Loaded -= Refresh;
        box.TextChanged -= Refresh;
        box.IsVisibleChanged -= VisibilityChanged;
        box.Loaded += Refresh;
        box.TextChanged += Refresh;
        box.IsVisibleChanged += VisibilityChanged; // an adorner outlives a collapsed parent, so it must follow visibility itself
        Update(box);
    }

    private static void Refresh(object sender, RoutedEventArgs e) => Update((TextBox)sender);

    private static void VisibilityChanged(object sender, DependencyPropertyChangedEventArgs e) => Update((TextBox)sender);

    private static void Update(TextBox box)
    {
        var layer = AdornerLayer.GetAdornerLayer(box);
        if (layer is null)
            return;
        var existing = layer.GetAdorners(box)?.OfType<PlaceholderAdorner>().FirstOrDefault();
        var show = box.IsVisible && string.IsNullOrEmpty(box.Text) && !string.IsNullOrEmpty(GetText(box));
        if (show && existing is null)
            layer.Add(new PlaceholderAdorner(box));
        else if (!show && existing is not null)
            layer.Remove(existing);
        else
            existing?.InvalidateVisual();
    }

    private sealed class PlaceholderAdorner : Adorner
    {
        private readonly TextBox _box;

        public PlaceholderAdorner(TextBox box) : base(box)
        {
            _box = box;
            IsHitTestVisible = false;
        }

        protected override void OnRender(DrawingContext dc)
        {
            var brush = (Brush)_box.FindResource("TextTertiaryBrush");
            var text = new FormattedText(GetText(_box) ?? "", CultureInfo.CurrentUICulture, _box.FlowDirection,
                new Typeface(_box.FontFamily, FontStyles.Normal, FontWeights.Normal, FontStretches.Normal),
                _box.FontSize, brush, VisualTreeHelper.GetDpi(this).PixelsPerDip)
            {
                MaxTextWidth = Math.Max(1, _box.ActualWidth - _box.Padding.Left - _box.Padding.Right - 8),
                Trimming = TextTrimming.CharacterEllipsis,
            };
            text.MaxTextHeight = Math.Max(1, _box.ActualHeight - _box.Padding.Top);
            // Align with where the caret starts inside the Fluent TextBox template.
            dc.DrawText(text, new Point(_box.Padding.Left + _box.BorderThickness.Left + 2, _box.Padding.Top + _box.BorderThickness.Top + 1));
        }
    }
}
