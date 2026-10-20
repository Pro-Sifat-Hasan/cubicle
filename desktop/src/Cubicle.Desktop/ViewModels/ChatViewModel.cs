using System.Collections.ObjectModel;
using System.Net.Http;
using System.Text.Json;
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

    public ObservableCollection<ConversationItemViewModel> Conversations { get; } = [];
    public ObservableCollection<ChatMessageViewModel> Messages { get; } = [];
    public ObservableCollection<AgentChoice> AgentChoices { get; } = [];

    [ObservableProperty] private ConversationItemViewModel? _selected;
    [ObservableProperty] private AgentChoice? _newChatAgent;

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(SendCommand))]
    private string _draft = "";

    /// <summary>What the agent is doing right now, e.g. "Searching the web…".</summary>
    [ObservableProperty] private string _liveStatus = "";

    /// <summary>The reply so far, while the agent is still writing it.</summary>
    [ObservableProperty] private string _liveText = "";

    [ObservableProperty] private bool _isReplying;

    public bool HasThread => Selected is not null;
    public string ThreadTitle => Selected?.Title ?? "";
    public string ThreadAgent => Selected is null ? "" : $"{Selected.Name} · {Selected.AgentTitle}";

    /// <summary>Raised when the thread changed, so the view can scroll to the newest message.</summary>
    public event Action? ThreadUpdated;

    public void Load(IReadOnlyList<Conversation> conversations, IReadOnlyList<Agent> agents)
    {
        _agents = agents.ToDictionary(a => a.Id);
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
        if (value is not null)
            _ = Run(async c => await LoadThreadAsync(c, value.Conversation.Id));
    }

    private async Task LoadThreadAsync(GatewayClient c, string id)
    {
        var detail = await c.GetConversationAsync(id);
        if (Selected?.Conversation.Id != id)
            return;
        var agent = _agents.GetValueOrDefault(detail.AgentId);
        Messages.Clear();
        foreach (var m in detail.Messages)
            Messages.Add(new ChatMessageViewModel(m, agent));
        IsReplying = detail.Busy;
        if (!detail.Busy)
            LiveStatus = LiveText = "";
        else if (LiveStatus.Length == 0)
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
        Messages.Add(new ChatMessageViewModel(msg, null));
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
        if (Str(data, "status") is { Length: > 0 } status)
            LiveStatus = status;
        if (Str(data, "text") is { Length: > 0 } text)
        {
            LiveText = text;
            LiveStatus = "Writing…";
        }
        ThreadUpdated?.Invoke();
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
    public bool Busy => Conversation.Busy;
    public string When => Conversation.UpdatedAt.ToLocalTime() is var t && t.Date == DateTime.Today ? t.ToString("HH:mm") : t.ToString("dd MMM");
}

public sealed class ChatMessageViewModel(ChatMessage message, Agent? agent)
{
    public ChatMessage Message { get; } = message;
    public bool FromOwner => Message.FromOwner;
    public bool FromAgent => !Message.FromOwner;
    public bool Failed => Message.Failed;
    public string Content => Message.Failed ? $"Something went wrong: {Message.Content}" : Message.Content;
    public string Id => agent?.Id ?? "";
    public string Name => agent?.Name ?? "Agent";
    public string Time => Message.CreatedAt.ToLocalTime().ToString("HH:mm");
}
