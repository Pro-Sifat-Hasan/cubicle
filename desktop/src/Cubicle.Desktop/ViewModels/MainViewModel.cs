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
        Onboarding = new OnboardingViewModel(() => _client, ShowError);
        Chat.PropertyChanged += (_, e) =>
        {
            if (e.PropertyName == nameof(ChatViewModel.Selected) && Chat.Selected is not null)
                ShowRequests = false;
        };
        Onboarding.Finished += profile => _ = FinishOnboardingAsync(profile);
        Onboarding.MissionChosen += title => _ = RunMissionAsync(title);
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
    public OnboardingViewModel Onboarding { get; }

    public ObservableCollection<Agent> Staff { get; } = [];
    public ObservableCollection<AssignmentViewModel> Assignments { get; } = [];
    public ObservableCollection<SubtaskViewModel> SelectedSubtasks { get; } = [];
    public ObservableCollection<ActivityLine> SelectedActivity { get; } = [];
    public ObservableCollection<InboxItemViewModel> Inbox { get; } = [];
    public ObservableCollection<ActivityItem> Activity { get; } = [];
    public ObservableCollection<RequestHistoryViewModel> RequestHistory { get; } = [];
    public ObservableCollection<MemoryViewModel> Memories { get; } = [];
    public ObservableCollection<AgentChoice> AssignTargets { get; } = [];

    /// <summary>One-tap missions for the owner's profession, shown while there is no work yet.</summary>
    public ObservableCollection<string> StarterMissions { get; } = [];

    // ---- connection / status ----

    [ObservableProperty] private bool _isConnected;
    [ObservableProperty] private string _connectionText = "Starting your office…";
    [ObservableProperty] private bool _breakerOpen;
    [ObservableProperty] private string _usageText = "";
    [ObservableProperty] private int _selectedSection;
    [ObservableProperty] private string? _errorText;

    /// <summary>True while first-run setup covers the window.</summary>
    [ObservableProperty] private bool _showOnboarding;

    /// <summary>Who the owner is and how they set up their staff; null until the gateway answers.</summary>
    [ObservableProperty, NotifyPropertyChangedFor(nameof(Greeting))]
    private OwnerProfile? _profile;

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
            if (Profile is { Onboarded: true, Passion: { } persona })
            {
                var text = persona.Greet(string.IsNullOrEmpty(name) ? "there" : name, Profile.AgentName, DateTime.Now.DayOfYear);
                const string morning = "Good morning";
                return text.StartsWith(morning, StringComparison.Ordinal) ? part + text[morning.Length..] : text;
            }
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
                var client = new GatewayClient(_gatewayUri, token);
                _client = client;
                try
                {
                    _catalog = await client.GetCatalogAsync(ct);
                    await RefreshAllAsync();
                    await Ai.LoadAsync();
                    await LoadProfileAsync(ct);
                    IsConnected = true;
                    ConnectionText = "Office open";

                    await foreach (var evt in client.StreamEventsAsync(ct))
                        HandleEvent(evt);
                }
                catch
                {
                    client.Dispose(); // the loop connects again with a fresh client
                    if (ReferenceEquals(_client, client))
                        _client = null;
                    throw;
                }
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
            case "task.step":
                Chat.OnStep(evt.Data);
                return; // likewise: it only changes the timeline of the open chat
            case "conversation.updated":
                Chat.OnConversationUpdated(evt.Data);
                break;
            case "assignment.done":
                Notify?.Invoke("Done ✅", evt.Data.GetProperty("title").GetString() ?? "An assignment is finished.");
                break;
            case "inbox.new":
                Notify?.Invoke(evt.Data.TryGetProperty("kind", out var kind) && kind.GetString() == InboxItem.ApprovalKind
                    ? "Your staff need your approval" : "Your staff have a question",
                    (evt.Data.GetProperty("question").GetString() ?? "").Replace("**", ""));
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
        var inboxTask = c.GetInboxAsync(null); // open requests, and the answered ones for the history
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
        _restoringSelection = true; // refilling the list clears the list's selection; that is not the owner closing the panel
        try
        {
            Replace(Assignments, assignmentsTask.Result.OrderByDescending(t => t.CreatedAt)
                .Select(t => new AssignmentViewModel(t, names.GetValueOrDefault(t.Assignee, t.Assignee))));
            SelectedAssignment = Assignments.FirstOrDefault(a => a.Task.Id == selectedId) ?? SelectedAssignment;
        }
        finally
        {
            _restoringSelection = false;
        }
        OnPropertyChanged(nameof(Summary));

        // Keep half-typed answers when the inbox refreshes.
        var drafts = Inbox.ToDictionary(i => i.Item.Id, i => i.AnswerText);
        Replace(Inbox, inboxTask.Result.Where(i => i.Status == "open").Select(i => new InboxItemViewModel(i, names.GetValueOrDefault(i.EmployeeId, i.EmployeeId), SendAnswerAsync)
        {
            AnswerText = drafts.GetValueOrDefault(i.Id, ""),
        }));
        Replace(RequestHistory, inboxTask.Result.Where(i => i.Status == "answered").OrderByDescending(i => i.CreatedAt).Take(25)
            .Select(i => new RequestHistoryViewModel(i, names.GetValueOrDefault(i.EmployeeId, i.EmployeeId))));
        Chat.SetRequests(Inbox.ToList());
        Chat.RefreshLive();
        OnPropertyChanged(nameof(OpenQuestions));
        OnPropertyChanged(nameof(Summary));

        Replace(Activity, activityTask.Result);
        Replace(Memories, memoriesTask.Result.Select(m => new MemoryViewModel(m,
            string.IsNullOrEmpty(m.EmployeeId) ? "Added by you" : $"Learned by {names.GetValueOrDefault(m.EmployeeId, m.EmployeeId)}",
            DeleteMemoryAsync)));

        if (SelectedAssignment is not null)
            await LoadDetailAsync(SelectedAssignment.Task.Id);
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

    private bool _restoringSelection;
    private readonly HashSet<string> _expandedSteps = [];

    partial void OnSelectedAssignmentChanged(AssignmentViewModel? value)
    {
        if (value is null)
        {
            if (!_restoringSelection)
            {
                IsDetailOpen = false;
                SelectedDetail = null;
                SelectedSubtasks.Clear();
                SelectedActivity.Clear();
            }
            return;
        }
        IsDetailOpen = true;
        _ = SafeAsync(() => LoadDetailAsync(value.Task.Id));
    }

    /// <summary>Whether the assignment panel is shown. Closing it clears the selection, so clicking an assignment opens it again.</summary>
    [ObservableProperty, NotifyPropertyChangedFor(nameof(ShowDetail))]
    private bool _isDetailOpen;

    public bool ShowDetail => IsDetailOpen && SelectedDetail is not null;

    partial void OnSelectedDetailChanged(TaskDetail? value) => OnPropertyChanged(nameof(ShowDetail));

    [RelayCommand]
    private void CloseDetail() => SelectedAssignment = null;

    /// <summary>The plan and the activity trail start collapsed; the result is what matters first.</summary>
    [ObservableProperty] private bool _planExpanded;

    [ObservableProperty] private bool _activityExpanded;

    public string PlanSummary
    {
        get
        {
            var total = SelectedSubtasks.Count;
            if (total == 0)
                return "";
            var done = SelectedSubtasks.Count(s => s.Status == "done");
            return $"{total} step{(total == 1 ? "" : "s")} · {done} done";
        }
    }

    public string ActivitySummary
    {
        get
        {
            var tools = SelectedActivity.Count(a => a.IsTool);
            return tools == 0 ? $"{SelectedActivity.Count} update{(SelectedActivity.Count == 1 ? "" : "s")}"
                : $"{tools} tool call{(tools == 1 ? "" : "s")} · {SelectedActivity.Count} updates";
        }
    }

    public string DetailMeta => SelectedAssignment is null ? "" : SelectedAssignment.When;

    private async Task LoadDetailAsync(string id)
    {
        if (_client is null)
            return;
        var detailTask = _client.GetTaskAsync(id);
        var activityTask = _client.GetTaskActivityAsync(id);
        await Task.WhenAll(detailTask, activityTask);
        var detail = detailTask.Result;
        var agents = Staff.ToDictionary(e => e.Id);
        string Who(string? agentId) => agentId is not null && agents.TryGetValue(agentId, out var a) ? a.Name : "";

        var lines = activityTask.Result.Select(a => (a.TaskId, Line: ActivityLine.From(a, Who(a.EmployeeId)))).ToList();
        SelectedDetail = detail;
        Replace(SelectedActivity, lines.Select(l => l.Line));
        Replace(SelectedSubtasks, detail.Children
            .OrderBy(t => t.CreatedAt)
            .Select(t => new SubtaskViewModel(t, agents.GetValueOrDefault(t.Assignee),
                lines.Where(l => l.TaskId == t.Id).Select(l => l.Line).ToList(), _expandedSteps.Contains(t.Id),
                (stepId, open) => _ = open ? _expandedSteps.Add(stepId) : _expandedSteps.Remove(stepId))));
        OnPropertyChanged(nameof(PlanSummary));
        OnPropertyChanged(nameof(ActivitySummary));
        OnPropertyChanged(nameof(DetailMeta));
    }

    /// <summary>The line under the greeting: today's date and what the staff are up to.</summary>
    public string Summary
    {
        get
        {
            var parts = new List<string> { DateTime.Now.ToString("dddd, d MMMM") };
            if (ActiveCount > 0)
                parts.Add($"{ActiveCount} in progress");
            if (OpenQuestions > 0)
                parts.Add($"{OpenQuestions} need{(OpenQuestions == 1 ? "s" : "")} you");
            return string.Join(" · ", parts);
        }
    }

    // ---- profile and onboarding ----

    private OnboardingPacks? _packs;

    private async Task LoadProfileAsync(CancellationToken ct)
    {
        var packs = await _client!.GetOnboardingAsync(ct);
        ApplyPacks(packs);
        if (!packs.Profile.Onboarded && !ShowOnboarding)
        {
            Onboarding.Begin(packs, Ai.OwnerName);
            ShowOnboarding = true;
        }
    }

    private void ApplyPacks(OnboardingPacks packs)
    {
        _packs = packs;
        Profile = packs.Profile;
        Replace(StarterMissions, packs.Professions.FirstOrDefault(p => p.Id == packs.Profile.Profession)?.StarterMissions ?? []);
        OnPropertyChanged(nameof(MainAgentName));
    }

    private Task FinishOnboardingAsync(OwnerProfile? profile) => SafeAsync(async () =>
    {
        ShowOnboarding = false;
        if (profile is not null)
            await Ai.LoadAsync(); // the owner's name may have changed
        await LoadProfileAsync(_life.Token);
        await RefreshAllAsync();
        SelectedSection = TodaySection;
    });

    private Task RunMissionAsync(string mission) => SafeAsync(async () =>
    {
        var task = await _client!.CreateAssignmentAsync(mission, "");
        await RefreshAllAsync();
        SelectedAssignment = Assignments.FirstOrDefault(a => a.Task.Id == task.Id);
    });

    /// <summary>One-tap mission from the Today screen's empty state.</summary>
    [RelayCommand]
    private Task StartMission(string mission) => RunMissionAsync(mission);

    /// <summary>Runs first-run setup again, for example to pick another passion.</summary>
    [RelayCommand]
    private Task RunSetupAgain() => SafeAsync(async () =>
    {
        var packs = await _client!.GetOnboardingAsync();
        Onboarding.Begin(packs, Ai.OwnerName);
        Onboarding.AgentName = MainAgentName is "Your main agent" ? "" : MainAgentName;
        ShowOnboarding = true;
    });

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

    private Task SendAnswerAsync(InboxItemViewModel item, string? decision) => SafeAsync(async () =>
    {
        await _client!.AnswerAsync(item.Item.Id, item.AnswerText.Trim(), decision);
        Inbox.Remove(item);
        Chat.SetRequests(Inbox.ToList());
        OnPropertyChanged(nameof(OpenQuestions));
        OnPropertyChanged(nameof(Summary));
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

    // ---- inbox: the "Needs you" thread ----

    /// <summary>True while the Inbox shows the requests thread (questions and approvals) instead of a chat.</summary>
    [ObservableProperty] private bool _showRequests;

    [RelayCommand]
    private void OpenRequests()
    {
        Chat.Selected = null;
        ShowRequests = true;
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
