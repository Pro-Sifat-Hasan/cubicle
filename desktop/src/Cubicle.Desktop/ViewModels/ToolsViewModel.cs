using System.Collections.ObjectModel;
using System.IO;
using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// The Integrations page: apps agents can connect to (MCP servers, with guided setup), skills (reusable
/// instructions, installable from skills.sh) and plugins (installable bundles).
/// </summary>
public sealed partial class ToolsViewModel(Func<GatewayClient?> client, Action<string> showError, Action<string, string> notify)
    : ObservableObject
{
    /// <summary>Raised after a change, so the rest of the app refreshes.</summary>
    public event Action? Changed;

    public ObservableCollection<McpServerItemViewModel> Servers { get; } = [];
    public ObservableCollection<McpTemplate> ServerTemplates { get; } = [];
    public ObservableCollection<Skill> Skills { get; } = [];
    public ObservableCollection<SkillTemplate> SkillTemplates { get; } = [];

    /// <summary>The app catalog, grouped by kind.</summary>
    public ObservableCollection<AppGroupViewModel> AppGroups { get; } = [];

    /// <summary>Installed skills with who uses them.</summary>
    public ObservableCollection<SkillItemViewModel> SkillItems { get; } = [];

    /// <summary>Built-in skills not installed yet, for one-tap adding.</summary>
    public ObservableCollection<SkillTemplate> RecommendedSkills { get; } = [];

    /// <summary>Skills found by "Find skills" (skills.sh link, command or repository).</summary>
    public ObservableCollection<FoundSkillViewModel> FoundSkills { get; } = [];
    public ObservableCollection<PluginItemViewModel> Plugins { get; } = [];
    public ObservableCollection<PluginTemplateViewModel> PluginTemplates { get; } = [];
    public IReadOnlyList<string> Transports { get; } = ["stdio", "http"];

    /// <summary>"apps", "skills" or "plugins".</summary>
    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(ShowApps), nameof(ShowSkills), nameof(ShowPlugins))]
    private string _tab = "apps";

    public bool ShowApps => Tab == "apps";
    public bool ShowSkills => Tab == "skills";
    public bool ShowPlugins => Tab == "plugins";
    /// <summary>The panel for connecting (or managing) an app.</summary>
    [ObservableProperty] private ConnectViewModel? _connect;

    /// <summary>Whether the custom server editor (advanced) is open.</summary>
    [ObservableProperty] private bool _showAdvanced;

    [ObservableProperty] private McpEditorViewModel? _serverEditor;
    [ObservableProperty] private SkillEditorViewModel? _skillEditor;

    private IReadOnlyList<string> _secretNames = [];
    private IReadOnlyList<Agent> _agents = [];
    private IReadOnlyList<McpServer> _servers = [];

    public void Load(Catalog catalog, IReadOnlyList<McpServer> servers, IReadOnlyList<Skill> skills,
        IReadOnlyList<Plugin> plugins, IReadOnlyList<Agent> agents, IReadOnlyList<string> secretNames)
    {
        _secretNames = secretNames;
        _agents = agents;
        _servers = servers;
        var tests = Servers.ToDictionary(s => s.Server.Id, s => s.TestResult);
        Replace(Servers, servers.Select(s => new McpServerItemViewModel(s, UsedBy(agents, a => a.McpServers.Contains(s.Id)),
            MissingSecrets(s.SecretEnv.Concat(string.IsNullOrEmpty(s.AuthSecret) ? [] : [s.AuthSecret])), TestServerAsync, DeleteServerAsync,
            EditServer, ToggleServerAsync) { TestResult = tests.GetValueOrDefault(s.Id, "") }));
        Replace(Skills, skills);
        Replace(SkillItems, skills.Select(sk => new SkillItemViewModel(sk, UsedBy(agents, a => a.Skills.Contains(sk.Id)), EditSkillItem, DeleteSkillItemAsync)));
        Replace(Plugins, plugins.Select(p => new PluginItemViewModel(p, UninstallAsync)));
        if (ServerTemplates.Count == 0)
        {
            Replace(ServerTemplates, catalog.McpServers);
            Replace(SkillTemplates, catalog.Skills);
        }
        Replace(RecommendedSkills, SkillTemplates.Where(t => !skills.Any(sk => sk.TemplateId == t.Id)));
        BuildAppGroups(servers);
        Replace(PluginTemplates, catalog.Plugins.Select(p => new PluginTemplateViewModel(p,
            plugins.Any(i => i.CatalogId == p.Id), InstallAsync)));
        ServerEditor?.RefreshSecrets(secretNames);
    }

    private void BuildAppGroups(IReadOnlyList<McpServer> servers)
    {
        var cards = ServerTemplates.Select(t => new AppCardViewModel(t,
            servers.FirstOrDefault(s => s.TemplateId == t.Id && string.IsNullOrEmpty(s.PluginId)), OpenApp));
        var groups = cards.GroupBy(c => c.Template.Group)
            .OrderBy(g => GroupOrder(g.Key)).ThenBy(g => g.Key)
            .Select(g => new AppGroupViewModel(g.Key, g.OrderByDescending(c => c.IsConnected).ThenBy(c => c.Name).ToList()));
        Replace(AppGroups, groups);
    }

    private static int GroupOrder(string group) => group switch
    {
        "Code" => 0, "Productivity" => 1, "Data" => 2, "Web" => 3, "Utilities" => 4, _ => 5,
    };

    private static string UsedBy(IReadOnlyList<Agent> agents, Func<Agent, bool> uses)
    {
        var names = agents.Where(uses).Select(a => a.Name).ToList();
        return names.Count == 0 ? "Not used by any agent yet" : $"Used by {string.Join(", ", names)}";
    }

    private List<string> MissingSecrets(IEnumerable<string> names) => names.Where(n => !_secretNames.Contains(n)).ToList();

    // ---- apps ----

    private void OpenApp(AppCardViewModel card) =>
        Connect = new ConnectViewModel(card.Template, card.Server, _agents, _secretNames);

    [RelayCommand]
    private void CloseConnect() => Connect = null;

    [RelayCommand]
    private void OpenLink()
    {
        if (Connect is { HasLink: true } c)
            System.Diagnostics.Process.Start(new System.Diagnostics.ProcessStartInfo(c.Link) { UseShellExecute = true });
    }

    /// <summary>Connects the app in one step, then checks that it really works.</summary>
    [RelayCommand]
    private Task SaveConnectAsync() => Run(async c =>
    {
        if (Connect is not { IsBusy: false } panel)
            return;
        panel.IsBusy = true;
        panel.Status = "Connecting…";
        try
        {
            var result = await c.ConnectIntegrationAsync(panel.Template.Id, panel.Values(), panel.ChosenAgents());
            Changed?.Invoke();
            foreach (var f in panel.Fields.Where(f => f.IsSecret))
                f.Value = ""; // keys are never kept on screen
            panel.Status = "Saved. Checking the connection… (the first start can take a minute while packages download)";
            try
            {
                var test = await c.TestMcpServerAsync(result.Server.Id);
                panel.Status = string.IsNullOrEmpty(test.Error)
                    ? $"✓ Connected. {test.Tools.Count} tools are ready for {(result.Attached.Count > 0 ? "your chosen agents" : "agents you give it to")}."
                    : $"Saved, but the check failed: {test.Error}";
                panel.Succeeded = string.IsNullOrEmpty(test.Error);
            }
            catch (Exception e) when (e is GatewayException or HttpRequestException or OperationCanceledException)
            {
                panel.Status = $"Saved, but the check could not run: {e.Message}";
            }
        }
        catch
        {
            panel.Status = "";
            throw;
        }
        finally
        {
            panel.IsBusy = false;
        }
    });

    [RelayCommand]
    private Task DisconnectAsync(McpServer server) => Run(async c =>
    {
        await c.DeleteMcpServerAsync(server.Id);
        Connect = null;
        Changed?.Invoke();
    });

    // ---- skills from skills.sh ----

    public const string SkillsShUrl = "https://skills.sh";

    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(FindSkillsCommand))] private string _skillSource = "";
    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(FindSkillsCommand), nameof(InstallFoundCommand))] private bool _isFinding;
    [ObservableProperty] private string _findNote = "";
    private string _foundFrom = "";

    public bool HasFound => FoundSkills.Count > 0;
    public string FoundHeading => FoundSkills.Count == 0 ? "" : $"Found {FoundSkills.Count} skill{(FoundSkills.Count == 1 ? "" : "s")} in {_foundFrom}";

    [RelayCommand]
    private void BrowseSkillsSh() =>
        System.Diagnostics.Process.Start(new System.Diagnostics.ProcessStartInfo(SkillsShUrl) { UseShellExecute = true });

    [RelayCommand]
    private void PasteSkillSource()
    {
        try
        {
            if (System.Windows.Clipboard.ContainsText())
                SkillSource = System.Windows.Clipboard.GetText().Trim();
        }
        catch (System.Runtime.InteropServices.COMException)
        {
            showError("Windows would not let Cubicle read the clipboard. Paste with Ctrl+V instead.");
        }
    }

    private bool CanFind() => !IsFinding && !string.IsNullOrWhiteSpace(SkillSource);

    [RelayCommand(CanExecute = nameof(CanFind))]
    private Task FindSkillsAsync() => Run(async c =>
    {
        IsFinding = true;
        FindNote = "Looking…";
        FoundSkills.Clear();
        try
        {
            var found = await c.DiscoverSkillsAsync(SkillSource.Trim());
            _foundFrom = found.Source.ToString();
            Replace(FoundSkills, found.Skills.Select(f => new FoundSkillViewModel(f)));
            FindNote = found.Skills.All(f => f.Installed) ? "You already have all of these." : "";
        }
        catch
        {
            FindNote = "";
            throw;
        }
        finally
        {
            IsFinding = false;
            OnPropertyChanged(nameof(HasFound));
            OnPropertyChanged(nameof(FoundHeading));
        }
    });

    private bool CanInstallFound() => !IsFinding && FoundSkills.Any(f => f.IsChecked && f.CanPick);

    [RelayCommand]
    private Task InstallFoundAsync() => Run(async c =>
    {
        var picked = FoundSkills.Where(f => f.IsChecked && f.CanPick).Select(f => f.Skill.Path).ToList();
        if (picked.Count == 0)
        {
            FindNote = "Tick the skills you want first.";
            return;
        }
        IsFinding = true;
        try
        {
            var done = await c.InstallSkillsAsync(SkillSource.Trim(), picked);
            notify("Skills installed", done.Installed.Count == 0
                ? "Nothing new to install."
                : $"{done.Installed.Count} skill{(done.Installed.Count == 1 ? "" : "s")} added. Give them to agents on the Team page; review what a skill says first, because agents follow it.");
            FoundSkills.Clear();
            SkillSource = "";
            FindNote = "";
            Changed?.Invoke();
        }
        finally
        {
            IsFinding = false;
            OnPropertyChanged(nameof(HasFound));
            OnPropertyChanged(nameof(FoundHeading));
        }
    });

    private void EditSkillItem(SkillItemViewModel item) => SkillEditor = new SkillEditorViewModel(item.Skill);

    private Task DeleteSkillItemAsync(SkillItemViewModel item) => DeleteSkillAsync(item.Skill);

    // ---- MCP servers ----

    [RelayCommand]
    private Task AddServerTemplateAsync(McpTemplate template) => Run(async c =>
    {
        var server = await c.CreateMcpServerFromTemplateAsync(template.Id);
        notify("MCP server added", $"{server.Name} is ready. Turn it on for an agent on the Agents page.");
        Changed?.Invoke();
        // Presets that need a folder, a key or other setup open in the editor straight away.
        if (!string.IsNullOrEmpty(template.Setup))
            ServerEditor = new McpEditorViewModel(server, _secretNames);
    });

    [RelayCommand]
    private void NewServer() => ServerEditor = new McpEditorViewModel(null, _secretNames);

    private void EditServer(McpServerItemViewModel item) => ServerEditor = new McpEditorViewModel(item.Server, _secretNames);

    [RelayCommand]
    private void CloseServerEditor() => ServerEditor = null;

    [RelayCommand]
    private Task SaveServerAsync() => Run(async c =>
    {
        if (ServerEditor is not { } e)
            return;
        var saved = e.IsNew ? await c.CreateMcpServerAsync(e.ToDraft()) : await c.UpdateMcpServerAsync(e.Id!, e.ToDraft());
        notify("Saved", $"{saved.Name} is saved.");
        ServerEditor = null;
        Changed?.Invoke();
    });

    private Task TestServerAsync(McpServerItemViewModel item) => Run(async c =>
    {
        item.IsTesting = true;
        item.TestResult = "Connecting… (the first start can take a minute while packages download)";
        try
        {
            var result = await c.TestMcpServerAsync(item.Server.Id);
            item.TestResult = string.IsNullOrEmpty(result.Error)
                ? $"✓ Connected · {result.Tools.Count} tools: {string.Join(", ", result.Tools.Take(8).Select(t => t.Name))}{(result.Tools.Count > 8 ? ", …" : "")}"
                : $"✗ {result.Error}";
        }
        finally
        {
            item.IsTesting = false;
        }
    });

    private Task ToggleServerAsync(McpServerItemViewModel item, bool enabled) => Run(async c =>
    {
        var s = item.Server;
        await c.UpdateMcpServerAsync(s.Id, new McpServerDraft(s.Name, s.Description, s.Transport, s.Command, s.Args, s.Env,
            s.SecretEnv, s.Url, s.Headers, s.AuthSecret, enabled));
        Changed?.Invoke();
    });

    private Task DeleteServerAsync(McpServerItemViewModel item) => Run(async c =>
    {
        await c.DeleteMcpServerAsync(item.Server.Id);
        Servers.Remove(item);
        Changed?.Invoke();
    });

    /// <summary>Stores a secret an MCP server needs (called with the PasswordBox value).</summary>
    public Task SaveSecretAsync(string name, string value) => Run(async c =>
    {
        if (string.IsNullOrWhiteSpace(value))
            throw new FormatException("Paste the value first.");
        await c.SetSecretAsync(name, value.Trim());
        notify("Secret saved", $"{name} is stored encrypted. Your staff restart briefly to pick it up.");
        Changed?.Invoke();
    });

    // ---- skills ----

    [RelayCommand]
    private Task AddSkillTemplateAsync(SkillTemplate template) => Run(async c =>
    {
        var skill = await c.CreateSkillFromTemplateAsync(template.Id);
        notify("Skill added", $"'{skill.Name}' is ready. Give it to agents on the Agents page.");
        Changed?.Invoke();
    });

    [RelayCommand]
    private void NewSkill() => SkillEditor = new SkillEditorViewModel(null);

    [RelayCommand]
    private void EditSkill(Skill skill) => SkillEditor = new SkillEditorViewModel(skill);

    [RelayCommand]
    private void CloseSkillEditor() => SkillEditor = null;

    [RelayCommand]
    private Task SaveSkillAsync() => Run(async c =>
    {
        if (SkillEditor is not { } e)
            return;
        var draft = new SkillDraft(e.Name.Trim(), e.Description.Trim(), e.Content, e.Enabled);
        var saved = e.IsNew ? await c.CreateSkillAsync(draft) : await c.UpdateSkillAsync(e.Id!, draft);
        notify("Saved", $"Skill '{saved.Name}' is saved.");
        SkillEditor = null;
        Changed?.Invoke();
    });

    [RelayCommand]
    private Task DeleteSkillAsync(Skill skill) => Run(async c =>
    {
        await c.DeleteSkillAsync(skill.Id);
        Skills.Remove(skill);
        if (SkillEditor?.Id == skill.Id)
            SkillEditor = null;
        Changed?.Invoke();
    });

    /// <summary>Loads a SKILL.md file into the editor for review before saving.</summary>
    public void ImportSkillFile(string path)
    {
        try
        {
            var draft = PluginImport.ParseSkill(File.ReadAllText(path), Path.GetFileName(Path.GetDirectoryName(path)) ?? "skill");
            SkillEditor = new SkillEditorViewModel(null) { Name = draft.Name, Description = draft.Description, Content = draft.Content };
        }
        catch (Exception e) when (e is IOException or FormatException or UnauthorizedAccessException)
        {
            showError(e.Message);
        }
    }

    // ---- plugins ----

    private Task InstallAsync(PluginTemplateViewModel template) => Run(async c =>
    {
        var plugin = await c.InstallPluginAsync(template.Template.Id);
        notify("Plugin installed", Describe(plugin));
        Changed?.Invoke();
    });

    /// <summary>Installs a Claude Code plugin folder.</summary>
    public Task ImportPluginFolderAsync(string folder) => Run(async c =>
    {
        PluginBundle bundle;
        try
        {
            bundle = PluginImport.ReadPluginFolder(folder);
        }
        catch (Exception e) when (e is IOException or FormatException or UnauthorizedAccessException or System.Text.Json.JsonException)
        {
            throw new FormatException($"Could not read the plugin: {e.Message}");
        }
        var plugin = await c.InstallPluginBundleAsync(bundle);
        notify("Plugin installed", Describe(plugin));
        Changed?.Invoke();
    });

    private Task UninstallAsync(PluginItemViewModel item) => Run(async c =>
    {
        await c.UninstallPluginAsync(item.Plugin.Id);
        notify("Plugin removed", $"{item.Plugin.Name} and everything it added were removed.");
        Changed?.Invoke();
    });

    private static string Describe(Plugin p) =>
        $"{p.Name}: {PluginItemViewModel.Count(p.Agents.Count, "agent")}, {PluginItemViewModel.Count(p.McpServers.Count, "MCP server")}, {PluginItemViewModel.Count(p.Skills.Count, "skill")}.";

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

    private static void Replace<T>(ObservableCollection<T> target, IEnumerable<T> items)
    {
        target.Clear();
        foreach (var item in items)
            target.Add(item);
    }
}

