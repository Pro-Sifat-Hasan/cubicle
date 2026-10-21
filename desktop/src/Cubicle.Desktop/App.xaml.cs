using System.ComponentModel;
using System.Windows;
using Cubicle.Desktop.Services;
using Cubicle.Desktop.ViewModels;
using Cubicle.Desktop.Views;
using Microsoft.Win32;

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
    private bool _quitting;

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
        WindowPlacement.Restore(_window, preferences.Placement);
        ThemeService.ApplyTitleBar(_window);
        _window.Closing += OnWindowClosing;

        _tray = new TrayIcon(
            new TrayIcon.Actions(
                Open: ShowWindow,
                OpenSection: OpenSection,
                Quit: () => Quit(stopStaff: false),
                StopStaffAndQuit: () => Quit(stopStaff: true),
                SetNotifications: on => _vm.NotificationsOn = on,
                SetAutoStart: on => _vm.StartWithWindows = on),
            notificationsOn: _vm.NotificationsOn, autoStartOn: _vm.StartWithWindows);
        _vm.Notify += OnNotify;
        _vm.PropertyChanged += (_, args) =>
        {
            if (args.PropertyName is nameof(MainViewModel.Summary) or nameof(MainViewModel.IsConnected) or nameof(MainViewModel.OpenQuestions))
                _tray.SetStatus(_vm.TrayStatus, _vm.OpenQuestions);
            else if (args.PropertyName == nameof(MainViewModel.NotificationsOn))
                _tray.SetNotificationsChecked(_vm.NotificationsOn);
        };

        // A screen that goes away (undocking, a changed resolution) must not strand the window off screen.
        SystemEvents.DisplaySettingsChanged += OnDisplayChanged;
        SessionEnding += (_, _) => SavePlacement();

        if (!e.Args.Contains("--background", StringComparer.OrdinalIgnoreCase))
            ShowWindow();
        _vm.Start();
    }

    private void ShowWindow()
    {
        if (_window is null)
            return;
        if (!_window.IsVisible)
            _window.Show();
        if (_window.WindowState == WindowState.Minimized)
            _window.WindowState = WindowState.Normal;
        _window.Activate();
        _window.Topmost = true; // a plain Activate() only flashes the taskbar when another app has focus
        _window.Topmost = false;
        _window.Focus();
    }

    private void OpenSection(int section)
    {
        if (_vm is not null)
            _vm.SelectedSection = section;
        ShowWindow();
    }

    /// <summary>Shows a notification only when the owner is not already looking at the window, and only if they want them.</summary>
    private void OnNotify(string title, string message, int section)
    {
        if (_vm is null || !_vm.NotificationsOn || (_window is { IsVisible: true, IsActive: true }))
            return;
        _tray?.Notify(title, message, section);
    }

    private void OnDisplayChanged(object? sender, EventArgs e) =>
        Dispatcher.BeginInvoke(() =>
        {
            if (_window is { IsVisible: true })
                WindowPlacement.EnsureVisible(_window);
        });

    private void SavePlacement()
    {
        if (_window is not null && WindowPlacement.Capture(_window) is { } placement)
            UiPreferences.Update(p => p with { Placement = placement });
    }

    private void OnWindowClosing(object? sender, CancelEventArgs e)
    {
        if (_quitting)
            return;
        e.Cancel = true;
        SavePlacement();
        _window!.Hide();
        if (!UiPreferences.Load().TrayHintShown)
        {
            UiPreferences.Update(p => p with { TrayHintShown = true });
            _tray?.Notify("Cubicle is still working", "Your staff keep working in the background. Click the tray icon to open it again.",
                TrayIcon.Section.Today);
        }
    }

    private void Quit(bool stopStaff)
    {
        _quitting = true;
        SavePlacement();
        SystemEvents.DisplaySettingsChanged -= OnDisplayChanged;
        if (stopStaff)
            GatewayHost.Stop();
        _vm?.Dispose();
        _tray?.Dispose();
        _instance?.Dispose();
        Shutdown();
    }
}
