using System.Windows;
using Forms = System.Windows.Forms;

namespace Cubicle.Desktop.Services;

/// <summary>System-tray presence so Cubicle keeps running in the background.</summary>
public sealed class TrayIcon : IDisposable
{
    private readonly Forms.NotifyIcon _icon;

    public TrayIcon(Action open, Action quit, Action stopStaffAndQuit)
    {
        var iconStream = Application.GetResourceStream(new Uri("pack://application:,,,/Assets/cubicle.ico"))!.Stream;
        var menu = new Forms.ContextMenuStrip();
        menu.Items.Add("Open Cubicle", null, (_, _) => open());
        menu.Items.Add(new Forms.ToolStripSeparator());
        menu.Items.Add("Close window (staff keep working)", null, (_, _) => quit());
        menu.Items.Add("Stop staff and quit", null, (_, _) => stopStaffAndQuit());

        _icon = new Forms.NotifyIcon
        {
            Icon = new System.Drawing.Icon(iconStream),
            Text = "Cubicle — your AI staff",
            ContextMenuStrip = menu,
            Visible = true,
        };
        _icon.DoubleClick += (_, _) => open();
        _icon.BalloonTipClicked += (_, _) => open();
    }

    public void Notify(string title, string message) =>
        _icon.ShowBalloonTip(5000, title, message.Length > 200 ? message[..200] + "…" : message, Forms.ToolTipIcon.None);

    public void Dispose()
    {
        _icon.Visible = false;
        _icon.Dispose();
    }
}
