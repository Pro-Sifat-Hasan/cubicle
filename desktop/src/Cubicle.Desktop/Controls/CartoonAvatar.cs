using System.Globalization;
using System.Windows;
using System.Windows.Media;

namespace Cubicle.Desktop.Controls;

/// <summary>
/// A cartoon face for an agent, drawn from its id and name so every agent always looks like the same
/// character: skin, hair, eyes, mouth, shirt and background all come from a hash of the seed, and a job
/// in the id or name adds a prop (headset and glasses for engineers, a tie for the chief of staff, a beret
/// for writers, glasses for analysts and reviewers).
/// <c>&lt;controls:CartoonAvatar Seed="{Binding Id}" Label="{Binding Name}" /&gt;</c>
/// </summary>
public sealed class CartoonAvatar : FrameworkElement
{
    public static readonly DependencyProperty SeedProperty = DependencyProperty.Register(
        nameof(Seed), typeof(string), typeof(CartoonAvatar),
        new FrameworkPropertyMetadata("", FrameworkPropertyMetadataOptions.AffectsRender));

    public static readonly DependencyProperty LabelProperty = DependencyProperty.Register(
        nameof(Label), typeof(string), typeof(CartoonAvatar),
        new FrameworkPropertyMetadata("", FrameworkPropertyMetadataOptions.AffectsRender));

    /// <summary>0 draws a circle, 1 a square with soft corners; values between round the corners in proportion.</summary>
    public static readonly DependencyProperty SquarenessProperty = DependencyProperty.Register(
        nameof(Squareness), typeof(double), typeof(CartoonAvatar),
        new FrameworkPropertyMetadata(0.0, FrameworkPropertyMetadataOptions.AffectsRender));

    public string Seed
    {
        get => (string)GetValue(SeedProperty);
        set => SetValue(SeedProperty, value);
    }

    public string Label
    {
        get => (string)GetValue(LabelProperty);
        set => SetValue(LabelProperty, value);
    }

    public double Squareness
    {
        get => (double)GetValue(SquarenessProperty);
        set => SetValue(SquarenessProperty, value);
    }

    private static readonly Color[] Skins = ParseAll("#FFDDBF", "#F7C9A0", "#E8AE7F", "#C98B5E", "#A56B43", "#7D4E2E");
    private static readonly Color[] Hairs = ParseAll("#2B2118", "#4A2F1B", "#7A4A24", "#B5772F", "#D9B15B", "#C8452D", "#5C5C66", "#1E1E26");
    private static readonly Color[] Shirts = ParseAll("#FF7A59", "#FFB020", "#2BC48A", "#5B9BFF", "#9B7BFF", "#FF6B9A", "#17B8C4", "#EF5350");
    private static readonly Color[] Backdrops = ParseAll("#FFE8B0", "#CFF3E3", "#CFE2FF", "#E4D9FF", "#FFD6E3", "#C9F1F5", "#FFDCCF", "#E2EDC2");

    private static Color[] ParseAll(params string[] hex) =>
        [.. hex.Select(h => (Color)ColorConverter.ConvertFromString(h))];

    protected override void OnRender(DrawingContext dc)
    {
        base.OnRender(dc);
        var size = Math.Min(ActualWidth, ActualHeight);
        if (size <= 0)
            return;

        var look = Look.For(Seed, Label);
        dc.PushTransform(new TranslateTransform((ActualWidth - size) / 2, (ActualHeight - size) / 2));
        dc.PushTransform(new ScaleTransform(size / 100, size / 100));

        var radius = 50 - 38 * Math.Clamp(Squareness, 0, 1);
        dc.PushClip(new RectangleGeometry(new Rect(0, 0, 100, 100), radius, radius));
        Draw(dc, look);
        dc.Pop();

        dc.Pop();
        dc.Pop();
    }

