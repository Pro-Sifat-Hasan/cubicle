using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Interop;
using Drawing = System.Drawing;
using Forms = System.Windows.Forms;

namespace Cubicle.Desktop.Services;

/// <summary>
/// Opens the window where the owner left it, and never off screen: the saved position is checked against the
/// monitors that exist now (a laptop undocked from its second screen, a changed resolution or scale), and a first
/// run is sized to the work area of the screen the owner is using, not to a fixed number of pixels.
/// Uses the Win32 window placement, which works in physical pixels and so stays right on mixed-DPI setups.
/// </summary>
public static class WindowPlacement
{
    private const double DefaultWidth = 1280, DefaultHeight = 820; // device-independent pixels at 100% scale
    private const double ScreenShare = 0.92;                        // never more than this much of the work area
    private const int ShowNormal = 1, ShowMaximized = 3;
    private const int MinVisibleWidth = 160, MinVisibleHeight = 80; // how much must stay on a screen to grab the window

    /// <summary>Places the window when its handle is created (first Show); minimum size shrinks to fit small screens.</summary>
    public static void Restore(Window window, string? saved)
    {
        window.WindowStartupLocation = WindowStartupLocation.Manual;
        window.SourceInitialized += (_, _) =>
        {
            var hwnd = new WindowInteropHelper(window).Handle;
            var placement = Build(saved, out var workArea, out var scale);
            placement.length = Marshal.SizeOf<WindowPlacementData>();
            SetWindowPlacement(hwnd, ref placement);
            FitMinimumSize(window, workArea, scale);
        };
    }

    /// <summary>The window's position as text to store in the preferences (null when it cannot be read).</summary>
    public static string? Capture(Window window)
    {
        var hwnd = new WindowInteropHelper(window).Handle;
        if (hwnd == IntPtr.Zero)
            return null;
        var p = new WindowPlacementData { length = Marshal.SizeOf<WindowPlacementData>() };
        if (!GetWindowPlacement(hwnd, ref p))
            return null;
        var r = p.rcNormalPosition;
        return $"{r.Left},{r.Top},{r.Right},{r.Bottom},{(p.showCmd == ShowMaximized ? ShowMaximized : ShowNormal)}";
    }

    /// <summary>Moves a window that ended up off every screen (monitor unplugged, resolution changed) back into view.</summary>
    public static void EnsureVisible(Window window)
    {
        var hwnd = new WindowInteropHelper(window).Handle;
        if (hwnd == IntPtr.Zero || window.WindowState == WindowState.Maximized || !GetWindowRect(hwnd, out var rect))
            return;
        var area = Forms.Screen.FromHandle(hwnd).WorkingArea;
        var bounds = new Rect(rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top);
        var width = Math.Min(bounds.Width, area.Width * ScreenShare);
        var height = Math.Min(bounds.Height, area.Height * ScreenShare);
        var visible = Rect.Intersect(new Rect(area.Left, area.Top, area.Width, area.Height), bounds);
        if (!visible.IsEmpty && visible.Width >= MinVisibleWidth && visible.Height >= MinVisibleHeight
            && width >= bounds.Width && height >= bounds.Height)
            return;
        var x = area.Left + (area.Width - width) / 2;
        var y = area.Top + (area.Height - height) / 2;
        SetWindowPos(hwnd, IntPtr.Zero, (int)x, (int)y, (int)width, (int)height, 0x0004 | 0x0010); // no z-order change, no activate
    }

    private static WindowPlacementData Build(string? saved, out Drawing.Rectangle workArea, out double scale)
    {
        var placement = new WindowPlacementData { showCmd = ShowNormal };
        if (TryParse(saved, out var rect, out var show))
        {
            var screen = ScreenFor(rect);
            if (screen is not null)
            {
                workArea = screen.WorkingArea;
                scale = ScaleOf(screen);
                rect = Fit(rect, workArea);
                placement.rcNormalPosition = new Rect32(rect.X, rect.Y, rect.Right, rect.Bottom);
                placement.showCmd = show;
                return placement;
            }
        }

        // First run, or the saved screen is gone: centre on the screen the owner is using right now.
        var current = Forms.Screen.FromPoint(Forms.Cursor.Position);
        workArea = current.WorkingArea;
        scale = ScaleOf(current);
        var width = (int)Math.Min(DefaultWidth * scale, workArea.Width * ScreenShare);
        var height = (int)Math.Min(DefaultHeight * scale, workArea.Height * ScreenShare);
        var left = workArea.Left + (workArea.Width - width) / 2;
        var top = workArea.Top + (workArea.Height - height) / 2;
        placement.rcNormalPosition = new Rect32(left, top, left + width, top + height);
        return placement;
    }

