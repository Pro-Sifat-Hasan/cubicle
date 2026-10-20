using System.Windows;
using System.Windows.Input;
using System.Windows.Media;

namespace Cubicle.Desktop.Controls;

/// <summary>
/// Passes mouse-wheel scrolling from an inner control (a list or a text box) to
/// the page's scroll viewer, so scrolling over it scrolls the page:
/// <c>controls:WheelPassthrough.IsEnabled="True"</c>.
/// </summary>
public static class WheelPassthrough
{
    public static readonly DependencyProperty IsEnabledProperty = DependencyProperty.RegisterAttached(
        "IsEnabled", typeof(bool), typeof(WheelPassthrough), new PropertyMetadata(false, OnChanged));

    public static bool GetIsEnabled(DependencyObject d) => (bool)d.GetValue(IsEnabledProperty);

    public static void SetIsEnabled(DependencyObject d, bool value) => d.SetValue(IsEnabledProperty, value);

    private static void OnChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        if (d is not UIElement element)
            return;
        element.PreviewMouseWheel -= Forward;
        if (e.NewValue is true)
            element.PreviewMouseWheel += Forward;
    }

    private static void Forward(object sender, MouseWheelEventArgs e)
    {
        if (e.Handled || sender is not DependencyObject d || VisualTreeHelper.GetParent(d) is not UIElement parent)
            return;
        e.Handled = true;
        parent.RaiseEvent(new MouseWheelEventArgs(e.MouseDevice, e.Timestamp, e.Delta) { RoutedEvent = UIElement.MouseWheelEvent, Source = sender });
    }
}