public sealed partial class McpServerItemViewModel(
    McpServer server,
    string usedBy,
    IReadOnlyList<string> missingSecrets,
    Func<McpServerItemViewModel, Task> test,
    Func<McpServerItemViewModel, Task> delete,
    Action<McpServerItemViewModel> edit,
    Func<McpServerItemViewModel, bool, Task> toggle) : ObservableObject
{
    public McpServer Server { get; } = server;
    public string UsedBy { get; } = usedBy;
    public string Where => Server.Transport == "http" ? Server.Url ?? "" : $"{Server.Command} {string.Join(' ', Server.Args)}";
    public string MissingSecretsText => missingSecrets.Count == 0 ? "" : $"Needs secret: {string.Join(", ", missingSecrets)} (Edit to add it)";
    public string Origin => string.IsNullOrEmpty(Server.PluginId) ? "" : "From a plugin";

    [ObservableProperty]
    [NotifyCanExecuteChangedFor(nameof(TestCommand))]
    private bool _isTesting;

    [ObservableProperty] private string _testResult = "";

    public bool Enabled
    {
        get => Server.Enabled;
        set
        {
            if (value != Server.Enabled)
                _ = toggle(this, value);
        }
    }

    private bool CanTest() => !IsTesting;

    [RelayCommand(CanExecute = nameof(CanTest))]
    private Task TestAsync() => test(this);

    [RelayCommand]
    private Task DeleteAsync() => delete(this);

    [RelayCommand]
    private void Edit() => edit(this);
}

