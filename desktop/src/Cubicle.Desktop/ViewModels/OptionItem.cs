using CommunityToolkit.Mvvm.ComponentModel;

namespace Cubicle.Desktop.ViewModels;

/// <summary>A checkable choice in an editor (a tool, MCP server, skill or teammate).</summary>
public sealed partial class OptionItem(string id, string label, string detail = "") : ObservableObject
{
    public string Id { get; } = id;
    public string Label { get; } = label;
    public string Detail { get; } = detail;

    // Name feeds the shared avatar template when the option is an agent.
    public string Name => Label;

    [ObservableProperty] private bool _isChecked;

    public static List<string> Checked(IEnumerable<OptionItem> items) =>
        items.Where(i => i.IsChecked).Select(i => i.Id).ToList();
}

/// <summary>An agent choice in a picker; an empty id means "the main agent".</summary>
public sealed record AgentChoice(string Id, string Label)
{
    public override string ToString() => Label;
}
