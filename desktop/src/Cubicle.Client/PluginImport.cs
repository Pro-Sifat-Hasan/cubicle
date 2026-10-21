using System.Text.Json;
using System.Text.RegularExpressions;

namespace Cubicle.Client;

/// <summary>
/// Reads skills and plugins in the formats used by Claude Code, so existing ones
/// can be installed into Cubicle:
/// <list type="bullet">
/// <item>a skill: a <c>SKILL.md</c> file with <c>name</c> and <c>description</c> front matter;</item>
/// <item>a plugin folder: <c>.claude-plugin/plugin.json</c>, <c>skills/*/SKILL.md</c>,
/// <c>agents/*.md</c> and <c>.mcp.json</c>.</item>
/// </list>
/// Secrets are never read: <c>${NAME}</c> values become references to Cubicle secrets.
/// </summary>
public static partial class PluginImport
{
    [GeneratedRegex(@"^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$")]
    private static partial Regex WholeVariable();

    [GeneratedRegex(@"^Bearer\s+\$\{([A-Za-z_][A-Za-z0-9_]*)\}$", RegexOptions.IgnoreCase)]
    private static partial Regex BearerVariable();

    [GeneratedRegex(@"[^a-z0-9]+")]
    private static partial Regex NonSlug();

    /// <summary>Parses a SKILL.md file. The name falls back to <paramref name="fallbackName"/> (e.g. the folder name).</summary>
    public static SkillDraft ParseSkill(string text, string fallbackName)
    {
        var (meta, body) = SplitFrontMatter(text);
        var name = Slug(meta.GetValueOrDefault("name") ?? fallbackName);
        var description = meta.GetValueOrDefault("description") ?? "";
        if (string.IsNullOrWhiteSpace(description))
            throw new FormatException($"Skill '{name}' needs a description in its front matter.");
        if (string.IsNullOrWhiteSpace(body))
            throw new FormatException($"Skill '{name}' has no instructions.");
        return new SkillDraft(name, description, body.Trim());
    }

    /// <summary>Reads a Claude Code plugin folder into a bundle Cubicle can install.</summary>
    public static PluginBundle ReadPluginFolder(string folder)
    {
        if (!Directory.Exists(folder))
            throw new DirectoryNotFoundException(folder);
        var root = Path.GetFullPath(folder);
        var name = Path.GetFileName(root.TrimEnd(Path.DirectorySeparatorChar));
        string description = "", version = "";
        JsonElement? inlineMcp = null;

        var manifest = Path.Join(root, ".claude-plugin", "plugin.json");
        if (File.Exists(manifest))
        {
            using var doc = JsonDocument.Parse(File.ReadAllText(manifest));
            var m = doc.RootElement;
            name = Str(m, "name") ?? name;
            description = Str(m, "description") ?? "";
            version = Str(m, "version") ?? "";
            if (m.TryGetProperty("mcpServers", out var servers) && servers.ValueKind == JsonValueKind.Object)
                inlineMcp = servers.Clone();
        }

        var skills = new List<BundleSkill>();
        var skillsDir = Path.Join(root, "skills");
        if (Directory.Exists(skillsDir))
        {
            foreach (var file in Directory.EnumerateFiles(skillsDir, "SKILL.md", SearchOption.AllDirectories).Order())
            {
                var s = ParseSkill(File.ReadAllText(file), Path.GetFileName(Path.GetDirectoryName(file)!));
                skills.Add(new BundleSkill(s.Name, s.Name, s.Description, s.Content));
            }
        }

        var mcp = new List<BundleMcpServer>();
        var mcpFile = Path.Join(root, ".mcp.json");
        if (File.Exists(mcpFile))
        {
            using var doc = JsonDocument.Parse(File.ReadAllText(mcpFile));
            var servers = doc.RootElement.TryGetProperty("mcpServers", out var s) ? s : doc.RootElement;
            mcp.AddRange(ReadMcpServers(servers, root));
        }
        else if (inlineMcp is { } inline)
        {
            mcp.AddRange(ReadMcpServers(inline, root));
        }

        var agents = new List<BundleAgent>();
        var agentsDir = Path.Join(root, "agents");
        if (Directory.Exists(agentsDir))
        {
            foreach (var file in Directory.EnumerateFiles(agentsDir, "*.md").Order())
            {
                var (meta, body) = SplitFrontMatter(File.ReadAllText(file));
                var id = Slug(meta.GetValueOrDefault("name") ?? Path.GetFileNameWithoutExtension(file));
                agents.Add(new BundleAgent(
                    Id: id,
                    Name: Title(id),
                    Title: Title(id),
                    Description: Truncate(meta.GetValueOrDefault("description") ?? "", 1000),
                    Instructions: body.Trim(),
                    Model: "", // Claude Code model aliases don't apply to every provider
                    Skills: skills.Select(sk => sk.Id).ToList(),
                    McpServers: mcp.Select(m => m.Id).ToList()));
            }
        }

        if (agents.Count + skills.Count + mcp.Count == 0)
            throw new FormatException("This folder has no agents, skills or MCP servers (expected agents/, skills/ or .mcp.json).");
        return new PluginBundle(name, description, version, agents, mcp, skills);
    }

