using System.Text.Json;

namespace Cubicle.Client;

/// <summary>An AI agent on your staff, as configured by you and stored by the gateway.</summary>
public sealed record Agent(
    string Id,
    string Name,
    string Title,
    string Avatar,
    string Role,
    string Description,
    string Instructions,
    string Model,
    string Effort,
    IReadOnlyList<string> Tools,
    IReadOnlyList<string> McpServers,
    IReadOnlyList<string> Skills,
    IReadOnlyList<string> Team,
    bool Enabled,
    string? TemplateId,
    string? PluginId,
    string Status,
    string? CurrentTaskId,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt)
{
    /// <summary>The main agent receives assignments and automations and plans them with its team.</summary>
    public bool IsMain => Role == "manager";
}

/// <summary>The editable fields of an agent (create and update).</summary>
public sealed record AgentDraft(
    string Name,
    string Title,
    string Avatar,
    string Description,
    string Instructions,
    string Model,
    string Effort,
    IReadOnlyList<string> Tools,
    IReadOnlyList<string> McpServers,
    IReadOnlyList<string> Skills,
    IReadOnlyList<string> Team,
    bool Enabled = true);

/// <summary>A unit of work: an assignment from you, a delegated subtask, the main agent's wrap-up, or a chat reply.</summary>
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
    DateTimeOffset UpdatedAt,
    string? RoutineId = null,
    string? ConversationId = null)
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

/// <summary>A question, or a request for approval, from one of your agents.</summary>
public sealed record InboxItem(
    string Id,
    string TaskId,
    string EmployeeId,
    string Question,
    string? Answer,
    string Status,
    DateTimeOffset CreatedAt,
    string? Kind = null,
    string? ConversationId = null,
    string? TaskTitle = null)
{
    public const string ApprovalKind = "approval";

    /// <summary>A yes/no request to do something with side effects, as opposed to a question.</summary>
    public bool IsApproval => Kind == ApprovalKind;

    /// <summary>The agent is waiting in a chat (as opposed to working on an assignment).</summary>
    public bool FromChat => !string.IsNullOrEmpty(ConversationId);
}

/// <summary>An automation: work an agent does on a schedule.</summary>
public sealed record Routine(
    string Id,
    string Title,
    string Description,
    string Schedule,
    bool Enabled,
    DateTimeOffset NextRunAt,
    DateTimeOffset? LastRunAt,
    string? LastTaskId,
    string AgentId = "",
    string? LastStatus = null);

public sealed record ActivityItem(long Id, string? EmployeeId, string? TaskId, string Message, DateTimeOffset CreatedAt);

/// <summary>One thing an agent did while working: thought, said something, or used a tool (with its input and output).</summary>
public sealed record AgentStep(
    string Id,
    string TaskId,
    string? ConversationId,
    string? AgentId,
    string Kind,
    string? Name,
    string? Input,
    string? Output,
    string Status,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt)
{
    public bool IsThinking => Kind == "thinking";
    public bool IsNote => Kind == "note";
    public bool IsTool => Kind == "tool";
    public bool IsRunning => Status == "running";
    public bool Failed => Status == "error";

    /// <summary>A friendly title for the step: "Write file", "Ask teammate", "Thinking".</summary>
    public string Title => IsThinking ? "Thinking" : IsNote ? "Note" : StepText.ToolTitle(Name);

    /// <summary>The most telling part of the input on one line: the file, the command, the search, the teammate.</summary>
    public string Summary => IsTool ? StepText.InputSummary(Name, Input) : "";
}

public static class StepText
{
    private static readonly Dictionary<string, string> Titles = new(StringComparer.OrdinalIgnoreCase)
    {
        ["Read"] = "Read file", ["read_file"] = "Read file", ["Write"] = "Write file", ["write_file"] = "Write file",
        ["Edit"] = "Edit file", ["edit_file"] = "Edit file", ["Glob"] = "Find files", ["find_files"] = "Find files",
        ["list_files"] = "List files", ["Grep"] = "Search files", ["grep_files"] = "Search files",
        ["WebSearch"] = "Search the web", ["web_search"] = "Search the web", ["WebFetch"] = "Read web page",
        ["web_fetch"] = "Read web page", ["run_command"] = "Run command", ["remember"] = "Remember",
        ["forget"] = "Forget", ["load_skill"] = "Load skill", ["ask_teammate"] = "Ask teammate",
        ["start_assignment"] = "Start assignment", ["ask_owner"] = "Ask you", ["request_approval"] = "Ask approval",
    };