    private static void Draw(DrawingContext dc, Look look)
    {
        var skin = Skins[look.Skin];
        var skinShade = Shade(skin, 0.86);
        var hair = Hairs[look.Hair];
        var shirt = Shirts[look.Shirt];
        var ink = Brush("#2A2230");

        // backdrop with a soft light spot
        dc.DrawRectangle(new LinearGradientBrush(Backdrops[look.Backdrop], Shade(Backdrops[look.Backdrop], 0.88), 90), null,
            new Rect(0, 0, 100, 100));

        // hair behind the head (long styles fall past the shoulders)
        if (look.HairStyle is HairStyle.Long)
            dc.DrawRoundedRectangle(Brush(hair), null, new Rect(20, 28, 60, 62), 26, 26);
        if (look.HairStyle is HairStyle.Bun)
            dc.DrawEllipse(Brush(hair), null, new Point(50, 13), 11, 11);

        // shoulders and shirt
        dc.DrawEllipse(Brush(shirt), null, new Point(50, 104), 40, 28);
        dc.DrawEllipse(Brush(Shade(shirt, 0.82)), null, new Point(50, 108), 40, 20);
        if (look.Prop is Prop.Tie)
        {
            dc.DrawGeometry(Brush("#F4F1EA"), null, Poly(41, 76, 50, 84, 59, 76, 59, 100, 41, 100));
            dc.DrawGeometry(Brush("#C0392B"), null, Poly(47.5, 82, 52.5, 82, 54, 100, 46, 100));
            dc.DrawGeometry(Brush("#C0392B"), null, Poly(46, 79, 54, 79, 50, 84));
        }

        // neck and ears
        dc.DrawRoundedRectangle(Brush(skinShade), null, new Rect(43, 62, 14, 18), 5, 5);
        dc.DrawEllipse(Brush(skin), null, new Point(24.5, 50), 4.5, 6.5);
        dc.DrawEllipse(Brush(skin), null, new Point(75.5, 50), 4.5, 6.5);

        // head
        dc.DrawEllipse(Brush(skin), null, new Point(50, 46), 26, 28);

        DrawHairFront(dc, look.HairStyle, hair);

        // brows, eyes
        var brow = new Pen(Brush(Shade(hair, 0.9)), 2.2) { StartLineCap = PenLineCap.Round, EndLineCap = PenLineCap.Round };
        var browLift = look.Mood == Mood.Curious ? 3 : 0;
        dc.DrawLine(brow, new Point(33, 38 - browLift), new Point(43, 36.5));
        dc.DrawLine(brow, new Point(57, 36.5), new Point(67, 38));
        DrawEyes(dc, look, ink);

        // nose, cheeks, mouth
        dc.DrawEllipse(Brush(skinShade), null, new Point(50, 52), 2.6, 2);
        dc.DrawEllipse(new SolidColorBrush(Color.FromArgb(70, 255, 107, 107)), null, new Point(32, 56), 5, 3.2);
        dc.DrawEllipse(new SolidColorBrush(Color.FromArgb(70, 255, 107, 107)), null, new Point(68, 56), 5, 3.2);
        DrawMouth(dc, look.Mood, ink);

        DrawProp(dc, look.Prop, ink);
    }

    private static void DrawHairFront(DrawingContext dc, HairStyle style, Color color)
    {
        var b = Brush(color);
        switch (style)
        {
            case HairStyle.Short:
                dc.DrawGeometry(b, null, Path(g =>
                {
                    g.BeginFigure(new Point(24, 44), true, true);
                    g.BezierTo(new Point(22, 16), new Point(78, 16), new Point(76, 44), true, true);
                    g.BezierTo(new Point(70, 34), new Point(60, 28), new Point(50, 28), true, true);
                    g.BezierTo(new Point(40, 28), new Point(30, 34), new Point(24, 44), true, true);
                }));
                break;
            case HairStyle.SidePart:
                dc.DrawGeometry(b, null, Path(g =>
                {
                    g.BeginFigure(new Point(23, 46), true, true);
                    g.BezierTo(new Point(18, 14), new Point(82, 12), new Point(77, 46), true, true);
                    g.BezierTo(new Point(74, 36), new Point(66, 30), new Point(40, 31), true, true);
                    g.BezierTo(new Point(32, 32), new Point(26, 38), new Point(23, 46), true, true);
                }));
                break;
            case HairStyle.Long:
                dc.DrawGeometry(b, null, Path(g =>
                {
                    g.BeginFigure(new Point(23, 50), true, true);
                    g.BezierTo(new Point(17, 12), new Point(83, 12), new Point(77, 50), true, true);
                    g.BezierTo(new Point(72, 36), new Point(60, 26), new Point(50, 26), true, true);
                    g.BezierTo(new Point(38, 28), new Point(28, 38), new Point(23, 50), true, true);
                }));
                break;
            case HairStyle.Bun:
                dc.DrawGeometry(b, null, Path(g =>
                {
                    g.BeginFigure(new Point(24, 44), true, true);
                    g.BezierTo(new Point(24, 16), new Point(76, 16), new Point(76, 44), true, true);
                    g.BezierTo(new Point(70, 32), new Point(58, 27), new Point(50, 27), true, true);
                    g.BezierTo(new Point(40, 27), new Point(30, 34), new Point(24, 44), true, true);
                }));
                break;
            case HairStyle.Curly:
                foreach (var (x, y, r) in new[] { (28d, 34d, 8d), (38, 25, 9), (50, 22, 9), (62, 25, 9), (72, 34, 8), (24, 44, 6), (76, 44, 6) })
                    dc.DrawEllipse(b, null, new Point(x, y), r, r);
                break;
            case HairStyle.Spiky:
                dc.DrawGeometry(b, null, Poly(24, 42, 22, 24, 34, 32, 36, 14, 46, 28, 54, 10, 60, 28, 70, 14, 70, 32, 80, 22, 76, 42,
                    66, 32, 50, 28, 34, 32));
                break;
            case HairStyle.Bald:
                break;
        }
    }

