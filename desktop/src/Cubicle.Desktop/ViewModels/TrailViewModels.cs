using CommunityToolkit.Mvvm.ComponentModel;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>Turns the gateway's activity rows into lines the app can show, grouped by the task that produced them.</summary>
public static class TrailBuilder
{
    public static List<(string TaskId, ActivityLine Line)> Lines(IEnumerable<ActivityItem> items, IReadOnlyDictionary<string, Agent> agents) =>
        items.Select(a => (a.TaskId ?? "",
            ActivityLine.From(a, a.EmployeeId is { } id && agents.TryGetValue(id, out var agent) ? agent.Name : ""))).ToList();
}

/// <summary>Which collapsible sections the owner has open, kept across refreshes so a live update never folds them shut.</summary>
public sealed class OpenSections
{
    private readonly HashSet<string> _open = [];

    public bool IsOpen(string key) => _open.Contains(key);

    public void Set(string key, bool open)
    {
        if (open)
            _open.Add(key);
        else
            _open.Remove(key);
    }
}

/// <summary>"How it was done" under one agent reply: the tools it used and what it did, oldest first.</summary>
public sealed partial class ReplyTrailViewModel : ObservableObject
{
    private readonly string _key;
    private readonly OpenSections _sections;

    public ReplyTrailViewModel(string key, IReadOnlyList<ActivityLine> lines, OpenSections sections)
    {
        _key = key;
        _sections = sections;
        Lines = lines;
        _isOpen = sections.IsOpen(key);
    }

    public IReadOnlyList<ActivityLine> Lines { get; }
    public bool HasLines => Lines.Count > 0;
    public int ToolCount => Lines.Count(l => l.IsTool);
    public string Title => ToolCount switch
    {
        0 => "How it was done",
        1 => "How it was done · 1 tool used",
        _ => $"How it was done · {ToolCount} tools used",
    };

    [ObservableProperty] private bool _isOpen;

    partial void OnIsOpenChanged(bool value) => _sections.Set(_key, value);
}

/// <summary>An assignment an agent started from a chat: status, plan, how it was done and the result, kept in step with the work.</summary>
public sealed partial class StartedCardViewModel : ObservableObject
{
    private readonly OpenSections _sections;

    public StartedCardViewModel(TaskDetail detail, IReadOnlyList<(string TaskId, ActivityLine Line)> lines,
        IReadOnlyDictionary<string, Agent> agents, OpenSections sections)
    {
        _sections = sections;
        Detail = detail;
        Steps = detail.Children.OrderBy(t => t.CreatedAt)
            .Select(t => new SubtaskViewModel(t, agents.GetValueOrDefault(t.Assignee),
                lines.Where(l => l.TaskId == t.Id).Select(l => l.Line).ToList(), sections.IsOpen("step:" + t.Id),
                (id, open) => sections.Set("step:" + id, open)))
            .ToList();
        Trail = new ReplyTrailViewModel("trail:" + detail.Id, lines.Select(l => l.Line).ToList(), sections);
        _planOpen = sections.IsOpen("plan:" + detail.Id);
    }

    public TaskDetail Detail { get; }
    public string Title => Detail.Title;
    public string Status => Detail.Status;
    public string StatusLabel => StatusText.For(Detail.Status);
    public string? Result => Detail.Result;
    public string? Error => Detail.Error;
    public IReadOnlyList<SubtaskViewModel> Steps { get; }
    public bool HasSteps => Steps.Count > 0;
    public ReplyTrailViewModel Trail { get; }
    public bool IsRunning => Detail.Status is "queued" or "in_progress" or "delegated";
    public string PlanSummary => $"{Steps.Count} step{(Steps.Count == 1 ? "" : "s")} · {Steps.Count(s => s.Status == "done")} done";

    [ObservableProperty] private bool _planOpen;

    partial void OnPlanOpenChanged(bool value) => _sections.Set("plan:" + Detail.Id, value);
}
