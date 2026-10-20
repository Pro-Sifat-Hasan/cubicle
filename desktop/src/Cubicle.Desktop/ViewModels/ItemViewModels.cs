using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>A question from an employee, with an editable answer.</summary>
public sealed partial class InboxItemViewModel(InboxItem item, string employeeName, Func<InboxItemViewModel, Task> send) : ObservableObject
{
    public InboxItem Item { get; } = item;
    public string EmployeeName { get; } = employeeName;
    public string Asked => Item.CreatedAt.ToLocalTime().ToString("ddd HH:mm");

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(SendCommand))]
    private string _answerText = "";

    private bool CanSend() => !string.IsNullOrWhiteSpace(AnswerText);

    [RelayCommand(CanExecute = nameof(CanSend))]
    private Task SendAsync() => send(this);
}

public sealed partial class RoutineViewModel(Routine routine, Func<RoutineViewModel, bool, Task> toggle, Func<RoutineViewModel, Task> delete) : ObservableObject
{
    public Routine Routine { get; } = routine;
    public string When => Schedules.Describe(Routine.Schedule);
    public string NextRun => Routine.Enabled ? $"Next: {Routine.NextRunAt.ToLocalTime():ddd dd MMM, HH:mm}" : "Paused";
    public string LastRun => Routine.LastRunAt is { Year: > 1 } last ? $"Last: {last.ToLocalTime():ddd dd MMM, HH:mm}" : "Never run yet";

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

public sealed class AssignmentViewModel(TaskItem task)
{
    public TaskItem Task { get; } = task;
    public string Title => Task.Title;
    public string Status => Task.Status;
    public string StatusLabel => StatusText.For(Task.Status);
    public string When => Task.CreatedAt.ToLocalTime().ToString("ddd dd MMM, HH:mm");
}

public sealed class SubtaskViewModel(TaskItem task, string assigneeName)
{
    public TaskItem Task { get; } = task;
    public string Title => Task.Title;
    public string Assignee { get; } = assigneeName;
    public string Status => Task.Status;
    public string StatusLabel => StatusText.For(Task.Status);
    public string? Result => Task.Result;
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
        _ => status,
    };
}