    private static void DrawEyes(DrawingContext dc, Look look, Brush ink)
    {
        var lid = new Pen(ink, 2.4) { StartLineCap = PenLineCap.Round, EndLineCap = PenLineCap.Round };
        foreach (var x in new[] { 39.0, 61.0 })
        {
            if (look.Mood == Mood.Happy)
            {
                dc.DrawGeometry(null, lid, Path(g =>
                {
                    g.BeginFigure(new Point(x - 5, 47), false, false);
                    g.QuadraticBezierTo(new Point(x, 41), new Point(x + 5, 47), true, true);
                }));
                continue;
            }
            dc.DrawEllipse(Brushes.White, null, new Point(x, 46), 6, 6.5);
            dc.DrawEllipse(ink, null, new Point(x + (look.Mood == Mood.Curious ? 1.2 : 0), 46.5), 3.6, 4);
            dc.DrawEllipse(Brushes.White, null, new Point(x + 1.2, 44.8), 1.2, 1.2);
        }
    }

    private static void DrawMouth(DrawingContext dc, Mood mood, Brush ink)
    {
        var line = new Pen(ink, 2.4) { StartLineCap = PenLineCap.Round, EndLineCap = PenLineCap.Round };
        switch (mood)
        {
            case Mood.Grin:
                dc.DrawGeometry(Brush("#8E2F3B"), new Pen(ink, 1.6), Path(g =>
                {
                    g.BeginFigure(new Point(40, 60), true, true);
                    g.QuadraticBezierTo(new Point(50, 62), new Point(60, 60), true, true);
                    g.QuadraticBezierTo(new Point(50, 76), new Point(40, 60), true, true);
                }));
                dc.DrawGeometry(Brushes.White, null, Path(g =>
                {
                    g.BeginFigure(new Point(42, 60.6), true, true);
                    g.QuadraticBezierTo(new Point(50, 62.4), new Point(58, 60.6), true, true);
                    g.QuadraticBezierTo(new Point(50, 65), new Point(42, 60.6), true, true);
                }));
                break;
            case Mood.Smirk:
                dc.DrawGeometry(null, line, Path(g =>
                {
                    g.BeginFigure(new Point(41, 62), false, false);
                    g.QuadraticBezierTo(new Point(52, 68), new Point(60, 59), true, true);
                }));
                break;
            default:
                dc.DrawGeometry(null, line, Path(g =>
                {
                    g.BeginFigure(new Point(41, 61), false, false);
                    g.QuadraticBezierTo(new Point(50, 69), new Point(59, 61), true, true);
                }));
                break;
        }
    }

    private static void DrawProp(DrawingContext dc, Prop prop, Brush ink)
    {
        switch (prop)
        {
            case Prop.Glasses:
                DrawGlasses(dc, ink);
                break;
            case Prop.Headset:
                DrawGlasses(dc, ink);
                var band = new Pen(Brush("#3B4252"), 3.2) { StartLineCap = PenLineCap.Round, EndLineCap = PenLineCap.Round };
                dc.DrawGeometry(null, band, Path(g =>
                {
                    g.BeginFigure(new Point(22, 48), false, false);
                    g.BezierTo(new Point(20, 10), new Point(80, 10), new Point(78, 48), true, true);
                }));
                dc.DrawRoundedRectangle(Brush("#FF7A59"), null, new Rect(17, 42, 8, 16), 3.5, 3.5);
                dc.DrawRoundedRectangle(Brush("#FF7A59"), null, new Rect(75, 42, 8, 16), 3.5, 3.5);
                dc.DrawGeometry(null, new Pen(Brush("#3B4252"), 2) { StartLineCap = PenLineCap.Round }, Path(g =>
                {
                    g.BeginFigure(new Point(79, 56), false, false);
                    g.QuadraticBezierTo(new Point(78, 68), new Point(62, 67), true, true);
                }));
                dc.DrawEllipse(Brush("#3B4252"), null, new Point(60, 67), 3.2, 2.6);
                break;
            case Prop.Beret:
                dc.DrawGeometry(Brush("#C0392B"), null, Path(g =>
                {
                    g.BeginFigure(new Point(22, 30), true, true);
                    g.BezierTo(new Point(22, 8), new Point(82, 6), new Point(80, 28), true, true);
                    g.BezierTo(new Point(70, 34), new Point(38, 36), new Point(22, 30), true, true);
                }));
                dc.DrawEllipse(Brush("#962D22"), null, new Point(53, 8), 3, 3);
                break;
        }
    }