/// <summary>Edits an MCP server. Lists are edited as text: one argument per line, KEY=VALUE pairs.</summary>
public sealed partial class McpEditorViewModel : ObservableObject
{
    public McpEditorViewModel(McpServer? server, IReadOnlyList<string> secretNames)
    {
        Id = server?.Id;
        _name = server?.Name ?? "";
        _description = server?.Description ?? "";
        _transport = server?.Transport ?? "stdio";
        _command = server?.Command ?? "npx";
        _argsText = string.Join(Environment.NewLine, server?.Args ?? []);
        _envText = string.Join(Environment.NewLine, (server?.Env ?? new Dictionary<string, string>()).Select(kv => $"{kv.Key}={kv.Value}"));
        _secretEnvText = string.Join(", ", server?.SecretEnv ?? []);
        _url = server?.Url ?? "";
        _headersText = string.Join(Environment.NewLine, (server?.Headers ?? new Dictionary<string, string>()).Select(kv => $"{kv.Key}: {kv.Value}"));
        _authSecret = server?.AuthSecret ?? "";
        _enabled = server?.Enabled ?? true;
        RefreshSecrets(secretNames);
    }

    public string? Id { get; }
    public bool IsNew => Id is null;
    public string Heading => IsNew ? "New MCP server" : $"Edit {Name}";
    public bool IsStdio => Transport == "stdio";
    public bool IsHttp => Transport == "http";
    public ObservableCollection<SecretStatus> Secrets { get; } = [];

