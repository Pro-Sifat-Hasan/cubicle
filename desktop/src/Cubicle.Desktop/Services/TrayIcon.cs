using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Media;
using Drawing = System.Drawing;
using Forms = System.Windows.Forms;

namespace Cubicle.Desktop.Services;

/// <summary>
/// System-tray presence so Cubicle keeps running in the background. Like other tray apps: a click opens the window,
/// the tooltip says what the staff are doing, a dot on the icon says something needs the owner, the menu follows
/// the app's theme, and a notification opens the page it is about.
/// </summary>
public sealed class TrayIcon : IDisposable
{
    private readonly Forms.NotifyIcon _icon;
    private readonly Drawing.Icon _normal;
    private Drawing.Icon? _badged;
    private readonly Forms.ToolStripMenuItem _status;
    private readonly Forms.ToolStripMenuItem _inbox;
    private readonly Forms.ToolStripMenuItem _notifications;
    private readonly Forms.ToolStripMenuItem _autoStart;
    private int _balloonSection;
    private string _lastNotice = "";
    private DateTime _lastNoticeAt;

    public TrayIcon(Actions actions, bool notificationsOn, bool autoStartOn)
    {
        using var stream = Application.GetResourceStream(new Uri("pack://application:,,,/Assets/cubicle.ico"))!.Stream;
        _normal = new Drawing.Icon(stream, Forms.SystemInformation.SmallIconSize);

        _status = new Forms.ToolStripMenuItem("Cubicle") { Enabled = false };
        var open = new Forms.ToolStripMenuItem("Open Cubicle") { Font = new Drawing.Font(Forms.Control.DefaultFont, Drawing.FontStyle.Bold) };
        open.Click += (_, _) => actions.Open();
        _inbox = new Forms.ToolStripMenuItem("Inbox");
        _inbox.Click += (_, _) => actions.OpenSection(Section.Inbox);
        _notifications = new Forms.ToolStripMenuItem("Desktop notifications") { Checked = notificationsOn, CheckOnClick = true };
        _notifications.CheckedChanged += (_, _) => actions.SetNotifications(_notifications.Checked);
        _autoStart = new Forms.ToolStripMenuItem("Start when I sign in") { Checked = autoStartOn, CheckOnClick = true };
        _autoStart.CheckedChanged += (_, _) => actions.SetAutoStart(_autoStart.Checked);
        var quit = new Forms.ToolStripMenuItem("Quit (staff keep working)");
        quit.Click += (_, _) => actions.Quit();
        var stop = new Forms.ToolStripMenuItem("Stop staff and quit");
        stop.Click += (_, _) => actions.StopStaffAndQuit();

        var menu = new Forms.ContextMenuStrip { ShowImageMargin = false, ShowCheckMargin = true, Padding = new Forms.Padding(4) };
        menu.Items.AddRange([_status, new Forms.ToolStripSeparator(), open, _inbox, new Forms.ToolStripSeparator(),
            _notifications, _autoStart, new Forms.ToolStripSeparator(), quit, stop]);
        menu.Opening += (_, _) => ThemedMenu.Apply(menu);

        _icon = new Forms.NotifyIcon { Icon = _normal, Text = "Cubicle", ContextMenuStrip = menu, Visible = true };
        _icon.MouseClick += (_, e) =>
        {
            if (e.Button == Forms.MouseButtons.Left)
                actions.Open();
        };
        _icon.BalloonTipClicked += (_, _) => actions.OpenSection(_balloonSection);
        SetStatus("Your staff are ready", 0);
    }

    /// <summary>What the tray shows: a tooltip, the first menu line, the Inbox entry and a dot when something needs the owner.</summary>
    public void SetStatus(string text, int needYou)
    {
        _status.Text = text;
        _inbox.Text = needYou > 0 ? $"Inbox ({needYou} need you)" : "Inbox";
        _icon.Text = Truncate("Cubicle: " + text, 127); // NotifyIcon allows 127 characters
        if (needYou > 0)
            _badged ??= WithBadge(_normal);
        _icon.Icon = needYou > 0 ? _badged : _normal;
    }

    /// <summary>A notification; clicking it opens <paramref name="section"/>. The same message is not shown twice in a row.</summary>
    public void Notify(string title, string message, int section)
    {
        var text = Truncate(message, 200);
        if (text == _lastNotice && DateTime.UtcNow - _lastNoticeAt < TimeSpan.FromSeconds(30))
            return;
        (_lastNotice, _lastNoticeAt, _balloonSection) = (text, DateTime.UtcNow, section);
        _icon.ShowBalloonTip(6000, title, text, Forms.ToolTipIcon.None);
    }

