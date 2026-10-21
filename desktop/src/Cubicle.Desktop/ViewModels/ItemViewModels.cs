using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// A request from an agent: a question (answered with text) or an approval (approved or declined, with an optional note).
/// <paramref name="send"/> receives the decision ("approve" or "decline") for an approval, and null for a question.
/// </summary>
public sealed partial class InboxItemViewModel(InboxItem item, string employeeName, Func<InboxItemViewModel, string?, Task> send) : ObservableObject
{
    public InboxItem Item { get; } = item;
    public string EmployeeName { get; } = employeeName;
    public bool IsApproval => Item.IsApproval;
    public string Heading => IsApproval ? $"{EmployeeName} needs your approval" : $"{EmployeeName} has a question";

    /// <summary>Where the request came from: a chat, or an assignment.</summary>
    public string Source => Item.FromChat ? "In a chat"
        : string.IsNullOrEmpty(Item.TaskTitle) ? "" : $"On \u201C{Item.TaskTitle}\u201D";

    public string Asked => Item.CreatedAt.ToLocalTime().ToString("ddd HH:mm");

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(SendCommand))]
    private string _answerText = "";

    private bool CanSend() => !string.IsNullOrWhiteSpace(AnswerText);

    [RelayCommand(CanExecute = nameof(CanSend))]
    private Task SendAsync() => send(this, null);

    [RelayCommand]
    private Task ApproveAsync() => send(this, "approve");

    [RelayCommand]
    private Task DeclineAsync() => send(this, "decline");
}

/// <summary>A request you already dealt with, for the history under "Needs you".</summary>
public sealed class RequestHistoryViewModel(InboxItem item, string employeeName)
{
    public string Who { get; } = employeeName;
    public string Question { get; } = item.Question;
    public bool IsApproval => item.IsApproval;
    public string Outcome { get; } = item.IsApproval
        ? (item.Answer?.StartsWith("Approved", StringComparison.Ordinal) == true ? "Approved" : "Declined")
        : "Answered";

    public string Reply { get; } = item.IsApproval ? "" : item.Answer ?? "";
    public string When { get; } = item.CreatedAt.ToLocalTime().ToString("ddd dd MMM, HH:mm");
}

public sealed partial class RoutineViewModel(
    Routine routine,
    string who,
    Func<RoutineViewModel, bool, Task> toggle,
    Func<RoutineViewModel, Task> delete,
    Action<RoutineViewModel> edit,
    Func<RoutineViewModel, Task> runNow) : ObservableObject
{
    public Routine Routine { get; } = routine;
    public string When => Schedules.Describe(Routine.Schedule);
    public string Who { get; } = who;
    public string NextRun => Routine.Enabled ? $"Next: {Routine.NextRunAt.ToLocalTime():ddd dd MMM, HH:mm}" : "Paused";
    public string LastRun => Routine.LastRunAt is { Year: > 1 } last ? $"Last: {last.ToLocalTime():ddd dd MMM, HH:mm}" : "Never run yet";
    public string LastStatus => Routine.LastStatus ?? "";
    public string LastStatusLabel => string.IsNullOrEmpty(Routine.LastStatus) ? "" : StatusText.For(Routine.LastStatus);

    [RelayCommand]
    private void Edit() => edit(this);

    [RelayCommand]
    private Task RunNowAsync() => runNow(this);

    public bool Enabled
    {
        get => Routine.Enabled;
        set
        {
            if (value != Routine.Enabled)
                _ = toggle(this, value);
        }
    }

    [RelayCommand]
    private Task DeleteAsync() => delete(this);
}

public sealed partial class MemoryViewModel(Memory memory, string source, Func<MemoryViewModel, Task> delete) : ObservableObject
{
    public Memory Memory { get; } = memory;
    public string Content => Memory.Content;
    public string Source { get; } = source;
    public string Added => Memory.CreatedAt.ToLocalTime().ToString("dd MMM yyyy");

    [RelayCommand]
    private Task DeleteAsync() => delete(this);
}