    [ObservableProperty] private string _name;
    [ObservableProperty] private string _description;

    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(IsStdio), nameof(IsHttp))]
    private string _transport;

    [ObservableProperty] private string _command;
    [ObservableProperty] private string _argsText;
    [ObservableProperty] private string _envText;
    [ObservableProperty] private string _secretEnvText;
    [ObservableProperty] private string _url;
    [ObservableProperty] private string _headersText;
    [ObservableProperty] private string _authSecret;
    [ObservableProperty] private bool _enabled;

    private IReadOnlyList<string> _secretNames = [];

    partial void OnSecretEnvTextChanged(string value) => RefreshSecrets(_secretNames);
    partial void OnAuthSecretChanged(string value) => RefreshSecrets(_secretNames);
    partial void OnTransportChanged(string value) => RefreshSecrets(_secretNames);

    public void RefreshSecrets(IReadOnlyList<string> secretNames)
    {
        _secretNames = secretNames;
        var needed = IsStdio ? SplitNames(SecretEnvText) : string.IsNullOrWhiteSpace(AuthSecret) ? [] : [AuthSecret.Trim()];
        Secrets.Clear();
        foreach (var n in needed)
            Secrets.Add(new SecretStatus(n, secretNames.Contains(n)));
    }

    private static List<string> SplitNames(string text) =>
        text.Split([',', ' ', '\n', '\r', ';'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries).Distinct().ToList();

    public McpServerDraft ToDraft()
    {
        if (string.IsNullOrWhiteSpace(Name))
            throw new FormatException("Give the server a name.");
        var args = ArgsText.Split('\n', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries).ToList();
        return new McpServerDraft(Name.Trim(), Description.Trim(), Transport, IsStdio ? Command.Trim() : null, IsStdio ? args : [],
            IsStdio ? Pairs(EnvText, '=') : new Dictionary<string, string>(), IsStdio ? SplitNames(SecretEnvText) : [],
            IsHttp ? Url.Trim() : null, IsHttp ? Pairs(HeadersText, ':') : new Dictionary<string, string>(),
            IsHttp && !string.IsNullOrWhiteSpace(AuthSecret) ? AuthSecret.Trim() : null, Enabled);
    }

    private static Dictionary<string, string> Pairs(string text, char separator)
    {
        var result = new Dictionary<string, string>();
        foreach (var line in text.Split('\n', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries))
        {
            var i = line.IndexOf(separator);
            if (i <= 0)
                throw new FormatException($"'{line}' should look like NAME{separator}value.");
            result[line[..i].Trim()] = line[(i + 1)..].Trim();
        }
        return result;
    }
}

