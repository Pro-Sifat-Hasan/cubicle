using System.ComponentModel;
using System.Windows;
using Cubicle.Desktop.Services;
using Cubicle.Desktop.ViewModels;
using Cubicle.Desktop.Views;

namespace Cubicle.Desktop;

/// <summary>
/// Startup: one instance per user, a tray icon, and a window that hides instead
/// of closing so the office keeps running in the background.
/// Pass <c>--background</c> (used by "start with Windows") to start in the tray.
/// </summary>
public partial class App : Application
{
    private SingleInstance? _instance;
    private TrayIcon? _tray;
    private MainViewModel? _vm;
    private MainWindow? _window;
    private bool _hintShown;

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        PortableMode.ApplyIfPresent(); // must run before anything reads paths, ports or the instance name
        _instance = SingleInstance.TryAcquire(() => Dispatcher.BeginInvoke(ShowWindow));
        if (_instance is null)
        {
            Shutdown();
            return;
        }

        var preferences = UiPreferences.Load();
        ThemeService.Apply(preferences.Theme);

        _vm = new MainViewModel(preferences.Theme);
        _window = new MainWindow { DataContext = _vm };
        ThemeService.ApplyTitleBar(_window);
        _window.Closing += OnWindowClosing;

        _tray = new TrayIcon(open: ShowWindow, quit: () => Quit(stopStaff: false), stopStaffAndQuit: () => Quit(stopStaff: true));
        _vm.Notify += (title, message) => _tray.Notify(title, message);

        if (!e.Args.Contains("--background", StringComparer.OrdinalIgnoreCase))
            ShowWindow();
        _vm.Start();
    }

    private void ShowWindow()
    {
        if (_window is null)
            return;
        _window.Show();
        if (_window.WindowState == WindowState.Minimized)
            _window.WindowState = WindowState.Normal;
        _window.Activate();
    }

    private void OnWindowClosing(object? sender, CancelEventArgs e)
    {
        e.Cancel = true;
        _window!.Hide();
        if (!_hintShown)
        {
            _hintShown = true;
            _tray?.Notify("Cubicle is still working", "Your staff keep working in the background. Open Cubicle from the tray.");
        }
    }

    private void Quit(bool stopStaff)
    {
        if (stopStaff)
            GatewayHost.Stop();
        _vm?.Dispose();
        _tray?.Dispose();
        _instance?.Dispose();
        Shutdown();
    }
}