    public void SetNotificationsChecked(bool on) => _notifications.Checked = on;

    public void Dispose()
    {
        _icon.Visible = false;
        _icon.Dispose();
        _badged?.Dispose();
        _normal.Dispose();
    }

    private static string Truncate(string text, int max) => text.Length > max ? text[..(max - 1)] + "…" : text;

    private static Drawing.Icon WithBadge(Drawing.Icon baseIcon)
    {
        using var bitmap = new Drawing.Bitmap(baseIcon.Width, baseIcon.Height);
        using (var g = Drawing.Graphics.FromImage(bitmap))
        {
            g.SmoothingMode = Drawing.Drawing2D.SmoothingMode.AntiAlias;
            g.DrawIcon(baseIcon, 0, 0);
            var d = Math.Max(6, bitmap.Width / 2);
            var dot = new Drawing.Rectangle(bitmap.Width - d, bitmap.Height - d, d - 1, d - 1);
            using var ring = new Drawing.SolidBrush(Drawing.Color.FromArgb(255, 20, 20, 24));
            using var fill = new Drawing.SolidBrush(Drawing.Color.FromArgb(255, 255, 176, 32));
            g.FillEllipse(ring, Drawing.Rectangle.Inflate(dot, 1, 1));
            g.FillEllipse(fill, dot);
        }
        var handle = bitmap.GetHicon();
        try
        {
            return (Drawing.Icon)Drawing.Icon.FromHandle(handle).Clone();
        }
        finally
        {
            DestroyIcon(handle);
        }
    }

    [DllImport("user32.dll")] private static extern bool DestroyIcon(IntPtr handle);

    /// <summary>Pages the tray can open (the same numbers as the navigation rail).</summary>
    public static class Section
    {
        public const int Today = 0, Inbox = 1;
    }

    public sealed record Actions(
        Action Open, Action<int> OpenSection, Action Quit, Action StopStaffAndQuit,
        Action<bool> SetNotifications, Action<bool> SetAutoStart);

    /// <summary>Colours the menu from the app's palette, so it is dark in the dark theme instead of a grey Windows 95 box.</summary>
    private static class ThemedMenu
    {
        public static void Apply(Forms.ContextMenuStrip menu)
        {
            var colors = new Palette();
            menu.Renderer = new Forms.ToolStripProfessionalRenderer(colors) { RoundedEdges = false };
            menu.BackColor = colors.Surface;
            menu.ForeColor = colors.Text;
            menu.Font = new Drawing.Font("Segoe UI Variable Text", 9.5f);
            foreach (Forms.ToolStripItem item in menu.Items)
            {
                item.ForeColor = item.Enabled ? colors.Text : colors.Muted;
                item.Padding = new Forms.Padding(2, 5, 2, 5);
            }
        }

        private sealed class Palette : Forms.ProfessionalColorTable
        {
            public Drawing.Color Surface { get; } = Pick("SurfaceBrush", Drawing.Color.White);
            public Drawing.Color Text { get; } = Pick("TextPrimaryBrush", Drawing.Color.Black);
            public Drawing.Color Muted { get; } = Pick("TextTertiaryBrush", Drawing.Color.Gray);
            private Drawing.Color Hover { get; } = Pick("SurfaceHoverBrush", Drawing.Color.LightGray);
            private Drawing.Color Border { get; } = Pick("BorderBrush", Drawing.Color.Gray);

            public override Drawing.Color ToolStripDropDownBackground => Surface;
            public override Drawing.Color MenuBorder => Border;
            public override Drawing.Color MenuItemBorder => Hover;
            public override Drawing.Color MenuItemSelected => Hover;
            public override Drawing.Color MenuItemSelectedGradientBegin => Hover;
            public override Drawing.Color MenuItemSelectedGradientEnd => Hover;
            public override Drawing.Color ImageMarginGradientBegin => Surface;
            public override Drawing.Color ImageMarginGradientMiddle => Surface;
            public override Drawing.Color ImageMarginGradientEnd => Surface;
            public override Drawing.Color SeparatorDark => Border;
            public override Drawing.Color SeparatorLight => Surface;
            public override Drawing.Color CheckBackground => Hover;
            public override Drawing.Color CheckSelectedBackground => Hover;
            public override Drawing.Color CheckPressedBackground => Hover;

            private static Drawing.Color Pick(string key, Drawing.Color fallback) =>
                Application.Current.TryFindResource(key) is SolidColorBrush { Color: var c }
                    ? Drawing.Color.FromArgb(255, c.R, c.G, c.B)
                    : fallback;
        }
    }
}