public sealed class AssignmentViewModel(TaskItem task, string assigneeName)
{
    public TaskItem Task { get; } = task;
    public string Title => Task.Title;
    public string Status => Task.Status;
    public string StatusLabel => StatusText.For(Task.Status);
    public string When => $"{Task.CreatedAt.ToLocalTime():ddd dd MMM, HH:mm} · {assigneeName}{(string.IsNullOrEmpty(Task.RoutineId) ? "" : " · automation")}";

    /// <summary>True while the team is still on it (shows a progress bar in the list).</summary>
    public bool IsRunning => Task.Status is "queued" or "in_progress" or "delegated";
}

/// <summary>One line of an agent's trail: a tool it used, or a note about what it did.</summary>
public sealed record ActivityLine(string Who, string Text, bool IsTool, string Time)
{
    public const string ToolPrefix = "Tool: ";

    public string Glyph => IsTool ? "\uE90F" : "\uE946";

    public static ActivityLine From(ActivityItem item, string who)
    {
        var isTool = item.Message.StartsWith(ToolPrefix, StringComparison.Ordinal);
        return new ActivityLine(who, isTool ? item.Message[ToolPrefix.Length..] : item.Message, isTool,
            item.CreatedAt.ToLocalTime().ToString("HH:mm:ss"));
    }
}

/// <summary>One step of an assignment's plan: who does it, what they were asked, how they did it and what they produced.</summary>
public sealed partial class SubtaskViewModel : ObservableObject
{
    private readonly Action<string, bool> _toggled;

    public SubtaskViewModel(TaskItem task, Agent? agent, IReadOnlyList<ActivityLine> activity, bool expanded, Action<string, bool> toggled)
    {
        Task = task;
        Agent = agent;
        Activity = activity;
        _isExpanded = expanded;
        _toggled = toggled;
    }

    public TaskItem Task { get; }
    public Agent? Agent { get; }
    public string Title => Task.Title;

    // Id/Name feed the shared avatar template (colour by id, initial from name).
    public string Id => Task.Assignee;
    public string Name => Agent?.Name ?? Task.Assignee;
    public string Role => Agent?.Title ?? "";
    public string Strengths => Agent?.Description ?? "";
    public string Brief => Task.Description.Trim();
    public string Status => Task.Status;
    public string StatusLabel => StatusText.For(Task.Status);
    public string? Result => Task.Result;

    public IReadOnlyList<ActivityLine> Activity { get; }
    public bool HasActivity => Activity.Count > 0;
    public int ToolCount => Activity.Count(a => a.IsTool);
    public string ActivityTitle => ToolCount switch
    {
        0 => "Activity",
        1 => "Activity · 1 tool used",
        _ => $"Activity · {ToolCount} tools used",
    };

    [ObservableProperty] private bool _isExpanded;

    partial void OnIsExpandedChanged(bool value) => _toggled(Task.Id, value);
}

/// <summary>One message the staff looked at (email, Discord, Slack) and what became of it.</summary>
public sealed class TriageViewModel(TriageItem item)
{
    public string Status => item.Status;
    public string StatusLabel => StatusText.For(item.Status);
    public string Summary => string.IsNullOrWhiteSpace(item.Summary) ? "(no summary)" : item.Summary;
    public string Source => item.Source switch
    {
        "gmail" => "Email",
        "discord" => "Discord",
        "slack" => "Slack",
        "" => "Message",
        var s => char.ToUpperInvariant(s[0]) + s[1..],
    };
    public string When => item.UpdatedAt.ToLocalTime().ToString("ddd dd MMM, HH:mm");
}

public static class StatusText
{
    public static string For(string status) => status switch
    {
        "queued" => "Queued",
        "in_progress" => "Working",
        "delegated" => "With the team",
        "waiting_human" => "Needs you",
        "done" => "Done",
        "failed" => "Failed",
        "cancelled" => "Cancelled",
        "idle" => "Available",
        "working" => "Working",
        "waiting" => "Waiting on you",
        "offline" => "Offline",
        "proposed" => "Waiting for you",
        "approved" => "Approved",
        "dismissed" => "Skipped",
        _ => status,
    };
}
