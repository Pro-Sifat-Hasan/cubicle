namespace Cubicle.Client.Tests;

public class PluginImportTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "cubicle-plugin-" + Guid.NewGuid().ToString("N")[..8]);

    public PluginImportTests() => Directory.CreateDirectory(_dir);

    public void Dispose() => Directory.Delete(_dir, recursive: true);

    private void Write(string relative, string content)
    {
        var path = Path.Combine(_dir, relative);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        File.WriteAllText(path, content);
    }

    [Fact]
    public void Parses_skill_front_matter()
    {
        var skill = PluginImport.ParseSkill("""
            ---
            name: Release Notes
            description: "Use when writing release notes."
            ---
            # Release notes
            Group changes by type.
            """, "fallback");

        Assert.Equal("release-notes", skill.Name);
        Assert.Equal("Use when writing release notes.", skill.Description);
        Assert.StartsWith("# Release notes", skill.Content);
    }

    [Fact]
    public void Skill_without_description_is_rejected() =>
        Assert.Throws<FormatException>(() => PluginImport.ParseSkill("---\nname: x\n---\nBody", "x"));

    [Fact]
    public void Reads_a_claude_code_plugin_folder()
    {
        Write(".claude-plugin/plugin.json", """{"name": "ship-it", "description": "Release tools", "version": "1.2.0"}""");
        Write("skills/changelog/SKILL.md", "---\ndescription: Use for changelogs.\n---\nKeep a changelog.");
        Write("agents/release-manager.md", "---\nname: release-manager\ndescription: Cuts releases\nmodel: sonnet\n---\nYou ship safely.");
        Write(".mcp.json", """
            {"mcpServers": {
              "github": {"type": "http", "url": "https://api.githubcopilot.com/mcp/",
                         "headers": {"Authorization": "Bearer ${GITHUB_TOKEN}", "X-Mode": "read"}},
              "local": {"command": "${CLAUDE_PLUGIN_ROOT}/bin/server", "args": ["--root", "${CLAUDE_PLUGIN_ROOT}"],
                        "env": {"API_KEY": "${API_KEY}", "LEVEL": "debug"}}
            }}
            """);

        var bundle = PluginImport.ReadPluginFolder(_dir);

        Assert.Equal(("ship-it", "Release tools", "1.2.0"), (bundle.Name, bundle.Description, bundle.Version));
        var skill = Assert.Single(bundle.Skills);
        Assert.Equal("changelog", skill.Name);
        var agent = Assert.Single(bundle.Agents);
        Assert.Equal(("release-manager", "Release Manager", "You ship safely."), (agent.Id, agent.Name, agent.Instructions));
        Assert.Equal("", agent.Model);
        Assert.Equal(["changelog"], agent.Skills);
        Assert.Equal(["github", "local"], agent.McpServers);

        var github = bundle.McpServers.Single(m => m.Id == "github");
        Assert.Equal(("http", "GITHUB_TOKEN"), (github.Transport, github.AuthSecret));
        Assert.Equal("read", github.Headers["X-Mode"]);
        var local = bundle.McpServers.Single(m => m.Id == "local");
        Assert.Equal("stdio", local.Transport);
        Assert.StartsWith(_dir, local.Command);
        Assert.Equal(["API_KEY"], local.SecretEnv);
        Assert.Equal("debug", local.Env["LEVEL"]);
        Assert.False(local.Env.ContainsKey("API_KEY")); // secrets are referenced, never copied
    }

    [Fact]
    public void Empty_folder_is_rejected() =>
        Assert.Throws<FormatException>(() => PluginImport.ReadPluginFolder(_dir));
}
