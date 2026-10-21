using System.ComponentModel;
using System.Windows;
using System.Windows.Media.Animation;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Views;

public partial class MainWindow : Window
{
    private const double CompactWidth = 1008; // Microsoft's "large" breakpoint: below it the rail shows icons only

    public MainWindow()
    {
        InitializeComponent();
        DataContextChanged += (_, e) =>
        {
            if (e.NewValue is MainViewModel vm)
                vm.PropertyChanged += OnViewModelChanged;
        };
    }

    // Collapse the navigation rail to icons on narrow windows.
    private void Window_SizeChanged(object sender, SizeChangedEventArgs e)
    {
        if (DataContext is MainViewModel vm)
            vm.IsCompact = e.NewSize.Width < CompactWidth;
    }

    /// <summary>A short fade when the page changes, so switching feels like moving rather than a flash.</summary>
    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName != nameof(MainViewModel.SelectedSection) || !IsVisible || !SystemParameters.ClientAreaAnimation)
            return;
        Pages.BeginAnimation(OpacityProperty, new DoubleAnimation(0.0, 1.0, TimeSpan.FromMilliseconds(160))
        {
            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut },
        });
    }
}
