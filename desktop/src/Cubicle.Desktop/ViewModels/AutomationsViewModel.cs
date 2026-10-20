using System.Collections.ObjectModel;
using System.Globalization;
using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// The Automations page: work your agents do on a schedule. Sent to the main
/// agent, an automation is planned with the team; sent to any other agent, that
/// agent does it alone.
/// </summary>
public sealed partial class AutomationsViewModel : ObservableObject
{
    private readonly Func<GatewayClient?> _client;
    private readonly Action<string> _showError;
    private readonly Action<string, string> _notify;

    public AutomationsViewModel(Func<GatewayClient?> client, Action<string> showError, Action<string, string> notify)
    {
        _client = client;
        _showError = showError;
        _notify = notify;
        foreach (var d in new[] { DayOfWeek.Monday, DayOfWeek.Tuesday, DayOfWeek.Wednesday, DayOfWeek.Thursday,
                     DayOfWeek.Friday, DayOfWeek.Saturday, DayOfWeek.Sunday })
            Days.Add(new OptionItem(((int)d).ToString(CultureInfo.InvariantCulture), CultureInfo.InvariantCulture.DateTimeFormat.GetAbbreviatedDayName(d))
            { IsChecked = d is >= DayOfWeek.Monday and <= DayOfWeek.Friday });
    }

    /// <summary>Raised after a change, so the rest of the app refreshes.</summary>
    public event Action? Changed;

