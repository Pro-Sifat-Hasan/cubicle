using System.Diagnostics;
using System.Text.RegularExpressions;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Media;

namespace Cubicle.Desktop.Controls;

/// <summary>
/// Renders the Markdown subset the staff write (headings, paragraphs, lists,
/// bold, italics, inline code, code blocks, links, rules) into a themed FlowDocument:
/// <c>&lt;FlowDocumentScrollViewer controls:Markdown.Source="{Binding Result}" /&gt;</c>.
/// </summary>
public static partial class Markdown
{
    public static readonly DependencyProperty SourceProperty = DependencyProperty.RegisterAttached(
        "Source", typeof(string), typeof(Markdown), new PropertyMetadata(null, OnSourceChanged));

    public static string? GetSource(DependencyObject d) => (string?)d.GetValue(SourceProperty);

    public static void SetSource(DependencyObject d, string? value) => d.SetValue(SourceProperty, value);

    private static void OnSourceChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        if (d is FlowDocumentScrollViewer viewer)
            viewer.Document = Render(e.NewValue as string ?? "");
        else if (d is RichTextBox box) // a read-only box sizes to its content (chat bubbles)
            box.Document = Render(e.NewValue as string ?? "");
    }

    public static FlowDocument Render(string markdown)
    {
        var doc = new FlowDocument
        {
            PagePadding = new Thickness(0),
            FontFamily = new FontFamily("Segoe UI Variable Text, Segoe UI"),
            FontSize = 14,
            LineHeight = 22,
        };
        doc.SetResourceReference(TextElement.ForegroundProperty, "TextPrimaryBrush");

        var lines = markdown.Replace("\r\n", "\n").Split('\n');
        List? list = null;
        for (var i = 0; i < lines.Length; i++)
        {
            var line = lines[i];
            var trimmed = line.Trim();

            if (trimmed.StartsWith("```", StringComparison.Ordinal))
            {
                list = null;
                var code = new List<string>();
                while (++i < lines.Length && !lines[i].Trim().StartsWith("```", StringComparison.Ordinal))
                    code.Add(lines[i]);
                doc.Blocks.Add(CodeBlock(string.Join("\n", code)));
                continue;
            }
            if (trimmed.Length == 0)
            {
                list = null;
                continue;
            }
            if (trimmed is "---" or "***" or "___")
            {
                list = null;
                doc.Blocks.Add(Rule());
                continue;
            }
            var heading = HeadingRegex().Match(trimmed);
            if (heading.Success)
            {
                list = null;
                var level = heading.Groups[1].Value.Length;
                var p = Paragraph(heading.Groups[2].Value);
                p.FontWeight = FontWeights.SemiBold;
                p.FontSize = level switch { 1 => 22, 2 => 18, _ => 15.5 };
                p.Margin = new Thickness(0, level == 1 ? 6 : 12, 0, 6);
                doc.Blocks.Add(p);
                continue;
            }
            var bullet = BulletRegex().Match(line);
            var numbered = NumberedRegex().Match(line);
            if (bullet.Success || numbered.Success)
            {
                var marker = bullet.Success ? TextMarkerStyle.Disc : TextMarkerStyle.Decimal;
                if (list is null || list.MarkerStyle != marker)
                {
                    list = new List { MarkerStyle = marker, Margin = new Thickness(0, 2, 0, 8), Padding = new Thickness(22, 0, 0, 0) };
                    doc.Blocks.Add(list);
                }
                var text = bullet.Success ? bullet.Groups[1].Value : numbered.Groups[1].Value;
                var item = Paragraph(text);
                item.Margin = new Thickness(0, 1, 0, 1);
                list.ListItems.Add(new ListItem(item));
                continue;
            }

            list = null;
            var para = Paragraph(trimmed);
            // Join soft-wrapped lines into one paragraph.
            while (i + 1 < lines.Length && IsContinuation(lines[i + 1]))
            {
                para.Inlines.Add(new Run(" "));
                AddInlines(para.Inlines, lines[++i].Trim());
            }
            doc.Blocks.Add(para);
        }
        return doc;
    }

    private static bool IsContinuation(string next)
    {
        var t = next.Trim();
        return t.Length > 0 && !t.StartsWith('#') && !t.StartsWith("```", StringComparison.Ordinal)
            && !BulletRegex().IsMatch(next) && !NumberedRegex().IsMatch(next) && t is not ("---" or "***" or "___");
    }

    private static Paragraph Paragraph(string text)
    {
        var p = new Paragraph { Margin = new Thickness(0, 0, 0, 10) };
        AddInlines(p.Inlines, text);
        return p;
    }

    private static Block CodeBlock(string code)
    {
        var p = new Paragraph(new Run(code))
        {
            FontFamily = new FontFamily("Cascadia Mono, Consolas"),
            FontSize = 12.5,
            LineHeight = 19,
            Padding = new Thickness(12, 10, 12, 10),
            Margin = new Thickness(0, 4, 0, 12),
        };
        p.SetResourceReference(TextElement.BackgroundProperty, "CodeBackgroundBrush");
        return p;
    }

    private static Block Rule()
    {
        var line = new Border { Height = 1, Margin = new Thickness(0, 8, 0, 8) };
        line.SetResourceReference(Border.BackgroundProperty, "DividerBrush");
        return new BlockUIContainer(line);
    }

    private static void AddInlines(InlineCollection inlines, string text)
    {
        var last = 0;
        foreach (Match m in InlineRegex().Matches(text))
        {
            if (m.Index > last)
                inlines.Add(new Run(text[last..m.Index]));
            if (m.Groups["bold"].Success)
                inlines.Add(new Bold(new Run(m.Groups["bold"].Value)));
            else if (m.Groups["italic"].Success)
                inlines.Add(new Italic(new Run(m.Groups["italic"].Value)));
            else if (m.Groups["code"].Success)
            {
                var run = new Run(m.Groups["code"].Value) { FontFamily = new FontFamily("Cascadia Mono, Consolas"), FontSize = 12.5 };
                run.SetResourceReference(TextElement.BackgroundProperty, "CodeBackgroundBrush");
                inlines.Add(run);
            }
            else if (m.Groups["label"].Success)
                inlines.Add(Link(m.Groups["label"].Value, m.Groups["url"].Value));
            last = m.Index + m.Length;
        }
        if (last < text.Length)
            inlines.Add(new Run(text[last..]));
    }

    private static Inline Link(string label, string url)
    {
        // Only web links are clickable; anything else renders as plain text.
        if (!Uri.TryCreate(url, UriKind.Absolute, out var uri) || (uri.Scheme != Uri.UriSchemeHttps && uri.Scheme != Uri.UriSchemeHttp))
            return new Run(label);
        var link = new Hyperlink(new Run(label)) { NavigateUri = uri, ToolTip = uri.ToString() };
        link.SetResourceReference(TextElement.ForegroundProperty, "AccentTextBrush");
        link.RequestNavigate += (_, e) => Process.Start(new ProcessStartInfo(e.Uri.ToString()) { UseShellExecute = true });
        return link;
    }

    [GeneratedRegex(@"^(#{1,6})\s+(.*)$")]
    private static partial Regex HeadingRegex();

    [GeneratedRegex(@"^\s*[-*+]\s+(.*)$")]
    private static partial Regex BulletRegex();

    [GeneratedRegex(@"^\s*\d+[.)]\s+(.*)$")]
    private static partial Regex NumberedRegex();

    [GeneratedRegex(@"\*\*(?<bold>[^*]+)\*\*|(?<!\*)\*(?<italic>[^*\s][^*]*)\*(?!\*)|`(?<code>[^`]+)`|\[(?<label>[^\]]+)\]\((?<url>[^)\s]+)\)")]
    private static partial Regex InlineRegex();
}