    // Input fields that best describe a call, most telling first.
    private static readonly string[] SummaryKeys =
        ["file_path", "path", "command", "query", "url", "pattern", "teammate", "name", "title", "action", "question", "fact", "request"];

    public static string ToolTitle(string? name)
    {
        if (string.IsNullOrEmpty(name))
            return "Tool";
        if (Titles.TryGetValue(name, out var title))
            return title;
        var words = name.Replace('_', ' ').Trim();
        return char.ToUpperInvariant(words[0]) + words[1..];
    }

    public static string InputSummary(string? name, string? input)
    {
        if (string.IsNullOrWhiteSpace(input))
            return "";
        try
        {
            using var doc = System.Text.Json.JsonDocument.Parse(input);
            if (doc.RootElement.ValueKind == System.Text.Json.JsonValueKind.Object)
            {
                foreach (var key in SummaryKeys)
                    if (doc.RootElement.TryGetProperty(key, out var v) && v.ValueKind == System.Text.Json.JsonValueKind.String
                        && v.GetString() is { Length: > 0 } text)
                        return OneLine(key is "file_path" or "path" ? ShortPath(text) : text);
            }
        }
        catch (System.Text.Json.JsonException)
        {
            // still streaming in, or not JSON: fall through to the raw text
        }
        return OneLine(input);
    }

    /// <summary>
    /// A call's input for reading: one "name: value" per field, with long or multi-line values (a file's content,
    /// a command) on their own lines as real text. Input still streaming in (not valid JSON yet) is unescaped as far as it goes.
    /// </summary>
    public static string DisplayInput(string? input)
    {
        if (string.IsNullOrWhiteSpace(input))
            return "";
        try
        {
            using var doc = System.Text.Json.JsonDocument.Parse(input);
            if (doc.RootElement.ValueKind == System.Text.Json.JsonValueKind.Object)
            {
                var sb = new System.Text.StringBuilder();
                foreach (var property in doc.RootElement.EnumerateObject())
                {
                    var text = property.Value.ValueKind == System.Text.Json.JsonValueKind.String
                        ? property.Value.GetString() ?? "" : property.Value.GetRawText();
                    if (sb.Length > 0)
                        sb.Append('\n');
                    if (text.Contains('\n') || text.Length > 100)
                        sb.Append(property.Name).Append(":\n").Append(text).Append('\n');
                    else
                        sb.Append(property.Name).Append(": ").Append(text);
                }
                return sb.ToString().TrimEnd();
            }
        }
        catch (System.Text.Json.JsonException)
        {
            // still streaming in
        }
        return Unescape(input);
    }

    /// <summary>Shortens long tool output for display, saying how much was left out.</summary>
    public static string Clip(string? text, int max = 4000)
    {
        if (string.IsNullOrEmpty(text) || text.Length <= max)
            return text ?? "";
        return text[..max] + $"\n… {text.Length - max:N0} more characters";
    }

    private static string Unescape(string raw)
    {
        var sb = new System.Text.StringBuilder(raw.Length);
        for (var i = 0; i < raw.Length; i++)
        {
            if (raw[i] != '\\' || i + 1 >= raw.Length)
            {
                sb.Append(raw[i]);
                continue;
            }
            var next = raw[++i];
            sb.Append(next switch { 'n' => '\n', 't' => '\t', 'r' => '\r', _ => next });
        }
        return sb.ToString();
    }

    private static string ShortPath(string path)
    {
        var parts = path.Replace('\\', '/').Split('/', StringSplitOptions.RemoveEmptyEntries);
        return parts.Length <= 2 ? path : string.Join('/', parts[^2..]);
    }