    private static IEnumerable<BundleMcpServer> ReadMcpServers(JsonElement servers, string root)
    {
        if (servers.ValueKind != JsonValueKind.Object)
            yield break;
        foreach (var p in servers.EnumerateObject())
        {
            var s = p.Value;
            var url = Str(s, "url");
            var type = Str(s, "type") ?? (url is null ? "stdio" : "http");
            var secretEnv = new List<string>();
            var env = new Dictionary<string, string>();
            if (s.TryGetProperty("env", out var e) && e.ValueKind == JsonValueKind.Object)
            {
                foreach (var kv in e.EnumerateObject())
                {
                    var value = kv.Value.GetString() ?? "";
                    // ${NAME} becomes a reference to the Cubicle secret NAME; plain values are kept.
                    if (WholeVariable().Match(value) is { Success: true } v)
                    {
                        if (v.Groups[1].Value != kv.Name)
                            env[kv.Name] = value; // renamed variables can't be mapped; keep the text visible
                        else
                            secretEnv.Add(kv.Name);
                    }
                    else
                    {
                        env[kv.Name] = value;
                    }
                }
            }
            var headers = new Dictionary<string, string>();
            string? auth = null;
            if (s.TryGetProperty("headers", out var h) && h.ValueKind == JsonValueKind.Object)
            {
                foreach (var kv in h.EnumerateObject())
                {
                    var value = kv.Value.GetString() ?? "";
                    if (kv.Name.Equals("Authorization", StringComparison.OrdinalIgnoreCase) && BearerVariable().Match(value) is { Success: true } b)
                        auth = b.Groups[1].Value;
                    else
                        headers[kv.Name] = value;
                }
            }
            var args = s.TryGetProperty("args", out var a) && a.ValueKind == JsonValueKind.Array
                ? a.EnumerateArray().Select(x => PluginRoot(x.GetString() ?? "", root)).ToList()
                : [];
            yield return new BundleMcpServer(
                Id: Slug(p.Name),
                Name: p.Name,
                Transport: type is "http" or "streamable-http" or "sse" ? "http" : "stdio",
                Command: Str(s, "command") is { } cmd ? PluginRoot(cmd, root) : null,
                Args: args,
                Env: env,
                SecretEnv: secretEnv,
                Url: url,
                Headers: headers,
                AuthSecret: auth);
        }
    }

    private static string PluginRoot(string value, string root) =>
        value.Replace("${CLAUDE_PLUGIN_ROOT}", root, StringComparison.Ordinal);

    /// <summary>Splits "---\nkey: value\n---\nbody" into front matter and body.</summary>
    internal static (Dictionary<string, string> Meta, string Body) SplitFrontMatter(string text)
    {
        var meta = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        var lines = text.Replace("\r\n", "\n").TrimStart('﻿').Split('\n');
        if (lines.Length == 0 || lines[0].Trim() != "---")
            return (meta, text);
        var end = Array.FindIndex(lines, 1, l => l.Trim() == "---");
        if (end < 0)
            return (meta, text);
        string? lastKey = null;
        for (var i = 1; i < end; i++)
        {
            var line = lines[i];
            var colon = line.IndexOf(':');
            if (colon > 0 && !char.IsWhiteSpace(line[0]))
            {
                lastKey = line[..colon].Trim();
                meta[lastKey] = Unquote(line[(colon + 1)..].Trim());
            }
            else if (lastKey is not null && line.Trim().Length > 0)
            {
                meta[lastKey] = (meta[lastKey] + " " + line.Trim()).Trim(); // folded continuation lines
            }
        }
        return (meta, string.Join('\n', lines[(end + 1)..]));
    }

    private static string Unquote(string v) =>
        v.Length >= 2 && (v[0] == '"' || v[0] == '\'') && v[^1] == v[0] ? v[1..^1] : v is ">" or "|" ? "" : v;

    private static string? Str(JsonElement e, string name) =>
        e.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;

    internal static string Slug(string name)
    {
        var s = NonSlug().Replace(name.Trim().ToLowerInvariant(), "-").Trim('-');
        return s.Length == 0 ? "item" : s.Length > 60 ? s[..60].Trim('-') : s;
    }

    private static string Title(string slug) =>
        string.Join(' ', slug.Split('-', StringSplitOptions.RemoveEmptyEntries).Select(w => char.ToUpperInvariant(w[0]) + w[1..]));

    private static string Truncate(string s, int n) => s.Length <= n ? s : s[..n];
}
