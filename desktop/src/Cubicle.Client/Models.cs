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

/// <summary>A question one of your agents asked you while working.</summary>
public sealed record InboxItem(
    string Id,
    string TaskId,
    string EmployeeId,
    string Question,
    string? Answer,
    string Status,
    DateTimeOffset CreatedAt);

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

public sealed record OfficeStatus(
    string Version,
    IReadOnlyDictionary<string, int> Tasks,
    int OpenQuestions,
    bool BreakerOpen,
    long TokensToday,
    long DailyTokenBudget);

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
public sealed record Memory(string Id, string Content, string? EmployeeId, DateTimeOffset CreatedAt);

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
    string? Setup)
{
    /// <summary>Secrets the owner must store before the server works.</summary>
    public IEnumerable<string> RequiredSecrets =>
        (SecretEnv ?? []).Concat(string.IsNullOrEmpty(AuthSecret) ? [] : [AuthSecret]);
}

public sealed record SkillTemplate(string Id, string Name, string Description, string Content);

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
    DateTimeOffset UpdatedAt);

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

public sealed record ConversationDetail(
    string Id,
    string AgentId,
    string Title,
    bool Busy,
    IReadOnlyList<ChatMessage> Messages);

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
