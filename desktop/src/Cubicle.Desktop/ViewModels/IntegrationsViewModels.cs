using System.Collections.ObjectModel;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>One app in the catalog of apps agents can connect to.</summary>
public sealed partial class AppCardViewModel(McpTemplate template, McpServer? connected, Action<AppCardViewModel> connect) : ObservableObject
{
    public McpTemplate Template { get; } = template;
    public McpServer? Server { get; } = connected;
    public string Name => Template.Name;
    public string Icon => string.IsNullOrEmpty(Template.Icon) ? "🔌" : Template.Icon;
    public string Description => Template.Description;
    public string Requires => string.IsNullOrEmpty(Template.Requires) ? "" : $"Needs {Template.Requires} on this PC";
    public bool IsConnected => Server is not null;
    public string ButtonText => IsConnected ? "Manage" : "Connect";

    [RelayCommand]
    private void Connect() => connect(this);
}

/// <summary>A category of apps ("Code", "Productivity"...).</summary>
public sealed record AppGroupViewModel(string Title, IReadOnlyList<AppCardViewModel> Apps);

/// <summary>One thing to fill in to connect an app. Secrets are bound to a password box and never shown again.</summary>
public sealed partial class FieldInputViewModel(IntegrationField field, bool alreadyStored) : ObservableObject
{
    public IntegrationField Field { get; } = field;
    public string Label => Field.Optional ? $"{Field.Label} (optional)" : Field.Label;
    public string Help => Field.Help ?? "";
    public string Placeholder => Field.Placeholder ?? "";
    public bool IsSecret => Field.IsSecret;
    public bool IsFolder => Field.IsFolder;
    public bool IsPlain => !Field.IsSecret;

    /// <summary>Shown under a key field when one is already saved: leaving it empty keeps it.</summary>
    public string StoredHint => alreadyStored && IsSecret ? "A key is already saved. Leave this empty to keep it, or paste a new one." : "";

    [ObservableProperty] private string _value = "";
}

public sealed partial class ConnectViewModel : ObservableObject
{
    public ConnectViewModel(McpTemplate template, McpServer? existing, IReadOnlyList<Agent> agents, IReadOnlyList<string> secretNames)
    {
        Template = template;
        Existing = existing;
        IsReconnect = existing is not null;
        foreach (var f in template.Fields ?? [])
            Fields.Add(new FieldInputViewModel(f, f.Secret is { } s && secretNames.Contains(s)));
        // Agents that benefit most are ticked; so are the ones that already have the app.
        foreach (var a in agents.Where(a => a.Enabled))
        {
            var recommended = (template.Agents ?? []).Any(id => id == a.Id || id == a.TemplateId);
            var has = existing is not null && a.McpServers.Contains(existing.Id);
            Agents.Add(new OptionItem(a.Id, a.Name, string.IsNullOrEmpty(a.Title) ? "Agent" : a.Title) { IsChecked = has || (!IsReconnect && recommended) });
        }
        if (existing is not null && !Fields.Any())
            Status = "Already connected. Choose who can use it and save.";
    }

    public McpTemplate Template { get; }

    /// <summary>The server already connected from this preset, when managing it.</summary>
    public McpServer? Existing { get; }
    public bool IsReconnect { get; }
    public string Icon => string.IsNullOrEmpty(Template.Icon) ? "🔌" : Template.Icon;
    public string Heading => IsReconnect ? $"Manage {Template.Name}" : $"Connect {Template.Name}";
    public string Link => Template.Link ?? "";
    public bool HasLink => !string.IsNullOrEmpty(Template.Link);
    public string Requires => string.IsNullOrEmpty(Template.Requires) ? "" : $"Needs {Template.Requires} installed on this PC.";
    public string SetupNote => Template.Setup ?? "";
    public bool HasFields => Fields.Count > 0;
    public string SaveText => IsReconnect ? "Save" : "Connect";
    public ObservableCollection<FieldInputViewModel> Fields { get; } = [];
    public ObservableCollection<OptionItem> Agents { get; } = [];

    [ObservableProperty, NotifyPropertyChangedFor(nameof(HasStatus))] private string _status = "";
    [ObservableProperty] private bool _isBusy;
    [ObservableProperty] private bool _succeeded;

    public bool HasStatus => Status.Length > 0;

    public Dictionary<string, string> Values() => Fields.ToDictionary(f => f.Field.Key, f => f.Value.Trim());
    public List<string> ChosenAgents() => OptionItem.Checked(Agents);
}

/// <summary>A skill found in a repository, with a tick box.</summary>
public sealed partial class FoundSkillViewModel(FoundSkill skill) : ObservableObject
{
    public FoundSkill Skill { get; } = skill;
    public string Name => Skill.Name;
    public string Description => Skill.Description;
    public bool Installed => Skill.Installed;
    public bool CanPick => !Skill.Installed;

    [ObservableProperty] private bool _isChecked = !skill.Installed;
}

/// <summary>An installed skill, with who uses it.</summary>
public sealed partial class SkillItemViewModel(Skill skill, string usedBy, Action<SkillItemViewModel> edit, Func<SkillItemViewModel, Task> delete)
    : ObservableObject
{
    public Skill Skill { get; } = skill;
    public string Name => Skill.Name;
    public string Description => Skill.Description;
    public string UsedBy { get; } = usedBy;
    public string Origin => !string.IsNullOrEmpty(Skill.PluginId) ? "From a plugin" : !string.IsNullOrEmpty(Skill.TemplateId) ? "Built in" : "Yours";

    [RelayCommand]
    private void Edit() => edit(this);

    [RelayCommand]
    private Task DeleteAsync() => delete(this);
}