    private static void DrawGlasses(DrawingContext dc, Brush ink)
    {
        var frame = new Pen(Brush("#2A2230"), 2) { LineJoin = PenLineJoin.Round };
        var glass = new SolidColorBrush(Color.FromArgb(46, 255, 255, 255));
        dc.DrawRoundedRectangle(glass, frame, new Rect(30, 38.5, 18, 14), 5, 5);
        dc.DrawRoundedRectangle(glass, frame, new Rect(52, 38.5, 18, 14), 5, 5);
        dc.DrawLine(frame, new Point(48, 45), new Point(52, 45));
        dc.DrawLine(frame, new Point(30, 44), new Point(25, 43));
        dc.DrawLine(frame, new Point(70, 44), new Point(75, 43));
    }

    // ---- drawing helpers ----

    private static readonly Dictionary<Color, SolidColorBrush> BrushCache = [];

    private static SolidColorBrush Brush(Color color)
    {
        if (!BrushCache.TryGetValue(color, out var brush))
        {
            brush = new SolidColorBrush(color);
            brush.Freeze();
            BrushCache[color] = brush;
        }
        return brush;
    }

    private static SolidColorBrush Brush(string hex) => Brush((Color)ColorConverter.ConvertFromString(hex));

    private static Color Shade(Color c, double factor) =>
        Color.FromRgb((byte)(c.R * factor), (byte)(c.G * factor), (byte)(c.B * factor));

    private static Geometry Poly(params double[] xy)
    {
        var geometry = new StreamGeometry();
        using (var g = geometry.Open())
        {
            g.BeginFigure(new Point(xy[0], xy[1]), true, true);
            for (var i = 2; i < xy.Length; i += 2)
                g.LineTo(new Point(xy[i], xy[i + 1]), true, false);
        }
        geometry.Freeze();
        return geometry;
    }

    private static Geometry Path(Action<StreamGeometryContext> build)
    {
        var geometry = new StreamGeometry();
        using (var g = geometry.Open())
            build(g);
        geometry.Freeze();
        return geometry;
    }

    // ---- what a character looks like ----

    private enum HairStyle { Short, SidePart, Long, Bun, Curly, Spiky, Bald }

    private enum Mood { Smile, Grin, Smirk, Happy, Curious }

    private enum Prop { None, Glasses, Headset, Tie, Beret }

    private sealed record Look(int Skin, int Hair, int Shirt, int Backdrop, HairStyle HairStyle, Mood Mood, Prop Prop)
    {
        public static Look For(string? seed, string? label)
        {
            var key = $"{seed}|{label}".ToLower(CultureInfo.InvariantCulture);
            var h = Hash(key);
            int Pick(int count, int salt) => (int)((h >> (salt * 5 % 28)) % (uint)count);

            var prop = key switch
            {
                _ when HasAny(key, "engineer", "dev", "code", "software", "program") => Prop.Headset,
                _ when HasAny(key, "chief", "manager", "main", "staff", "lead") => Prop.Tie,
                _ when HasAny(key, "writer", "quill", "editor", "translat") => Prop.Beret,
                _ when HasAny(key, "analyst", "data", "review", "quality", "research", "fact") => Prop.Glasses,
                _ => Pick(3, 7) == 1 ? Prop.Glasses : Prop.None,
            };
            var style = (HairStyle)Pick(7, 3);
            if (prop == Prop.Beret && style is HairStyle.Bald or HairStyle.Spiky)
                style = HairStyle.Long; // a beret sits on hair
            return new Look(Pick(Skins.Length, 1), Pick(Hairs.Length, 2), Pick(Shirts.Length, 4), Pick(Backdrops.Length, 5),
                style, (Mood)Pick(5, 6), prop);
        }

        private static bool HasAny(string text, params string[] words) => words.Any(text.Contains);

        // FNV-1a, then a final mix so nearby ids (agent-1, agent-2) look different.
        private static uint Hash(string text)
        {
            var h = 2166136261u;
            foreach (var ch in text)
                h = (h ^ ch) * 16777619u;
            h ^= h >> 15;
            h *= 2246822519u;
            h ^= h >> 13;
            return h;
        }
    }
}
