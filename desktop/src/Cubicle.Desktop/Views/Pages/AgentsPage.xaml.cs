using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Effects;

namespace Cubicle.Desktop.Views.Pages;

public partial class AgentsPage : UserControl
{
    private const double SideBySideWidth = 1180; // the team list and the editor fit next to each other from here
    private const double DrawerWidth = 580;
    private bool? _overlay;

    public AgentsPage() => InitializeComponent();

    /// <summary>
    /// On a wide window the editor sits beside the team. On a narrower one it slides over the list from the right
    /// (as a side sheet does in other desktop apps) instead of squeezing the cards or running off the window.
    /// </summary>
    private void Root_SizeChanged(object sender, SizeChangedEventArgs e)
    {
        var overlay = e.NewSize.Width < SideBySideWidth;
        if (overlay)
            Drawer.Width = Math.Min(DrawerWidth, Math.Max(320, e.NewSize.Width - 24));
        if (_overlay == overlay)
            return;
        _overlay = overlay;
        if (overlay)
        {
            Grid.SetColumn(Drawer, 0);
            Grid.SetColumnSpan(Drawer, 2);
            Drawer.HorizontalAlignment = HorizontalAlignment.Right;
            Drawer.Margin = new Thickness(0);
            Panel.SetZIndex(Drawer, 10);
            Drawer.Effect = new DropShadowEffect { BlurRadius = 32, ShadowDepth = 0, Opacity = 0.45, Color = Colors.Black };
        }
        else
        {
            Grid.SetColumn(Drawer, 1);
            Grid.SetColumnSpan(Drawer, 1);
            Drawer.HorizontalAlignment = HorizontalAlignment.Stretch;
            Drawer.Margin = new Thickness(24, 0, 0, 0);
            Drawer.Width = DrawerWidth;
            Panel.SetZIndex(Drawer, 0);
            Drawer.Effect = null;
        }
    }
}