    private static string OneLine(string text)
    {
        var line = string.Join(' ', text.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries)).Trim();
        return line.Length <= 140 ? line : line[..139] + "…";
    }
}

public sealed record OfficeStatus(
    string Version,
    IReadOnlyDictionary<string, int> Tasks,
    int OpenQuestions,
    bool BreakerOpen,
    long TokensToday,
    long DailyTokenBudget);

/// <summary>A message the staff looked at (email, Discord, Slack) and what became of it: proposed, approved, done or dismissed.</summary>
public sealed record TriageItem(string Key, string Source, string Summary, string Status, DateTimeOffset CreatedAt, DateTimeOffset UpdatedAt);

/// <summary>How the owner is reached outside the app. The webhook and mail password are write-only: only whether they are set is returned.</summary>
public sealed record AlertSettings(
    bool Waiting,
    bool Done,
    bool Discord,
    bool Email,
    string EmailTo,
    string SmtpHost,
    int SmtpPort,
    string SmtpUser,
    string EmailFrom,
    bool DiscordWebhookSet,
    bool SmtpPasswordSet);

/// <summary>How the office runs: which AI provider and model, effort, and limits.</summary>
public sealed record OfficeSettings(
    string Provider,
    string Model,
    string Effort,
    string BaseUrl,
    string OwnerName,
    bool WebAccess,
    long? DailyTokenBudget);

/// <summary>An AI provider the owner can choose (see gateway/internal/providers).</summary>
public sealed record ProviderInfo(
    string Id,
    string Name,
    string Kind,
    string Description,
    string? SecretName,
    bool KeyRequired,
    string? BaseUrl,
    bool BaseUrlEditable,
    string DefaultModel,
    IReadOnlyList<string> SuggestedModels,
    bool WebSearch,
    bool Configured,
    bool Active)
{
    public bool NeedsKey => !string.IsNullOrEmpty(SecretName);
}

/// <summary>Models offered by a provider. <see cref="Live"/> is false when only suggestions are available.</summary>
public sealed record ModelList(IReadOnlyList<string> Models, bool Live, string? Error);

/// <summary>A durable fact the staff keep in mind across tasks.</summary>
public sealed record Memory(string Id, string Content, string? EmployeeId, DateTimeOffset CreatedAt, string? Kind = null)
{
    /// <summary>The one record that describes the owner in full; everything else is a short fact.</summary>
    public bool IsProfile => Kind == "profile";
}

// ---- extensions ----

/// <summary>An MCP server: external tools (apps, files, services) your agents can use.</summary>
public sealed record McpServer(
    string Id,
    string Name,
    string Description,
    string Transport,
    string? Command,
    IReadOnlyList<string> Args,
    IReadOnlyDictionary<string, string> Env,
    IReadOnlyList<string> SecretEnv,
    string? Url,
    IReadOnlyDictionary<string, string> Headers,
    string? AuthSecret,
    bool Enabled,
    string? TemplateId,
    string? PluginId,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt);

public sealed record McpServerDraft(
    string Name,
    string Description,
    string Transport,
    string? Command,
    IReadOnlyList<string> Args,
    IReadOnlyDictionary<string, string> Env,
    IReadOnlyList<string> SecretEnv,
    string? Url,
    IReadOnlyDictionary<string, string> Headers,
    string? AuthSecret,
    bool Enabled = true);

public sealed record McpToolInfo(string Name, string Description);

/// <summary>Result of connecting to an MCP server: its tools, or why it failed.</summary>
public sealed record McpTestResult(IReadOnlyList<McpToolInfo> Tools, string? Error);

/// <summary>A skill: reusable instructions (SKILL.md) an agent loads when a task calls for it.</summary>
public sealed record Skill(
    string Id,
    string Name,
    string Description,
    string Content,
    bool Enabled,
    string? TemplateId,
    string? PluginId,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt);

public sealed record SkillDraft(string Name, string Description, string Content, bool Enabled = true);

/// <summary>An installed plugin: a bundle of agents, MCP servers and skills.</summary>
public sealed record Plugin(
    string Id,
    string Name,
    string Description,
    string Version,
    string Source,
    string? CatalogId,
    IReadOnlyList<string> Agents,
    IReadOnlyList<string> McpServers,
    IReadOnlyList<string> Skills,
    DateTimeOffset CreatedAt);

