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
/// Each page with its own logic has a view model (Agents, Tools, Chat,
/// Automations, AI settings); this one connects them. All members run on the UI thread.
/// </summary>
public sealed partial class MainViewModel : ObservableObject, IDisposable
{
    public const string RepoUrl = "https://github.com/Pro-Sifat-Hasan/cubicle";

    // Navigation rail order (MainWindow.xaml).
    public const int TodaySection = 0, InboxSection = 1, AgentsSection = 2, AutomationsSection = 3,
        ToolsSection = 4, MemorySection = 5, ActivitySection = 6, SettingsSection = 7;

    private readonly Uri _gatewayUri;
    private readonly CancellationTokenSource _life = new();
    private GatewayClient? _client;
    private Catalog? _catalog;
    private bool _refreshQueued;

    /// <summary>Raised for things worth a desktop notification (title, message).</summary>
    public event Action<string, string>? Notify;

    public MainViewModel(string theme = ThemeService.SystemMode)
    {
        var url = Environment.GetEnvironmentVariable("CUBICLE_GATEWAY_URL");
        _gatewayUri = new Uri(string.IsNullOrWhiteSpace(url) ? CubiclePaths.DefaultGatewayUrl : url);
        _startWithWindows = AutoStart.IsEnabled;
        _theme = theme;
        void ShowError(string message) => ErrorText = message;
        void Notice(string title, string message) => Notify?.Invoke(title, message);
        Ai = new AiSettingsViewModel(() => _client, ShowError, Notice);
        AgentsPage = new AgentsViewModel(() => _client, ShowError, Notice);
        Tools = new ToolsViewModel(() => _client, ShowError, Notice);
        Chat = new ChatViewModel(() => _client, ShowError);
        Automations = new AutomationsViewModel(() => _client, ShowError, Notice);
        AgentsPage.Changed += QueueRefresh;
        Tools.Changed += QueueRefresh;
        Automations.Changed += QueueRefresh;
        AgentsPage.ChatRequested += id =>
        {
            SelectedSection = InboxSection;
            _ = Chat.OpenChatWithAsync(id);
        };
    }

    public AiSettingsViewModel Ai { get; }
    public AgentsViewModel AgentsPage { get; }
    public ToolsViewModel Tools { get; }
    public ChatViewModel Chat { get; }
    public AutomationsViewModel Automations { get; }

    public ObservableCollection<Agent> Staff { get; } = [];
    public ObservableCollection<AssignmentViewModel> Assignments { get; } = [];
    public ObservableCollection<SubtaskViewModel> SelectedSubtasks { get; } = [];
    public ObservableCollection<InboxItemViewModel> Inbox { get; } = [];
    public ObservableCollection<ActivityItem> Activity { get; } = [];
    public ObservableCollection<MemoryViewModel> Memories { get; } = [];
    public ObservableCollection<AgentChoice> AssignTargets { get; } = [];

    // ---- connection / status ----

    [ObservableProperty] private bool _isConnected;
    [ObservableProperty] private string _connectionText = "Starting your office…";
    [ObservableProperty] private bool _breakerOpen;
    [ObservableProperty] private string _usageText = "";
    [ObservableProperty] private int _selectedSection;
    [ObservableProperty] private string? _errorText;

    public int OpenQuestions => Inbox.Count;

    [ObservableProperty] private int _activeCount;
    [ObservableProperty] private int _doneCount;

    public string Greeting
    {
        get
        {
            var hour = DateTime.Now.Hour;
            var part = hour < 12 ? "Good morning" : hour < 18 ? "Good afternoon" : "Good evening";
            var name = Ai.OwnerName.Trim();
            return string.IsNullOrEmpty(name) ? part : $"{part}, {name}";
        }
    }

    public string MainAgentName => Staff.FirstOrDefault(a => a.IsMain)?.Name ?? "Your main agent";

    public string VersionText { get; } =
        $"Cubicle {typeof(MainViewModel).Assembly.GetName().Version?.ToString(3)} · MIT licensed";

