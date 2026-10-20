using System.Windows;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Views;

public partial class MainWindow : Window
{
    private const double CompactWidth = 1080;

    public MainWindow() => InitializeComponent();

    // Collapse the navigation rail to icons on narrow windows.
    private void Window_SizeChanged(object sender, SizeChangedEventArgs e)
    {
        if (DataContext is MainViewModel vm)
            vm.IsCompact = e.NewSize.Width < CompactWidth;
    }
}