    /// <summary>The screen that shows enough of this rectangle to grab the window by, if any.</summary>
    private static Forms.Screen? ScreenFor(Drawing.Rectangle rect)
    {
        Forms.Screen? best = null;
        var bestArea = 0;
        foreach (var screen in Forms.Screen.AllScreens)
        {
            var overlap = Drawing.Rectangle.Intersect(screen.WorkingArea, rect);
            if (overlap.Width < MinVisibleWidth || overlap.Height < MinVisibleHeight)
                continue;
            if (overlap.Width * overlap.Height > bestArea)
                (best, bestArea) = (screen, overlap.Width * overlap.Height);
        }
        return best;
    }

    /// <summary>Shrinks a rectangle that is bigger than the work area and slides it fully inside.</summary>
    private static Drawing.Rectangle Fit(Drawing.Rectangle rect, Drawing.Rectangle area)
    {
        var width = Math.Min(rect.Width, (int)(area.Width * ScreenShare));
        var height = Math.Min(rect.Height, (int)(area.Height * ScreenShare));
        var left = Math.Clamp(rect.X, area.Left, Math.Max(area.Left, area.Right - width));
        var top = Math.Clamp(rect.Y, area.Top, Math.Max(area.Top, area.Bottom - height));
        return new Drawing.Rectangle(left, top, width, height);
    }

    private static bool TryParse(string? text, out Drawing.Rectangle rect, out int show)
    {
        rect = default;
        show = ShowNormal;
        var parts = text?.Split(',');
        if (parts is not { Length: 5 } || !parts.All(p => int.TryParse(p, out _)))
            return false;
        var n = parts.Select(int.Parse).ToArray();
        if (n[2] - n[0] < 200 || n[3] - n[1] < 150)
            return false;
        rect = new Drawing.Rectangle(n[0], n[1], n[2] - n[0], n[3] - n[1]);
        show = n[4] == ShowMaximized ? ShowMaximized : ShowNormal;
        return true;
    }

    private static void FitMinimumSize(Window window, Drawing.Rectangle workArea, double scale)
    {
        window.MinWidth = Math.Min(window.MinWidth, workArea.Width / scale * ScreenShare);
        window.MinHeight = Math.Min(window.MinHeight, workArea.Height / scale * ScreenShare);
    }

    private static double ScaleOf(Forms.Screen screen)
    {
        var point = new Point32(screen.Bounds.Left + screen.Bounds.Width / 2, screen.Bounds.Top + screen.Bounds.Height / 2);
        var monitor = MonitorFromPoint(point, 2); // nearest
        return monitor != IntPtr.Zero && GetDpiForMonitor(monitor, 0, out var dpiX, out _) == 0 && dpiX > 0 ? dpiX / 96.0 : 1.0;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct Point32(int x, int y)
    {
        public int X = x, Y = y;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct Rect32(int left, int top, int right, int bottom)
    {
        public int Left = left, Top = top, Right = right, Bottom = bottom;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct WindowPlacementData
    {
        public int length;
        public int flags;
        public int showCmd;
        public Point32 ptMinPosition;
        public Point32 ptMaxPosition;
        public Rect32 rcNormalPosition;
    }

    [DllImport("user32.dll")] private static extern bool SetWindowPlacement(IntPtr hWnd, ref WindowPlacementData lpwndpl);
    [DllImport("user32.dll")] private static extern bool GetWindowPlacement(IntPtr hWnd, ref WindowPlacementData lpwndpl);
    [DllImport("user32.dll")] private static extern bool GetWindowRect(IntPtr hWnd, out Rect32 lpRect);
    [DllImport("user32.dll")] private static extern bool SetWindowPos(IntPtr hWnd, IntPtr after, int x, int y, int cx, int cy, uint flags);
    [DllImport("user32.dll")] private static extern IntPtr MonitorFromPoint(Point32 pt, uint flags);
    [DllImport("shcore.dll")] private static extern int GetDpiForMonitor(IntPtr monitor, int type, out uint dpiX, out uint dpiY);
}
