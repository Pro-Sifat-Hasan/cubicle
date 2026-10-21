namespace Cubicle.Client.Tests;

public class IntegrationsApiTests
{
    private static readonly Uri Base = new("http://127.0.0.1:7878");
    private static readonly string Token = new('t', 48); // fake test credential

    private const string ServerJson = """
        {"id":"github","name":"GitHub","description":"d","transport":"http","args":[],"env":{},"secret_env":[],"url":"https://x",
         "headers":{},"auth_secret":"GITHUB_PERSONAL_ACCESS_TOKEN","enabled":true,"template_id":"github",
         "created_at":"2026-10-21T08:00:00Z","updated_at":"2026-10-21T08:00:00Z"}
        """;

    private const string DiscoverJson = """
        {"source":{"owner":"acme","repo":"skills"},"skills":[
          {"name":"debug","description":"Find bugs","path":"skills/debug/SKILL.md","installed":false}]}
        """;

    [Fact]
    public async Task Connects_an_app_with_values_and_agents()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json($$"""{"server":{{ServerJson}},"attached":["engineer"]}"""));
        using var client = new GatewayClient(Base, Token, handler);

        var result = await client.ConnectIntegrationAsync("github", new Dictionary<string, string> { ["token"] = "t0ken" }, ["engineer"]);

        var (req, body) = Assert.Single(handler.Requests);
        Assert.Equal("/api/v1/integrations/github/connect", req.RequestUri!.AbsolutePath);
        Assert.Contains("\"values\":{\"token\":\"t0ken\"}", body);
        Assert.Contains("\"agent_ids\":[\"engineer\"]", body);
        Assert.Equal("github", result.Server.Id);
        Assert.Equal(["engineer"], result.Attached);
    }

    [Fact]
    public async Task Reads_app_presets_with_fields_and_recommended_agents()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"tools":[],"agents":[],"skills":[],"plugins":[],"mcp_servers":[
              {"id":"slack","name":"Slack","category":"Productivity","icon":"💬","description":"d","transport":"stdio","command":"npx",
               "args":[],"env":{},"secret_env":["SLACK_BOT_TOKEN"],"fields":[
                 {"key":"token","label":"Bot token","kind":"secret","secret":"SLACK_BOT_TOKEN","placeholder":"xoxb-"},
                 {"key":"team","label":"Workspace ID","kind":"text"}],
               "agents":["manager"],"link":"https://api.slack.com/apps","requires":"Node.js"}]}
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var catalog = await client.GetCatalogAsync();

        var slack = Assert.Single(catalog.McpServers);
        Assert.Equal("Productivity", slack.Group);
        Assert.True(slack.Fields![0].IsSecret);
        Assert.False(slack.Fields[1].IsSecret);
        Assert.Equal(["manager"], slack.Agents);
    }

    [Fact]
    public async Task Discovers_and_installs_skills_from_a_source()
    {
        var handler = new FakeHandler(req => req.RequestUri!.AbsolutePath.EndsWith("/discover")
            ? FakeHandler.Json(DiscoverJson)
            : FakeHandler.Json("""{"installed":[],"skipped":["debug"]}"""));
        using var client = new GatewayClient(Base, Token, handler);

        var found = await client.DiscoverSkillsAsync("npx skills add acme/skills");
        var done = await client.InstallSkillsAsync("acme/skills", ["skills/debug/SKILL.md"]);

        Assert.Equal("acme/skills", found.Source.ToString());
        Assert.Equal("debug", Assert.Single(found.Skills).Name);
        Assert.Contains("\"source\":\"npx skills add acme/skills\"", handler.Requests[0].Body);
        Assert.Contains("\"paths\":[\"skills/debug/SKILL.md\"]", handler.Requests[1].Body);
        Assert.Equal(["debug"], done.Skipped);
    }
}
