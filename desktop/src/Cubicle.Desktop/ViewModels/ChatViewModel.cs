using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Net.Http;
using System.Text.Json;
using System.Windows.Threading;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// Chats in the Inbox: talk to any agent. Replies stream in live while the agent
/// works ("Searching the web…", then the answer as it is written).
/// </summary>
public sealed partial class ChatViewModel(Func<GatewayClient?> client, Action<string> showError) : ObservableObject
{
    private IReadOnlyDictionary<string, Agent> _agents = new Dictionary<string, Agent>();
    private bool _reloading; // re-selecting after a refresh must not reload the thread
    private readonly OpenSections _sections = new();
    private IReadOnlyList<InboxItemViewModel> _allRequests = [];
    private bool _hasLiveCards;
    private StepStore? _stepStore;
    private readonly TypeWriter _typer = new();
    private DispatcherTimer? _typeTimer;
    private string _activeTaskId = "";

    private StepStore _steps => _stepStore ??= new StepStore(_sections);
    private readonly HashSet<string> _seenMessages = [];
    private bool _threadLoaded; // the open chat has been shown once; messages that arrive after that are new

    public ObservableCollection<ConversationItemViewModel> Conversations { get; } = [];
    public ObservableCollection<ChatMessageViewModel> Messages { get; } = [];
    public ObservableCollection<AgentChoice> AgentChoices { get; } = [];

    /// <summary>Open questions and approvals from the agent of the open chat.</summary>
    public ObservableCollection<InboxItemViewModel> Requests { get; } = [];

    /// <summary>Assignments started from this chat whose reply is not shown (yet), so they are never hidden.</summary>
    public ObservableCollection<StartedCardViewModel> LooseStarted { get; } = [];