/// <summary>A plugin to install, e.g. read from a Claude Code plugin folder. Ids are local to the bundle.</summary>
public sealed record PluginBundle(
    string Name,
    string Description,
    string Version,
    IReadOnlyList<BundleAgent> Agents,
    IReadOnlyList<BundleMcpServer> McpServers,
    IReadOnlyList<BundleSkill> Skills);

public sealed record BundleAgent(
    string Id,
    string Name,
    string Title,
    string Description,
    string Instructions,
    string Model,
    IReadOnlyList<string> Skills,
    IReadOnlyList<string> McpServers,
    IReadOnlyList<string>? Tools = null);

public sealed record BundleMcpServer(
    string Id,
    string Name,
    string Transport,
    string? Command,
    IReadOnlyList<string> Args,
    IReadOnlyDictionary<string, string> Env,
    IReadOnlyList<string> SecretEnv,
    string? Url,
    IReadOnlyDictionary<string, string> Headers,
    string? AuthSecret);

public sealed record BundleSkill(string Id, string Name, string Description, string Content);

// ---- catalog (built-in templates) ----

public sealed record ToolInfo(string Id, string Name, string Description);

public sealed record AgentTemplate(
    string Id,
    string Category,
    string Name,
    string Title,
    string Avatar,
    string Description,
    string Instructions,
    IReadOnlyList<string> Tools,
    IReadOnlyList<string>? Skills);

public sealed record McpTemplate(
    string Id,
    string Name,
    string Description,
    string Transport,
    string? Command,
    IReadOnlyList<string>? Args,
    IReadOnlyDictionary<string, string>? Env,
    IReadOnlyList<string>? SecretEnv,
    string? Url,
    string? AuthSecret,
    string? Requires,
    string? Setup,
    string? Category = null,
    string? Icon = null,
    IReadOnlyList<IntegrationField>? Fields = null,
    IReadOnlyList<string>? Agents = null,
    string? Link = null)
{
    /// <summary>The kind of app, for grouping ("Code", "Productivity", "Web"...).</summary>
    public string Group => string.IsNullOrEmpty(Category) ? "Other" : Category;

    /// <summary>Secrets the owner must store before the server works.</summary>
    public IEnumerable<string> RequiredSecrets =>
        (SecretEnv ?? []).Concat(string.IsNullOrEmpty(AuthSecret) ? [] : [AuthSecret]);
}

/// <summary>One thing to fill in to connect an app: a key, a folder, a workspace id.</summary>
public sealed record IntegrationField(string Key, string Label, string Kind, string? Secret, string? Placeholder, string? Help, bool Optional = false)
{
    public bool IsSecret => Kind == "secret";
    public bool IsFolder => Kind == "folder";
}

public sealed record ConnectResult(McpServer Server, IReadOnlyList<string> Attached);

public sealed record SkillTemplate(string Id, string Name, string Description, string Content);

/// <summary>A skill found in a GitHub repository (the way skills.sh lists them).</summary>
public sealed record FoundSkill(string Name, string Description, string Path, bool Installed);

public sealed record SkillSourceInfo(string Owner, string Repo, string? Ref, string? Skill, string? Dir)
{
    public override string ToString() => $"{Owner}/{Repo}";
}

public sealed record FoundSkills(SkillSourceInfo Source, IReadOnlyList<FoundSkill> Skills);

public sealed record InstalledSkills(IReadOnlyList<Skill> Installed, IReadOnlyList<string> Skipped);

public sealed record PluginTemplate(
    string Id,
    string Name,
    string Description,
    string Version,
    IReadOnlyList<string>? Agents,
    IReadOnlyList<string>? McpServers,
    IReadOnlyList<string>? Skills,
    string? Requires);

public sealed record Catalog(
    IReadOnlyList<ToolInfo> Tools,
    IReadOnlyList<AgentTemplate> Agents,
    IReadOnlyList<McpTemplate> McpServers,
    IReadOnlyList<SkillTemplate> Skills,
    IReadOnlyList<PluginTemplate> Plugins);

