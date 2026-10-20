using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// The Agents page: your staff, one-click role templates, and an editor for each
/// agent's instructions, model, tools, MCP servers, skills and team.
/// </summary>
public sealed partial class AgentsViewModel(Func<GatewayClient?> client, Action<string> showError, Action<string, string> notify)
    : ObservableObject
{
    private IReadOnlyList<Agent> _agents = [];
    private Catalog? _catalog;
    private IReadOnlyList<McpServer> _servers = [];
    private IReadOnlyList<Skill> _skills = [];
    private bool _reloading; // re-selecting after a refresh must not reset the editor

    /// <summary>Raised when the owner wants to chat with an agent (agent id).</summary>
    public event Action<string>? ChatRequested;

    /// <summary>Raised after a change, so the rest of the app refreshes.</summary>
    public event Action? Changed;

    public ObservableCollection<AgentItemViewModel> Agents { get; } = [];
    public ObservableCollection<TemplateGroup> TemplateGroups { get; } = [];
    public IReadOnlyList<string> Efforts { get; } = ["", "low", "medium", "high", "xhigh", "max"];

    [ObservableProperty] private AgentItemViewModel? _selected;
    [ObservableProperty] private AgentEditorViewModel? _editor;
    [ObservableProperty] private bool _showTemplates;

    public bool HasEditor => Editor is not null;
    public bool ShowEmpty => Editor is null && !ShowTemplates;

    partial void OnEditorChanged(AgentEditorViewModel? value)
    {
        OnPropertyChanged(nameof(HasEditor));
        OnPropertyChanged(nameof(ShowEmpty));
    }

    partial void OnShowTemplatesChanged(bool value) => OnPropertyChanged(nameof(ShowEmpty));

    partial void OnSelectedChanged(AgentItemViewModel? value)
    {
        if (value is null || _reloading)
            return;
        ShowTemplates = false;
        Editor = BuildEditor(value.Agent);
    }

    /// <summary>Called by the main view model with fresh data from the gateway.</summary>
    public void Load(IReadOnlyList<Agent> agents, Catalog catalog, IReadOnlyList<McpServer> servers, IReadOnlyList<Skill> skills)
    {
        _agents = agents;
        _catalog = catalog;
        _servers = servers;
        _skills = skills;
        var names = agents.ToDictionary(a => a.Id, a => a.Name);
        var selectedId = Selected?.Agent.Id;
        Agents.Clear();
        foreach (var a in agents)
            Agents.Add(new AgentItemViewModel(a, names));
        if (TemplateGroups.Count == 0)
        {
            foreach (var g in catalog.Agents.GroupBy(t => t.Category))
                TemplateGroups.Add(new TemplateGroup(g.Key, g.ToList()));
        }

        // Keep the owner's unsaved edits; otherwise show the latest version.
        var current = Agents.FirstOrDefault(a => a.Agent.Id == selectedId);
        _reloading = true;
        Selected = current;
        _reloading = false;
        if (Editor is { IsNew: false } editor)
        {
            if (current is null)
                Editor = null;
            else if (!editor.IsDirty)
                Editor = BuildEditor(current.Agent);
            else
                editor.UpdateLive(current.Agent);
        }
    }

    private AgentEditorViewModel BuildEditor(Agent? agent) =>
        new(agent, _catalog?.Tools ?? [], _servers, _skills, _agents);

    [RelayCommand]
    private void NewAgent()
    {
        Selected = null;
        ShowTemplates = false;
        Editor = BuildEditor(null);
    }

    [RelayCommand]
    private void BrowseTemplates()
    {
        Selected = null;
        Editor = null;
        ShowTemplates = true;
    }

    [RelayCommand]
    private Task AddTemplateAsync(AgentTemplate template) => Run(async c =>
    {
        var agent = await c.CreateAgentFromTemplateAsync(template.Id);
        notify("Agent added", $"{agent.Name} ({agent.Title}) joined your team.");
        await ReloadAndSelect(c, agent.Id);
    });

    [RelayCommand]
    private Task SaveAsync() => Run(async c =>
    {
        if (Editor is not { } e)
            return;
        var draft = e.ToDraft();
        var saved = e.IsNew ? await c.CreateAgentAsync(draft, e.JoinTeam) : await c.UpdateAgentAsync(e.Id!, draft);
        notify("Saved", $"{saved.Name} is updated. Changes apply from their next task.");
        Editor = null;
        await ReloadAndSelect(c, saved.Id);
    });

    [RelayCommand]
    private Task DeleteAsync() => Run(async c =>
    {
        if (Editor is not { IsNew: false, Id: { } id } e)
            return;
        await c.DeleteAgentAsync(id);
        notify("Agent removed", $"{e.Name} left your staff. Their queued work went to the main agent.");
        Editor = null;
        Selected = null;
        Changed?.Invoke();
    });

    [RelayCommand]
    private Task MakeMainAsync() => Run(async c =>
    {
        if (Editor is not { IsNew: false, Id: { } id })
            return;
        var main = await c.SetMainAgentAsync(id);
        notify("Main agent changed", $"{main.Name} now receives assignments and plans them with the team.");
        Editor = null;
        await ReloadAndSelect(c, id);
    });

    [RelayCommand]
    private void Chat()
    {
        if (Editor is { IsNew: false, Id: { } id })
            ChatRequested?.Invoke(id);
    }

    [RelayCommand]
    private void CancelEdit()
    {
        Editor = Selected is { } s ? BuildEditor(s.Agent) : null;
    }

    private async Task ReloadAndSelect(GatewayClient c, string id)
    {
        var agents = await c.GetAgentsAsync();
        if (_catalog is not null)
            Load(agents, _catalog, _servers, _skills);
        Selected = Agents.FirstOrDefault(a => a.Agent.Id == id);
        Changed?.Invoke();
    }

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
        catch (Exception e) when (e is GatewayException or HttpRequestException or OperationCanceledException or FormatException)
        {
            showError(e.Message);
        }
    }
}