    [ObservableProperty] private ConversationItemViewModel? _selected;
    [ObservableProperty] private AgentChoice? _newChatAgent;

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(SendCommand))]
    private string _draft = "";

    /// <summary>What the agent is doing right now, e.g. "Searching the web…".</summary>
    [ObservableProperty] private string _liveStatus = "";

    /// <summary>The reply so far, while the agent is still writing it.</summary>
    [ObservableProperty] private string _liveText = "";

    /// <summary>The part of the reply typed out so far (it catches up with <see cref="LiveText"/> a few characters at a time).</summary>
    [ObservableProperty] private string _liveShown = "";

    /// <summary>What the agent is doing, step by step, for the reply being written now.</summary>
    [ObservableProperty] private ObservableCollection<StepViewModel> _liveSteps = [];

    public string LiveDisplay => LiveShown.Length == 0 ? "" : LiveShown + (IsReplying ? " ▍" : "");
    public bool HasLiveSteps => LiveSteps.Count > 0;
    public string LiveHeadline => LiveSteps.Count == 0 ? LiveStatus : $"{LiveStatus}  ·  {LiveSteps.Count} step{(LiveSteps.Count == 1 ? "" : "s")}";

    [ObservableProperty] private bool _isReplying;

    /// <summary>The agent paused its reply to ask you something.</summary>
    [ObservableProperty] private bool _isWaiting;

    partial void OnLiveTextChanged(string value)
    {
        _typer.SetTarget(value);
        if (value.Length == 0)
        {
            LiveShown = "";
            return;
        }
        _typeTimer ??= new DispatcherTimer(DispatcherPriority.Background) { Interval = TimeSpan.FromMilliseconds(30) };
        _typeTimer.Tick -= TypeTick;
        _typeTimer.Tick += TypeTick;
        _typeTimer.Start();
    }

    private void TypeTick(object? sender, EventArgs e)
    {
        if (_typer.Step())
        {
            LiveShown = _typer.Shown;
            ThreadUpdated?.Invoke();
        }
        else
        {
            _typeTimer?.Stop();
        }
    }

    partial void OnLiveShownChanged(string value) => OnPropertyChanged(nameof(LiveDisplay));

    partial void OnIsReplyingChanged(bool value) => OnPropertyChanged(nameof(LiveDisplay));

    partial void OnLiveStatusChanged(string value) => OnPropertyChanged(nameof(LiveHeadline));

    partial void OnLiveStepsChanged(ObservableCollection<StepViewModel>? oldValue, ObservableCollection<StepViewModel> newValue)
    {
        if (oldValue is not null)
            oldValue.CollectionChanged -= LiveStepsChanged;
        newValue.CollectionChanged += LiveStepsChanged;
        LiveStepsChanged(null, null!);
    }

    private void LiveStepsChanged(object? sender, System.Collections.Specialized.NotifyCollectionChangedEventArgs e)
    {
        OnPropertyChanged(nameof(HasLiveSteps));
        OnPropertyChanged(nameof(LiveHeadline));
    }

    private void ShowStepsOf(string taskId)
    {
        _activeTaskId = taskId;
        LiveSteps = taskId.Length == 0 ? [] : _steps.For(taskId);
    }

    public bool HasThread => Selected is not null;
    public string ThreadTitle => Selected?.Title ?? "";
    public string ThreadAgent => Selected is null ? "" : $"{Selected.Name} · {Selected.AgentTitle}";

    /// <summary>Raised when the thread changed, so the view can scroll to the newest message.</summary>
    public event Action? ThreadUpdated;

    /// <summary>Raised when the owner sends a message, so the view always jumps to it.</summary>
    public event Action? OwnerSent;

    public void Load(IReadOnlyList<Conversation> conversations, IReadOnlyList<Agent> agents)
    {
        _agents = agents.ToDictionary(a => a.Id);
        FilterRequests();
        var selectedId = Selected?.Conversation.Id;
        // Clearing the list makes the ListBox null out Selected, which would wipe the open thread.
        _reloading = true;
        Conversations.Clear();
        foreach (var c in conversations)
            Conversations.Add(new ConversationItemViewModel(c, _agents.GetValueOrDefault(c.AgentId)));

        var pick = NewChatAgent?.Id;
        AgentChoices.Clear();
        foreach (var a in agents.Where(a => a.Enabled))
            AgentChoices.Add(new AgentChoice(a.Id, $"{a.Name} · {(string.IsNullOrEmpty(a.Title) ? "Agent" : a.Title)}{(a.IsMain ? " (main)" : "")}"));
        NewChatAgent = AgentChoices.FirstOrDefault(a => a.Id == pick) ?? AgentChoices.FirstOrDefault();

        var current = Conversations.FirstOrDefault(c => c.Conversation.Id == selectedId);
        Selected = current;
        _reloading = false;
        if (current is null && selectedId is not null)
        {
            LiveStatus = LiveText = "";
            Messages.Clear();
        }
    }

    partial void OnSelectedChanged(ConversationItemViewModel? value)
    {
        OnPropertyChanged(nameof(HasThread));
        OnPropertyChanged(nameof(ThreadTitle));
        OnPropertyChanged(nameof(ThreadAgent));
        if (_reloading)
            return;
        LiveStatus = LiveText = "";
        Messages.Clear();
        LooseStarted.Clear();
        IsWaiting = false;
        _steps.Clear();
        _seenMessages.Clear();
        _threadLoaded = false;
        ShowStepsOf("");
        FilterRequests();
        if (value is not null)
            _ = Run(async c => await LoadThreadAsync(c, value.Conversation.Id));
    }

    /// <summary>Gives the chat the open requests of the whole office; it shows the ones that came from this conversation.</summary>
    public void SetRequests(IReadOnlyList<InboxItemViewModel> all)
    {
        _allRequests = all;
        FilterRequests();
    }

    private void FilterRequests()
    {
        var id = Selected?.Conversation.Id;
        Requests.Clear();
        if (id is null)
            return;
        foreach (var r in _allRequests.Where(r => r.Item.ConversationId == id))
            Requests.Add(r);
    }

    /// <summary>Reloads the open thread while something in it is still running (a reply, or an assignment it started).</summary>
    public void RefreshLive()
    {
        if (Selected is { } s && (_hasLiveCards || IsReplying))
            _ = Run(async c => await LoadThreadAsync(c, s.Conversation.Id));
    }

    private async Task LoadThreadAsync(GatewayClient c, string id)
    {
        var detailTask = c.GetConversationAsync(id);
        var activityTask = c.GetConversationActivityAsync(id);
        var stepsTask = c.GetConversationStepsAsync(id);
        try
        {
            await Task.WhenAll(detailTask, activityTask, stepsTask);
        }
        catch (Exception e) when (!detailTask.IsFaulted)
        {
            Debug.WriteLine($"chat activity or steps unavailable: {e.Message}"); // the thread itself still shows
        }
        var detail = await detailTask;
        if (Selected?.Conversation.Id != id)
            return;
        var trails = TrailBuilder.Lines(activityTask.IsCompletedSuccessfully ? activityTask.Result : [], _agents);
        var agent = _agents.GetValueOrDefault(detail.AgentId);
        if (stepsTask.IsCompletedSuccessfully)
            _steps.Load(stepsTask.Result, _agents, detail.AgentId);

        // Assignments the agent started: status, plan, how it was done and the result, each under its reply.
        var cards = new List<(string Source, StartedCardViewModel Card)>();
        foreach (var started in detail.Started ?? [])
        {
            try
            {
                var task = await c.GetTaskAsync(started.Assignment.Id);
                var lines = TrailBuilder.Lines(await c.GetTaskActivityAsync(started.Assignment.Id), _agents);
                var steps = await TryStepsAsync(c, started.Assignment.Id);
                cards.Add((started.SourceTaskId, new StartedCardViewModel(task, lines, _agents, _sections, steps)));
            }
            catch (Exception e) when (e is GatewayException or HttpRequestException)
            {
                Debug.WriteLine($"could not load assignment {started.Assignment.Id}: {e.Message}");
            }
        }
        if (Selected?.Conversation.Id != id)
            return;

        Messages.Clear();
        foreach (var m in detail.Messages)
        {
            var own = m.FromOwner || m.TaskId is not { Length: > 0 };
            var trail = own ? null : new ReplyTrailViewModel("reply:" + m.Id,
                trails.Where(l => l.TaskId == m.TaskId).Select(l => l.Line).ToList(), _sections,
                m.TaskId is { Length: > 0 } t ? _steps.For(t) : null);
            var item = new ChatMessageViewModel(m, m.FromOwner ? null : agent, trail,
                own ? [] : cards.Where(x => x.Source == m.TaskId).Select(x => x.Card).ToList());
            // A reply that arrives whole (nothing was streamed to the screen) is typed out, once.
            if (!own && _threadLoaded && !m.Failed && LiveShown.Length == 0 && _seenMessages.Add(m.Id))
            {
                item.Typed += () => ThreadUpdated?.Invoke();
                item.TypeIn();
            }
            else
                _seenMessages.Add(m.Id);
            Messages.Add(item);
        }
        _threadLoaded = true;
        var shown = detail.Messages.Select(m => m.TaskId).Where(t => !string.IsNullOrEmpty(t)).ToHashSet();
        LooseStarted.Clear();
        foreach (var (source, card) in cards.Where(x => !shown.Contains(x.Source)))
            LooseStarted.Add(card);

        _hasLiveCards = cards.Any(x => x.Card.IsRunning);
        IsReplying = detail.Busy;
        IsWaiting = detail.Waiting;
        if (detail.Busy || detail.Waiting)
            ShowStepsOf(detail.Messages.LastOrDefault(m => m.FromOwner)?.TaskId ?? _activeTaskId);
        else
            ShowStepsOf("");
        if (detail.Waiting)
            LiveStatus = "Waiting for your answer";
        else if (!detail.Busy)
            LiveStatus = LiveText = "";
        else if (LiveStatus.Length == 0 || LiveStatus == "Waiting for your answer")
            LiveStatus = "Thinking…";
        ThreadUpdated?.Invoke();
    }

    /// <summary>Opens the newest chat with an agent, or starts one.</summary>
    public Task OpenChatWithAsync(string agentId) => Run(async c =>
    {
        var existing = Conversations.FirstOrDefault(x => x.Conversation.AgentId == agentId);
        if (existing is not null)
        {
            Selected = existing;
            return;
        }
        await StartWith(c, agentId);
    });

    [RelayCommand]
    private Task NewChatAsync() => Run(async c =>
    {
        if (NewChatAgent is { } a)
            await StartWith(c, a.Id);
    });

    private async Task StartWith(GatewayClient c, string agentId)
    {
        var conv = await c.CreateConversationAsync(agentId);
        var item = new ConversationItemViewModel(conv, _agents.GetValueOrDefault(agentId));
        Conversations.Insert(0, item);
        Selected = item;
    }

    private bool CanSend() => !string.IsNullOrWhiteSpace(Draft) && Selected is not null;

    [RelayCommand(CanExecute = nameof(CanSend))]
    private Task SendAsync() => Run(async c =>
    {
        if (Selected is not { } conv)
            return;
        var text = Draft.Trim();
        Draft = "";
        var msg = await c.SendMessageAsync(conv.Conversation.Id, text);
        Messages.Add(new ChatMessageViewModel(msg, null, null, []));
        _seenMessages.Add(msg.Id);
        OwnerSent?.Invoke();
        ShowStepsOf(msg.TaskId ?? "");
        IsReplying = true;
        LiveStatus = "Thinking…";
        LiveText = "";
        ThreadUpdated?.Invoke();
    });

    [RelayCommand]
    private Task DeleteConversationAsync(ConversationItemViewModel item) => Run(async c =>
    {
        await c.DeleteConversationAsync(item.Conversation.Id);
        Conversations.Remove(item);
        if (Selected == item)
            Selected = null;
    });

    /// <summary>A task.progress event from the gateway.</summary>
    public void OnProgress(JsonElement data)
    {
        if (Selected is null || Str(data, "conversation_id") != Selected.Conversation.Id)
            return;
        IsReplying = true;
        if (Str(data, "task_id") is { Length: > 0 } taskId && taskId != _activeTaskId)
            ShowStepsOf(taskId);
        var status = Str(data, "status") ?? "";
        var text = Str(data, "text") ?? "";
        if (status.Length > 0)
            LiveStatus = status;
        if (text.Length > 0)
        {
            LiveText = text;
            LiveStatus = "Writing…";
        }
        else if (status.Length == 0)
        {
            LiveText = ""; // the worker cleared the draft: it turned out to be a remark before a tool call
        }
        ThreadUpdated?.Invoke();
    }

    /// <summary>A task.step event: a thought, a remark or a tool call (with its input or output) of the reply being written.</summary>
    public void OnStep(JsonElement data)
    {
        if (Selected is null || Str(data, "conversation_id") != Selected.Conversation.Id)
            return;
        AgentStep? step;
        try
        {
            step = data.Deserialize<AgentStep>(GatewayClient.Json);
        }
        catch (JsonException e)
        {
            Debug.WriteLine($"unreadable step: {e.Message}");
            return;
        }
        if (step is null)
            return;
        IsReplying = true;
        if (step.TaskId != _activeTaskId)
            ShowStepsOf(step.TaskId);
        _steps.Upsert(step, _agents, Selected.Conversation.AgentId);
        if (step.IsNote)
            LiveText = ""; // what was typed so far became a remark in the timeline
        if (step.IsRunning && step.IsTool)
            LiveStatus = $"{step.Title}…";
        OnPropertyChanged(nameof(LiveHeadline));
        ThreadUpdated?.Invoke();
    }

    private async Task<IReadOnlyList<AgentStep>> TryStepsAsync(GatewayClient c, string taskId)
    {
        try
        {
            return await c.GetTaskStepsAsync(taskId);
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException)
        {
            Debug.WriteLine($"steps unavailable for {taskId}: {e.Message}");
            return [];
        }
    }

    /// <summary>A conversation.updated event: reload the open thread when it is the one that changed.</summary>
    public void OnConversationUpdated(JsonElement data)
    {
        if (Selected is not null && Str(data, "id") == Selected.Conversation.Id)
            _ = Run(async c => await LoadThreadAsync(c, Selected.Conversation.Id));
    }

    private static string? Str(JsonElement e, string name) =>
        e.ValueKind == JsonValueKind.Object && e.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;

    private async Task Run(Func<GatewayClient, Task> action)
    {
        if (client() is not { } c)
        {
            showError("Not connected to your office yet.");
            return;
        }
        try
        {
            await action(c);
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException or OperationCanceledException)
        {
            showError(e.Message);
        }
    }
}