// ---- chat ----

/// <summary>A chat with one agent.</summary>
public sealed record Conversation(
    string Id,
    string AgentId,
    string Title,
    string LastMessage,
    bool Busy,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt,
    bool Waiting = false);

public sealed record ChatMessage(
    string Id,
    string ConversationId,
    string Role,
    string Content,
    string? TaskId,
    string Status,
    DateTimeOffset CreatedAt)
{
    public bool FromOwner => Role == "owner";
    public bool Failed => Status == "failed";
}

/// <summary>An assignment an agent started during a chat reply (<see cref="SourceTaskId"/> is that reply's task).</summary>
public sealed record StartedAssignment(string SourceTaskId, TaskItem Assignment);

public sealed record ConversationDetail(
    string Id,
    string AgentId,
    string Title,
    bool Busy,
    IReadOnlyList<ChatMessage> Messages,
    IReadOnlyList<StartedAssignment>? Started = null,
    bool Waiting = false);

// ---- onboarding and profile ----

/// <summary>The voice and look a passion gives the main agent. Built-in passions are JSON packs in the gateway;
/// a custom passion's persona is written by the model.</summary>
public sealed record Persona(
    string Id,
    string Title,
    string Emoji,
    string SystemPromptStyle,
    IReadOnlyList<string> GreetingTemplates,
    IReadOnlyList<string> DailyFeedQueries,
    string AccentColor,
    IReadOnlyList<string> AvatarSet)
{
    public const string CustomId = "custom";

    /// <summary>A greeting with {name} (the owner) and {agent} filled in; <paramref name="seed"/> picks which one.</summary>
    public string Greet(string owner, string agent, int seed)
    {
        if (GreetingTemplates.Count == 0)
            return $"Hello, {owner}.";
        var template = GreetingTemplates[Math.Abs(seed) % GreetingTemplates.Count];
        return template.Replace("{name}", owner, StringComparison.Ordinal).Replace("{agent}", agent, StringComparison.Ordinal);
    }
}

public sealed record Teammate(string Id, string Name, string Title, string Avatar, string Description);

public sealed record PackRoutine(string Title, string Schedule, string Agent, bool Enabled);

/// <summary>A profession decides the team, routines and starter missions.</summary>
public sealed record Profession(
    string Id,
    string Title,
    string Emoji,
    string Description,
    IReadOnlyList<Teammate> Team,
    IReadOnlyList<PackRoutine> Routines,
    IReadOnlyList<string> StarterMissions,
    IReadOnlyList<string> StackSuggestions);

public sealed record OwnerProfile(
    bool Onboarded,
    string AgentName,
    string Profession,
    Persona? Passion,
    string? CustomPassion,
    IReadOnlyList<string>? Stack);

public sealed record OnboardingPacks(
    OwnerProfile Profile,
    IReadOnlyList<Persona> Passions,
    IReadOnlyList<Profession> Professions);

public sealed record ProfileInfo(OwnerProfile Profile, IReadOnlyList<string> StarterMissions);

/// <summary>What the owner chose in onboarding. For a custom passion, <see cref="Persona"/> is the generated one (optional).</summary>
public sealed record OnboardingRequest(
    string AgentName,
    string OwnerName,
    string Profession,
    string Passion,
    string? CustomPassion = null,
    Persona? Persona = null,
    IReadOnlyList<string>? Stack = null);

public sealed record OnboardingResult(
    OwnerProfile Profile,
    IReadOnlyList<Agent> Hired,
    IReadOnlyList<Routine> Routines,
    IReadOnlyList<string> StarterMissions);

/// <summary>Progress of a persona the main agent is writing for a custom passion.</summary>
public sealed record PersonaStatus(string Status, Persona? Persona, string? Error)
{
    public bool IsDone => Status == "done" && Persona is not null;
    public bool IsFailed => Status is "failed" or "cancelled";
}

/// <summary>A live event from the gateway's SSE stream.</summary>
public sealed record GatewayEvent(string Type, JsonElement Data, DateTimeOffset At);