public sealed record SecretStatus(string Name, bool Stored)
{
    public string StateText => Stored ? "✓ stored" : "not set";
}

public sealed partial class SkillEditorViewModel(Skill? skill) : ObservableObject
{
    public string? Id { get; } = skill?.Id;
    public bool IsNew => Id is null;
    public string Heading => IsNew ? "New skill" : $"Edit {Name}";

    [ObservableProperty] private string _name = skill?.Name ?? "";
    [ObservableProperty] private string _description = skill?.Description ?? "";
    [ObservableProperty] private string _content = skill?.Content ?? "";
    [ObservableProperty] private bool _enabled = skill?.Enabled ?? true;
}

public sealed partial class PluginItemViewModel(Plugin plugin, Func<PluginItemViewModel, Task> uninstall) : ObservableObject
{
    public Plugin Plugin { get; } = plugin;
    public string Contents => $"{Count(Plugin.Agents.Count, "agent")} · {Count(Plugin.McpServers.Count, "MCP server")} · {Count(Plugin.Skills.Count, "skill")}";
    public string Meta => string.Join(" · ", new[] { string.IsNullOrEmpty(Plugin.Version) ? "" : $"v{Plugin.Version}", Plugin.Source == "catalog" ? "Built-in pack" : "Imported" }.Where(s => s.Length > 0));

    public static string Count(int n, string noun) => $"{n} {noun}{(n == 1 ? "" : "s")}";

    [RelayCommand]
    private Task UninstallAsync() => uninstall(this);
}

public sealed partial class PluginTemplateViewModel(PluginTemplate template, bool installed, Func<PluginTemplateViewModel, Task> install)
    : ObservableObject
{
    public PluginTemplate Template { get; } = template;
    public bool Installed { get; } = installed;
    public string Contents => string.Join(" · ", new[]
    {
        Template.Agents is { Count: > 0 } a ? PluginItemViewModel.Count(a.Count, "agent") : "",
        Template.McpServers is { Count: > 0 } m ? PluginItemViewModel.Count(m.Count, "MCP server") : "",
        Template.Skills is { Count: > 0 } s ? PluginItemViewModel.Count(s.Count, "skill") : "",
    }.Where(x => x.Length > 0));

    private bool CanInstall() => !Installed;

    [RelayCommand(CanExecute = nameof(CanInstall))]
    private Task InstallAsync() => install(this);
}