    // ---- assignment composer ----

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(AssignCommand))]
    private string _newTitle = "";

    [ObservableProperty] private string _newDescription = "";

    /// <summary>Who gets the assignment: the main agent (empty id) or one agent directly.</summary>
    [ObservableProperty] private AgentChoice? _assignTo;

    // ---- memory composer ----

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(AddMemoryCommand))]
    private string _newMemory = "";

    // ---- selected assignment ----

    [ObservableProperty] private AssignmentViewModel? _selectedAssignment;
    [ObservableProperty] private TaskDetail? _selectedDetail;

    // ---- settings & layout ----

    [ObservableProperty] private bool _startWithWindows;

    /// <summary>Appearance: "System", "Light" or "Dark".</summary>
    [ObservableProperty] private string _theme;

    /// <summary>True when the window is narrow: the navigation rail collapses to icons.</summary>
    [ObservableProperty] private bool _isCompact;

    partial void OnThemeChanged(string value)
    {
        ThemeService.Apply(value);
        new UiPreferences(value).Save();
        OnPropertyChanged(nameof(ThemeGlyph));
        OnPropertyChanged(nameof(ThemeTooltip));
    }

    public string ThemeGlyph => Theme switch
    {
        ThemeService.LightMode => "",
        ThemeService.DarkMode => "",
        _ => "",
    };

    public string ThemeTooltip => $"Theme: {Theme} (click to change)";

    [RelayCommand]
    private void CycleTheme() => Theme = Theme switch
    {
        ThemeService.LightMode => ThemeService.DarkMode,
        ThemeService.DarkMode => ThemeService.SystemMode,
        _ => ThemeService.LightMode,
    };

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
                _catalog = await _client.GetCatalogAsync(ct);
                await RefreshAllAsync();
                await Ai.LoadAsync();
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
            case "task.progress":
                Chat.OnProgress(evt.Data);
                return; // frequent and cosmetic: no full refresh
            case "conversation.updated":
                Chat.OnConversationUpdated(evt.Data);
                break;
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
        if (_client is null || _catalog is null)
            return;
        var c = _client;
        var statusTask = c.GetStatusAsync();
        var agentsTask = c.GetAgentsAsync();
        var assignmentsTask = c.GetAssignmentsAsync(100);
        var inboxTask = c.GetInboxAsync("open");
        var routinesTask = c.GetRoutinesAsync();
        var activityTask = c.GetActivityAsync(150);
        var memoriesTask = c.GetMemoriesAsync();
        var conversationsTask = c.GetConversationsAsync();
        var serversTask = c.GetMcpServersAsync();
        var skillsTask = c.GetSkillsAsync();
        var pluginsTask = c.GetPluginsAsync();
        var secretsTask = c.GetSecretNamesAsync();
        await Task.WhenAll(statusTask, agentsTask, assignmentsTask, inboxTask, routinesTask, activityTask, memoriesTask,
            conversationsTask, serversTask, skillsTask, pluginsTask, secretsTask);

        var status = statusTask.Result;
        BreakerOpen = status.BreakerOpen;
        ActiveCount = assignmentsTask.Result.Count(t => !t.IsFinished && t.Status != "waiting_human");
        DoneCount = assignmentsTask.Result.Count(t => t.Status == "done");
        OnPropertyChanged(nameof(Greeting));
        UsageText = status.DailyTokenBudget > 0
            ? $"{status.TokensToday:N0} / {status.DailyTokenBudget:N0} tokens today"
            : $"{status.TokensToday:N0} tokens today";

        var agents = agentsTask.Result;
        Replace(Staff, agents);
        OnPropertyChanged(nameof(MainAgentName));
        var names = agents.ToDictionary(e => e.Id, e => e.Name);
        UpdateAssignTargets(agents);

        AgentsPage.Load(agents, _catalog, serversTask.Result, skillsTask.Result);
        Tools.Load(_catalog, serversTask.Result, skillsTask.Result, pluginsTask.Result, agents, secretsTask.Result);
        Chat.Load(conversationsTask.Result, agents);
        Automations.Load(routinesTask.Result, agents);

        var selectedId = SelectedAssignment?.Task.Id;
        Replace(Assignments, assignmentsTask.Result.Select(t => new AssignmentViewModel(t, names.GetValueOrDefault(t.Assignee, t.Assignee))));
        SelectedAssignment = Assignments.FirstOrDefault(a => a.Task.Id == selectedId) ?? SelectedAssignment;

        // Keep half-typed answers when the inbox refreshes.
        var drafts = Inbox.ToDictionary(i => i.Item.Id, i => i.AnswerText);
        Replace(Inbox, inboxTask.Result.Select(i => new InboxItemViewModel(i, names.GetValueOrDefault(i.EmployeeId, i.EmployeeId), SendAnswerAsync)
        {
            AnswerText = drafts.GetValueOrDefault(i.Id, ""),
        }));
        OnPropertyChanged(nameof(OpenQuestions));

        Replace(Activity, activityTask.Result);
        Replace(Memories, memoriesTask.Result.Select(m => new MemoryViewModel(m,
            string.IsNullOrEmpty(m.EmployeeId) ? "Added by you" : $"Learned by {names.GetValueOrDefault(m.EmployeeId, m.EmployeeId)}",
            DeleteMemoryAsync)));

        if (SelectedAssignment is not null)
            await LoadDetailAsync(SelectedAssignment.Task.Id, names);
    }

    private void UpdateAssignTargets(IReadOnlyList<Agent> agents)
    {
        var pick = AssignTo?.Id ?? "";
        var main = agents.FirstOrDefault(a => a.IsMain);
        AssignTargets.Clear();
        AssignTargets.Add(new AgentChoice("", $"{main?.Name ?? "Main agent"} · plans it with the team"));
        foreach (var a in agents.Where(a => a.Enabled && !a.IsMain))
            AssignTargets.Add(new AgentChoice(a.Id, $"{a.Name} · {(string.IsNullOrEmpty(a.Title) ? "Agent" : a.Title)} (alone)"));
        AssignTo = AssignTargets.FirstOrDefault(t => t.Id == pick) ?? AssignTargets[0];
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
        var target = string.IsNullOrEmpty(AssignTo?.Id) ? null : AssignTo.Id;
        var task = await _client!.CreateAssignmentAsync(NewTitle.Trim(), NewDescription.Trim(), target);
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

    private bool CanAddMemory() => !string.IsNullOrWhiteSpace(NewMemory);

    [RelayCommand(CanExecute = nameof(CanAddMemory))]
    private Task AddMemoryAsync() => SafeAsync(async () =>
    {
        await _client!.AddMemoryAsync(NewMemory.Trim());
        NewMemory = "";
        await RefreshAllAsync();
    });

    private Task DeleteMemoryAsync(MemoryViewModel m) => SafeAsync(async () =>
    {
        await _client!.DeleteMemoryAsync(m.Memory.Id);
        Memories.Remove(m);
    });

    [RelayCommand]
    private Task ResetBreakerAsync() => SafeAsync(() => _client!.ResetBreakerAsync());

    [RelayCommand]
    private void OpenDataFolder() => Open(CubiclePaths.Home);

    /// <summary>Opens the folder where the staff saved files for the selected assignment.</summary>
    [RelayCommand]
    private void OpenAssignmentFiles()
    {
        if (SelectedAssignment is null)
            return;
        OpenWorkspace(SelectedAssignment.Task.Id, "No files were saved for this assignment.");
    }

    /// <summary>Opens the folder where an agent saved files during the open chat.</summary>
    [RelayCommand]
    private void OpenChatFiles()
    {
        if (Chat.Selected is { } c)
            OpenWorkspace(c.Conversation.Id, "No files were saved in this chat yet.");
    }

    private void OpenWorkspace(string id, string missing)
    {
        var folder = Path.Combine(CubiclePaths.WorkspaceDirectory, id);
        if (Directory.Exists(folder))
            Open(folder);
        else
            ErrorText = missing;
    }

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
