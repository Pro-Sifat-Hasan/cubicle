using System.Collections.ObjectModel;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;
using Cubicle.Desktop.Services;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// State for the whole window. Keeps a live connection to the gateway: initial
/// load, then incremental refreshes driven by the gateway's event stream.
/// All members run on the UI thread.
/// </summary>
public sealed partial class MainViewModel : ObservableObject, IDisposable
{
    public const string RepoUrl = "https://github.com/Pro-Sifat-Hasan/cubicle";

    private readonly Uri _gatewayUri;
    private readonly CancellationTokenSource _life = new();
    private GatewayClient? _client;
    private bool _refreshQueued;

    /// <summary>Raised for things worth a desktop notification (title, message).</summary>
    public event Action<string, string>? Notify;

    public MainViewModel()
    {
        var url = Environment.GetEnvironmentVariable("CUBICLE_GATEWAY_URL");
        _gatewayUri = new Uri(string.IsNullOrWhiteSpace(url) ? CubiclePaths.DefaultGatewayUrl : url);
        _startWithWindows = AutoStart.IsEnabled;
    }

    public ObservableCollection<Employee> Staff { get; } = [];
    public ObservableCollection<AssignmentViewModel> Assignments { get; } = [];
    public ObservableCollection<SubtaskViewModel> SelectedSubtasks { get; } = [];
    public ObservableCollection<InboxItemViewModel> Inbox { get; } = [];
    public ObservableCollection<RoutineViewModel> Routines { get; } = [];
    public ObservableCollection<ActivityItem> Activity { get; } = [];
    public ObservableCollection<string> ConfiguredSecrets { get; } = [];

    public IReadOnlyList<string> RoutineKinds { get; } = ["Every weekday", "Every day", "Every N minutes"];

    // ---- connection / status ----

    [ObservableProperty] private bool _isConnected;
    [ObservableProperty] private string _connectionText = "Starting your office…";
    [ObservableProperty] private bool _breakerOpen;
    [ObservableProperty] private string _usageText = "";
    [ObservableProperty] private bool _needsApiKey;
    [ObservableProperty] private int _selectedSection;
    [ObservableProperty] private string? _errorText;

    public int OpenQuestions => Inbox.Count;