public sealed class ConversationItemViewModel(Conversation conversation, Agent? agent)
{
    public Conversation Conversation { get; } = conversation;

    // Id/Name feed the shared avatar template.
    public string Id => Conversation.AgentId;
    public string Name => agent?.Name ?? Conversation.AgentId;
    public string AgentTitle => string.IsNullOrEmpty(agent?.Title) ? "Agent" : agent.Title;
    public string Title => string.IsNullOrEmpty(Conversation.Title) ? "New chat" : Conversation.Title;
    public string Preview => string.IsNullOrEmpty(Conversation.LastMessage)
        ? "Say hello…"
        : string.Concat(Conversation.LastMessage.Where(ch => ch is not ('*' or '#' or '`' or '_'))).Trim(); // plain text, not Markdown
    public bool Busy => Conversation.Busy && !Conversation.Waiting;
    public bool Waiting => Conversation.Waiting;
    public string When => Conversation.UpdatedAt.ToLocalTime() is var t && t.Date == DateTime.Today ? t.ToString("HH:mm") : t.ToString("dd MMM");
}

public sealed class ChatMessageViewModel(ChatMessage message, Agent? agent, ReplyTrailViewModel? trail, IReadOnlyList<StartedCardViewModel> started)
    : ObservableObject
{
    private readonly TypeWriter _typer = new();
    private DispatcherTimer? _timer;
    private bool _typing;

    /// <summary>Types the reply out, as if the agent were writing it now.</summary>
    public void TypeIn()
    {
        if (Content.Length is 0 or > 8000)
            return;
        _typer.SetTarget(Content);
        _typing = true;
        OnPropertyChanged(nameof(Shown));
        _timer = new DispatcherTimer(DispatcherPriority.Background) { Interval = TimeSpan.FromMilliseconds(30) };
        _timer.Tick += (_, _) =>
        {
            if (_typer.Step())
            {
                OnPropertyChanged(nameof(Shown));
                Typed?.Invoke();
                return;
            }
            _typing = false;
            _timer.Stop();
            OnPropertyChanged(nameof(Shown));
        };
        _timer.Start();
    }

    /// <summary>Raised as new text is typed, so the view can keep the newest line in sight.</summary>
    public event Action? Typed;

    /// <summary>What the bubble shows: the reply, or the part typed so far.</summary>
    public string Shown => _typing ? _typer.Shown : Content;

    /// <summary>The tools the agent used for this reply, when it used any.</summary>
    public ReplyTrailViewModel? Trail { get; } = trail;
    public bool HasTrail => Trail is { HasLines: true };

    /// <summary>Assignments this reply started.</summary>
    public IReadOnlyList<StartedCardViewModel> Started { get; } = started;
    public bool HasStarted => Started.Count > 0;

    public ChatMessage Message { get; } = message;
    public bool FromOwner => Message.FromOwner;
    public bool FromAgent => !Message.FromOwner;
    public bool Failed => Message.Failed;
    public string Content => Message.Failed ? $"Something went wrong: {Message.Content}" : Message.Content;
    public string Id => agent?.Id ?? "";
    public string Name => agent?.Name ?? "Agent";
    public string Time => Message.CreatedAt.ToLocalTime().ToString("HH:mm");
}
