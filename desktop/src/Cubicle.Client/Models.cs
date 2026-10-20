using System.Text.Json;

namespace Cubicle.Client;

/// <summary>An AI employee as reported by the gateway.</summary>
public sealed record Employee(
    string Id,
    string Name,
    string Title,
    string Role,
    string Avatar,
    string Model,
    string Skills,
    string Status,
    string? CurrentTaskId,
    DateTimeOffset UpdatedAt)
{
    public bool IsManager => Role == "manager";
}

/// <summary>A unit of work: an assignment from you, a delegated subtask, or the manager's wrap-up.</summary>
public sealed record TaskItem(
    string Id,
    string? ParentId,
    string Kind,
    string Title,
    string Description,
    string Assignee,
    string Status,
    string? Result,
    string? Error,
    IReadOnlyList<string> DependsOn,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt)
{
    public bool IsAssignment => Kind == "assignment";
    public bool IsFinished => Status is "done" or "failed" or "cancelled";
}

public sealed record TaskDetail(
    string Id,
    string? ParentId,
    string Kind,
    string Title,
    string Description,
    string Assignee,
    string Status,
    string? Result,
    string? Error,
    IReadOnlyList<TaskItem> Children,
    IReadOnlyList<InboxItem> Inbox);

/// <summary>A question one of your employees asked you.</summary>
public sealed record InboxItem(
    string Id,
    string TaskId,
    string EmployeeId,
    string Question,
    string? Answer,
    string Status,
    DateTimeOffset CreatedAt);

/// <summary>A standing order that runs on a schedule.</summary>
public sealed record Routine(
    string Id,
    string Title,
    string Description,
    string Schedule,
    bool Enabled,
    DateTimeOffset NextRunAt,
    DateTimeOffset? LastRunAt,
    string? LastTaskId);

public sealed record ActivityItem(long Id, string? EmployeeId, string? TaskId, string Message, DateTimeOffset CreatedAt);

public sealed record OfficeStatus(
    string Version,
    IReadOnlyDictionary<string, int> Tasks,
    int OpenQuestions,
    bool BreakerOpen,
    long TokensToday,
    long DailyTokenBudget);

/// <summary>A live event from the gateway's SSE stream.</summary>
public sealed record GatewayEvent(string Type, JsonElement Data, DateTimeOffset At);