    // ---- assignment composer ----

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(AssignCommand))]
    private string _newTitle = "";

    [ObservableProperty] private string _newDescription = "";

    // ---- selected assignment ----

    [ObservableProperty] private AssignmentViewModel? _selectedAssignment;
    [ObservableProperty] private TaskDetail? _selectedDetail;

    // ---- routine composer ----

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(CreateRoutineCommand))]
    private string _routineTitle = "";

    [ObservableProperty] private string _routineDescription = "";
    [ObservableProperty] private int _routineKind;
    [ObservableProperty] private string _routineTime = "09:00";
    [ObservableProperty] private string _routineEveryMinutes = "60";

    // ---- settings ----

    [ObservableProperty] private bool _startWithWindows;

    public string DataFolder => CubiclePaths.Home;

    public void Start() => _ = RunConnectionLoopAsync(_life.Token);

    private async Task RunConnectionLoopAsync(CancellationToken ct)
    {
        while (!ct.IsCancellationRequested)
        {
            try
            {
                ConnectionText = "Starting your office…";
                if (!await GatewayHost.EnsureRunningAsync(_gatewayUri, ct))
                {
                    ConnectionText = "Gateway not running. Reinstall Cubicle or start cubicle-gateway.";
                    IsConnected = false;
                    await Task.Delay(5000, ct);
                    continue;
                }
                var token = CubiclePaths.TryReadAdminToken();
                if (token is null)
                {
                    ConnectionText = $"Waiting for {CubiclePaths.AdminTokenFile}…";
                    await Task.Delay(2000, ct);
                    continue;
                }

                _client?.Dispose();
                _client = new GatewayClient(_gatewayUri, token);
                await RefreshAllAsync();
                IsConnected = true;
                ConnectionText = "Office open";

                await foreach (var evt in _client.StreamEventsAsync(ct))
                    HandleEvent(evt);
            }
            catch (OperationCanceledException) when (ct.IsCancellationRequested)
            {
                return;
            }
            catch (Exception e) when (e is HttpRequestException or GatewayException or IOException or OperationCanceledException)
            {
                Debug.WriteLine($"gateway connection lost: {e.Message}");
            }
            IsConnected = false;
            ConnectionText = "Reconnecting…";
            try { await Task.Delay(3000, ct); } catch (OperationCanceledException) { return; }
        }
    }

    private void HandleEvent(GatewayEvent evt)
    {
        switch (evt.Type)
        {
            case "assignment.done":
                Notify?.Invoke("Done ✅", evt.Data.GetProperty("title").GetString() ?? "An assignment is finished.");
                break;
            case "inbox.new":
                Notify?.Invoke("Your staff have a question", evt.Data.GetProperty("question").GetString() ?? "");
                break;
            case "breaker.tripped":
                Notify?.Invoke("Work paused", "Daily token budget reached. Open Cubicle to resume.");
                break;
        }
        QueueRefresh();
    }

    /// <summary>Coalesces bursts of events into one refresh.</summary>
    private async void QueueRefresh()
    {
        if (_refreshQueued)
            return;
        _refreshQueued = true;
        try
        {
            await Task.Delay(250);
            await RefreshAllAsync();
        }
        catch (Exception e) when (e is HttpRequestException or GatewayException or OperationCanceledException)
        {
            Debug.WriteLine($"refresh failed: {e.Message}");
        }
        finally
        {
            _refreshQueued = false;
        }
    }

    private async Task RefreshAllAsync()
    {
        if (_client is null)
            return;
        var c = _client;
        var statusTask = c.GetStatusAsync();
        var staffTask = c.GetEmployeesAsync();
        var assignmentsTask = c.GetAssignmentsAsync(100);
        var inboxTask = c.GetInboxAsync("open");
        var routinesTask = c.GetRoutinesAsync();
        var activityTask = c.GetActivityAsync(150);
        var secretsTask = c.GetSecretNamesAsync();
        await Task.WhenAll(statusTask, staffTask, assignmentsTask, inboxTask, routinesTask, activityTask, secretsTask);

        var status = statusTask.Result;
        BreakerOpen = status.BreakerOpen;
        UsageText = status.DailyTokenBudget > 0
            ? $"{status.TokensToday:N0} / {status.DailyTokenBudget:N0} tokens today"
            : $"{status.TokensToday:N0} tokens today";

        Replace(Staff, staffTask.Result);
        var names = staffTask.Result.ToDictionary(e => e.Id, e => e.Name);

        var selectedId = SelectedAssignment?.Task.Id;
        Replace(Assignments, assignmentsTask.Result.Select(t => new AssignmentViewModel(t)));
        SelectedAssignment = Assignments.FirstOrDefault(a => a.Task.Id == selectedId) ?? SelectedAssignment;

        // Keep half-typed answers when the inbox refreshes.
        var drafts = Inbox.ToDictionary(i => i.Item.Id, i => i.AnswerText);
        Replace(Inbox, inboxTask.Result.Select(i => new InboxItemViewModel(i, names.GetValueOrDefault(i.EmployeeId, i.EmployeeId), SendAnswerAsync)
        {
            AnswerText = drafts.GetValueOrDefault(i.Id, ""),
        }));
        OnPropertyChanged(nameof(OpenQuestions));

        Replace(Routines, routinesTask.Result.Select(r => new RoutineViewModel(r, ToggleRoutineAsync, DeleteRoutineAsync)));
        Replace(Activity, activityTask.Result);
        Replace(ConfiguredSecrets, secretsTask.Result);
        NeedsApiKey = !secretsTask.Result.Contains("ANTHROPIC_API_KEY");

        if (SelectedAssignment is not null)
            await LoadDetailAsync(SelectedAssignment.Task.Id, names);
    }

    partial void OnSelectedAssignmentChanged(AssignmentViewModel? value)
    {
        if (value is null)
        {
            SelectedDetail = null;
            SelectedSubtasks.Clear();
            return;
        }
        _ = SafeAsync(async () => await LoadDetailAsync(value.Task.Id, Staff.ToDictionary(e => e.Id, e => e.Name)));
    }

    private async Task LoadDetailAsync(string id, Dictionary<string, string> names)
    {
        if (_client is null)
            return;
        var detail = await _client.GetTaskAsync(id);
        SelectedDetail = detail;
        Replace(SelectedSubtasks, detail.Children
            .OrderBy(t => t.CreatedAt)
            .Select(t => new SubtaskViewModel(t, names.GetValueOrDefault(t.Assignee, t.Assignee))));
    }

    // ---- commands ----

    private bool CanAssign() => !string.IsNullOrWhiteSpace(NewTitle);

    [RelayCommand(CanExecute = nameof(CanAssign))]
    private Task AssignAsync() => SafeAsync(async () =>
    {
        var task = await _client!.CreateAssignmentAsync(NewTitle.Trim(), NewDescription.Trim());
        NewTitle = NewDescription = "";
        await RefreshAllAsync();
        SelectedAssignment = Assignments.FirstOrDefault(a => a.Task.Id == task.Id);
    });

    [RelayCommand]
    private Task CancelSelectedAsync() => SafeAsync(async () =>
    {
        if (SelectedAssignment is { } a && !a.Task.IsFinished)
            await _client!.CancelTaskAsync(a.Task.Id);
    });

    private Task SendAnswerAsync(InboxItemViewModel item) => SafeAsync(async () =>
    {
        await _client!.AnswerAsync(item.Item.Id, item.AnswerText.Trim());
        Inbox.Remove(item);
        OnPropertyChanged(nameof(OpenQuestions));
    });

    private bool CanCreateRoutine() => !string.IsNullOrWhiteSpace(RoutineTitle);

    [RelayCommand(CanExecute = nameof(CanCreateRoutine))]
    private Task CreateRoutineAsync() => SafeAsync(async () =>
    {
        string schedule;
        if (RoutineKind == 2)
        {
            if (!int.TryParse(RoutineEveryMinutes, out var minutes) || minutes < 5)
                throw new FormatException("Interval must be a whole number of minutes, at least 5.");
            schedule = Schedules.Every(TimeSpan.FromMinutes(minutes));
        }
        else
        {
            if (!TimeOnly.TryParse(RoutineTime, out var at))
                throw new FormatException("Time must look like 09:00.");
            schedule = RoutineKind == 0 ? Schedules.Weekdays(at) : Schedules.Daily(at);
        }
        await _client!.CreateRoutineAsync(RoutineTitle.Trim(), RoutineDescription.Trim(), schedule);
        RoutineTitle = RoutineDescription = "";
        await RefreshAllAsync();
    });

    private Task ToggleRoutineAsync(RoutineViewModel r, bool enabled) => SafeAsync(async () =>
    {
        await _client!.SetRoutineEnabledAsync(r.Routine.Id, enabled);
        await RefreshAllAsync();
    });

    private Task DeleteRoutineAsync(RoutineViewModel r) => SafeAsync(async () =>
    {
        await _client!.DeleteRoutineAsync(r.Routine.Id);
        Routines.Remove(r);
    });

    /// <summary>Called from the view with the PasswordBox contents (never bound or stored in memory longer than needed).</summary>
    public Task SaveApiKeyAsync(string key) => SafeAsync(async () =>
    {
        if (string.IsNullOrWhiteSpace(key))
            throw new FormatException("Paste your Anthropic API key first.");
        await _client!.SetSecretAsync("ANTHROPIC_API_KEY", key.Trim());
        await RefreshAllAsync();
        Notify?.Invoke("API key saved", "Your staff are restarting with real model access.");
    });

    [RelayCommand]
    private Task ResetBreakerAsync() => SafeAsync(() => _client!.ResetBreakerAsync());

    [RelayCommand]
    private void OpenDataFolder() => Open(CubiclePaths.Home);

    [RelayCommand]
    private void OpenRepo() => Open(RepoUrl);

    [RelayCommand]
    private void Navigate(string index) => SelectedSection = int.Parse(index);

    partial void OnStartWithWindowsChanged(bool value) => AutoStart.Set(value);

    [RelayCommand]
    private void DismissError() => ErrorText = null;

    private static void Open(string target) =>
        Process.Start(new ProcessStartInfo(target) { UseShellExecute = true });

    private async Task SafeAsync(Func<Task> action)
    {
        if (_client is null)
        {
            ErrorText = "Not connected to your office yet.";
            return;
        }
        try
        {
            ErrorText = null;
            await action();
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException or FormatException or OperationCanceledException)
        {
            ErrorText = e.Message;
        }
    }

    private static void Replace<T>(ObservableCollection<T> target, IEnumerable<T> items)
    {
        target.Clear();
        foreach (var item in items)
            target.Add(item);
    }

    public void Dispose()
    {
        _life.Cancel();
        _client?.Dispose();
        _life.Dispose();
    }
}