    public ObservableCollection<RoutineViewModel> Routines { get; } = [];
    public ObservableCollection<AgentChoice> AgentChoices { get; } = [];
    public ObservableCollection<OptionItem> Days { get; } = [];
    public IReadOnlyList<string> RepeatKinds { get; } = ["Every weekday", "Every day", "Weekly on…", "Every N minutes", "Once"];

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(SaveCommand))]
    private string _title = "";

    [ObservableProperty] private string _description = "";
    [ObservableProperty] private AgentChoice? _agent;

    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(ShowTime), nameof(ShowDays), nameof(ShowInterval), nameof(ShowDate))]
    private int _repeatKind;

    [ObservableProperty] private string _time = "09:00";
    [ObservableProperty] private string _everyMinutes = "60";
    [ObservableProperty] private DateTime? _onceDate = DateTime.Today.AddDays(1);

    /// <summary>The automation being edited, or null when adding a new one.</summary>
    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(IsEditing), nameof(SaveLabel), nameof(FormTitle))]
    private string? _editingId;

    public bool ShowTime => RepeatKind != 3;
    public bool ShowDays => RepeatKind == 2;
    public bool ShowInterval => RepeatKind == 3;
    public bool ShowDate => RepeatKind == 4;
    public bool IsEditing => EditingId is not null;
    public string SaveLabel => IsEditing ? "Save changes" : "Add automation";
    public string FormTitle => IsEditing ? "Edit automation" : "New automation";

    public void Load(IReadOnlyList<Routine> routines, IReadOnlyList<Agent> agents)
    {
        var names = agents.ToDictionary(a => a.Id, a => a.Name);
        var main = agents.FirstOrDefault(a => a.IsMain);
        Routines.Clear();
        foreach (var r in routines)
        {
            var who = string.IsNullOrEmpty(r.AgentId) || r.AgentId == main?.Id
                ? $"{main?.Name ?? "Main agent"} plans it with the team"
                : $"{names.GetValueOrDefault(r.AgentId, r.AgentId)} does it";
            Routines.Add(new RoutineViewModel(r, who, ToggleAsync, DeleteAsync, Edit, RunNowAsync));
        }
        var pick = Agent?.Id ?? "";
        AgentChoices.Clear();
        AgentChoices.Add(new AgentChoice("", $"{main?.Name ?? "Main agent"} (main) · plans it with the team"));
        foreach (var a in agents.Where(a => a.Enabled && !a.IsMain))
            AgentChoices.Add(new AgentChoice(a.Id, $"{a.Name} · {(string.IsNullOrEmpty(a.Title) ? "Agent" : a.Title)}"));
        Agent = AgentChoices.FirstOrDefault(a => a.Id == pick) ?? AgentChoices[0];
    }

    private string BuildSchedule()
    {
        if (RepeatKind == 3)
        {
            if (!int.TryParse(EveryMinutes, out var minutes) || minutes < 5)
                throw new FormatException("Interval must be a whole number of minutes, at least 5.");
            return Schedules.Every(TimeSpan.FromMinutes(minutes));
        }
        if (!TimeOnly.TryParse(Time, CultureInfo.InvariantCulture, out var at))
            throw new FormatException("Time must look like 09:00.");
        return RepeatKind switch
        {
            0 => Schedules.Weekdays(at),
            1 => Schedules.Daily(at),
            2 => Schedules.Weekly(Days.Where(d => d.IsChecked).Select(d => (DayOfWeek)int.Parse(d.Id, CultureInfo.InvariantCulture)), at),
            _ => OnceDate is { } date
                ? Schedules.Once(date.Date + at.ToTimeSpan())
                : throw new FormatException("Pick a date."),
        };
    }

    private bool CanSave() => !string.IsNullOrWhiteSpace(Title);

    [RelayCommand(CanExecute = nameof(CanSave))]
    private Task SaveAsync() => Run(async c =>
    {
        var schedule = BuildSchedule();
        var agentId = Agent?.Id ?? "";
        var saved = EditingId is { } id
            ? await c.UpdateRoutineAsync(id, Title.Trim(), Description.Trim(), schedule, agentId)
            : await c.CreateRoutineAsync(Title.Trim(), Description.Trim(), schedule, agentId);
        _notify(IsEditing ? "Automation saved" : "Automation added",
            $"{saved.Title}: {Schedules.Describe(saved.Schedule)}. Next run {saved.NextRunAt.ToLocalTime():ddd dd MMM, HH:mm}.");
        ClearForm();
        Changed?.Invoke();
    });

    [RelayCommand]
    private void CancelEdit() => ClearForm();

    private void ClearForm()
    {
        EditingId = null;
        Title = Description = "";
    }

    private void Edit(RoutineViewModel item)
    {
        var r = item.Routine;
        EditingId = r.Id;
        Title = r.Title;
        Description = r.Description;
        Agent = AgentChoices.FirstOrDefault(a => a.Id == r.AgentId) ?? AgentChoices.FirstOrDefault();
        var s = r.Schedule.ToLowerInvariant();
        var parts = s.Split('@');
        if (s.StartsWith("every:", StringComparison.Ordinal))
        {
            RepeatKind = 3;
            var rest = s[6..];
            EveryMinutes = rest.EndsWith('h') && int.TryParse(rest[..^1], out var h) ? (h * 60).ToString(CultureInfo.InvariantCulture)
                : rest.TrimEnd('m');
        }
        else if (parts[0] == "once" && DateTime.TryParseExact(parts[1], "yyyy-MM-dd't'HH:mm", CultureInfo.InvariantCulture, DateTimeStyles.None, out var at))
        {
            RepeatKind = 4;
            OnceDate = at.Date;
            Time = at.ToString("HH:mm", CultureInfo.InvariantCulture);
        }
        else if (parts[0] == "weekly" && parts.Length == 3)
        {
            RepeatKind = 2;
            var codes = parts[1].Split(',');
            foreach (var d in Days)
                d.IsChecked = codes.Contains(d.Label.ToLowerInvariant());
            Time = parts[2];
        }
        else if (parts.Length == 2)
        {
            RepeatKind = parts[0] == "daily" ? 1 : 0;
            Time = parts[1];
        }
    }

    private Task RunNowAsync(RoutineViewModel item) => Run(async c =>
    {
        await c.RunRoutineNowAsync(item.Routine.Id);
        _notify("Automation started", $"{item.Routine.Title} is running now. Follow it on the Today page.");
        Changed?.Invoke();
    });

    private Task ToggleAsync(RoutineViewModel item, bool enabled) => Run(async c =>
    {
        await c.SetRoutineEnabledAsync(item.Routine.Id, enabled);
        Changed?.Invoke();
    });

    private Task DeleteAsync(RoutineViewModel item) => Run(async c =>
    {
        await c.DeleteRoutineAsync(item.Routine.Id);
        Routines.Remove(item);
        if (EditingId == item.Routine.Id)
            ClearForm();
    });

    private async Task Run(Func<GatewayClient, Task> action)
    {
        if (_client() is not { } c)
        {
            _showError("Not connected to your office yet.");
            return;
        }
        try
        {
            await action(c);
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException or FormatException or ArgumentException or OperationCanceledException)
        {
            _showError(e.Message);
            if (e is GatewayException)
                Changed?.Invoke(); // e.g. a toggle was refused: show the real state again
        }
    }
}
