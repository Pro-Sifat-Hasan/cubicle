using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace Cubicle.Desktop.Views.Pages;

public partial class TodayPage : UserControl
{
    public TodayPage() => InitializeComponent();

    // The assignments list sits inside the page's scroll viewer; pass wheel
    // events up so scrolling over the list scrolls the whole column.
    private void List_PreviewMouseWheel(object sender, MouseWheelEventArgs e)
    {
        if (e.Handled)
            return;
        e.Handled = true;
        var parent = (UIElement)((FrameworkElement)sender).Parent;
        parent.RaiseEvent(new MouseWheelEventArgs(e.MouseDevice, e.Timestamp, e.Delta) { RoutedEvent = MouseWheelEvent, Source = sender });
    }
}