public sealed record TemplateGroup(string Category, IReadOnlyList<AgentTemplate> Templates);

/// <summary>An agent in the roster list.</summary>
public sealed class AgentItemViewModel(Agent agent, IReadOnlyDictionary<string, string> names)
{
    public Agent Agent { get; } = agent;

    // Id/Name feed the shared avatar template.
    public string Id => Agent.Id;
    public string Name => Agent.Name;
    public string Title => string.IsNullOrEmpty(Agent.Title) ? "Agent" : Agent.Title;
    public string Avatar => Agent.Avatar;
    public bool IsMain => Agent.IsMain;
    public bool Enabled => Agent.Enabled;
    public string Status => Agent.Enabled ? Agent.Status : "offline";
    public string StatusLabel => Agent.Enabled ? StatusText.For(Agent.Status) : "Turned off";

    public string Summary
    {
        get
        {
            var parts = new List<string>();
            if (Agent.Team.Count > 0)
            {
                var team = Agent.Team.Select(id => names.GetValueOrDefault(id, id)).ToList();
                parts.Add(team.Count <= 3 ? $"Team: {string.Join(", ", team)}" : $"Team: {string.Join(", ", team.Take(3))} +{team.Count - 3}");
            }
            if (Agent.McpServers.Count > 0)
                parts.Add($"{Agent.McpServers.Count} MCP");
            if (Agent.Skills.Count > 0)
                parts.Add($"{Agent.Skills.Count} skill{(Agent.Skills.Count == 1 ? "" : "s")}");
            return string.Join(" · ", parts);
        }
    }
}

/// <summary>Edits one agent (or a new one).</summary>
public sealed partial class AgentEditorViewModel : ObservableObject
{
    public AgentEditorViewModel(Agent? agent, IReadOnlyList<ToolInfo> tools, IReadOnlyList<McpServer> servers,
        IReadOnlyList<Skill> skills, IReadOnlyList<Agent> agents)
    {
        Id = agent?.Id;
        IsMain = agent?.IsMain ?? false;
        _name = agent?.Name ?? "";
        _title = agent?.Title ?? "";
        _avatar = agent?.Avatar ?? "🤖";
        _description = agent?.Description ?? "";
        _instructions = agent?.Instructions ?? "";
        _model = agent?.Model ?? "";
        _effort = agent?.Effort ?? "";
        _enabled = agent?.Enabled ?? true;
        _status = agent is null ? "" : StatusText.For(agent.Status);

        var chosenTools = agent?.Tools ?? ["files", "web", "memory"];
        foreach (var t in tools)
            Tools.Add(new OptionItem(t.Id, t.Name, t.Description) { IsChecked = chosenTools.Contains(t.Id) });
        foreach (var s in servers)
            McpServers.Add(new OptionItem(s.Id, s.Name, s.Enabled ? s.Description : "Turned off in Tools")
            { IsChecked = agent?.McpServers.Contains(s.Id) ?? false });
        foreach (var s in skills)
            Skills.Add(new OptionItem(s.Id, s.Name, s.Description) { IsChecked = agent?.Skills.Contains(s.Id) ?? false });
        foreach (var a in agents.Where(a => a.Id != Id))
            Team.Add(new OptionItem(a.Id, a.Name, string.IsNullOrEmpty(a.Title) ? a.Description : a.Title)
            { IsChecked = agent?.Team.Contains(a.Id) ?? false });

        foreach (var option in Tools.Concat(McpServers).Concat(Skills).Concat(Team))
            option.PropertyChanged += (_, _) => IsDirty = true;
        PropertyChanged += MarkDirty;
    }

    private void MarkDirty(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName is not (nameof(IsDirty) or nameof(Status)))
            IsDirty = true;
    }

    public string? Id { get; }
    public bool IsNew => Id is null;
    public bool IsMain { get; }
    public bool CanMakeMain => !IsNew && !IsMain;
    public bool CanDelete => !IsNew && !IsMain;
    public string Heading => IsNew ? "New agent" : Name;

    public ObservableCollection<OptionItem> Tools { get; } = [];
    public ObservableCollection<OptionItem> McpServers { get; } = [];
    public ObservableCollection<OptionItem> Skills { get; } = [];
    public ObservableCollection<OptionItem> Team { get; } = [];

    [ObservableProperty] private string _name;
    [ObservableProperty] private string _title;
    [ObservableProperty] private string _avatar;
    [ObservableProperty] private string _description;
    [ObservableProperty] private string _instructions;
    [ObservableProperty] private string _model;
    [ObservableProperty] private string _effort;
    [ObservableProperty] private bool _enabled;
    [ObservableProperty] private bool _joinTeam = true;
    [ObservableProperty] private bool _isDirty;
    [ObservableProperty] private string _status;

    /// <summary>Live status changes while the owner is editing.</summary>
    public void UpdateLive(Agent agent) => Status = StatusText.For(agent.Status);

    public AgentDraft ToDraft()
    {
        if (string.IsNullOrWhiteSpace(Name))
            throw new FormatException("Give the agent a name.");
        return new AgentDraft(Name.Trim(), Title.Trim(), Avatar.Trim(), Description.Trim(), Instructions.Trim(),
            Model.Trim(), Effort, OptionItem.Checked(Tools), OptionItem.Checked(McpServers), OptionItem.Checked(Skills),
            OptionItem.Checked(Team), IsMain || Enabled);
    }
}
