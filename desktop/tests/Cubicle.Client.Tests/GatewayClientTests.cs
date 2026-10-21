using System.Net;

namespace Cubicle.Client.Tests;

public class GatewayClientTests
{
    private static readonly string Token = new('t', 48); // fake test credential
    private static readonly Uri Base = new("http://127.0.0.1:7878");

    private const string AgentJson = """
        {"id":"manager","name":"Max","title":"Chief of Staff","avatar":"🧭","role":"manager",
         "description":"planning","instructions":"Be calm.","model":"","effort":"",
         "tools":["files","web"],"mcp_servers":["time"],"skills":[],"team":["writer"],"enabled":true,
         "status":"working","current_task_id":"tsk_1","created_at":"2026-10-20T10:00:00Z","updated_at":"2026-10-20T10:00:00Z"}
        """;

    [Fact]
    public async Task Sends_bearer_token_and_parses_snake_case()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json($"[{AgentJson}]"));
        using var client = new GatewayClient(Base, Token, handler);

        var agents = await client.GetAgentsAsync();

        var req = Assert.Single(handler.Requests).Request;
        Assert.Equal("Bearer", req.Headers.Authorization?.Scheme);
        Assert.Equal(Token, req.Headers.Authorization?.Parameter);
        var max = Assert.Single(agents);
        Assert.True(max.IsMain);
        Assert.Equal("tsk_1", max.CurrentTaskId);
        Assert.Equal(["time"], max.McpServers);
        Assert.Equal(["writer"], max.Team);
    }

    [Fact]
    public async Task Create_assignment_posts_json_body()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"id":"tsk_9","kind":"assignment","title":"Plan my week","description":"","assignee":"writer",
             "status":"queued","depends_on":[],"created_at":"2026-10-20T10:00:00Z","updated_at":"2026-10-20T10:00:00Z"}
            """, HttpStatusCode.Created));
        using var client = new GatewayClient(Base, Token, handler);

        var task = await client.CreateAssignmentAsync("Plan my week", "", "writer");

        var (req, body) = Assert.Single(handler.Requests);
        Assert.Equal(HttpMethod.Post, req.Method);
        Assert.Equal("/api/v1/assignments", req.RequestUri!.AbsolutePath);
        Assert.Contains("\"title\":\"Plan my week\"", body);
        Assert.Contains("\"agent_id\":\"writer\"", body);
        Assert.Equal("tsk_9", task.Id);
        Assert.True(task.IsAssignment);
    }

    [Fact]
    public async Task Agent_update_sends_every_editable_field()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json(AgentJson));
        using var client = new GatewayClient(Base, Token, handler);

        await client.UpdateAgentAsync("manager", new AgentDraft("Max", "Chief", "🧭", "d", "Be calm.", "", "high",
            ["files"], ["time"], ["email-writing"], ["writer"]));

        var (req, body) = Assert.Single(handler.Requests);
        Assert.Equal(HttpMethod.Put, req.Method);
        Assert.Equal("/api/v1/agents/manager", req.RequestUri!.AbsolutePath);
        foreach (var part in new[] { "\"effort\":\"high\"", "\"mcp_servers\":[\"time\"]", "\"team\":[\"writer\"]", "\"enabled\":true" })
            Assert.Contains(part, body);
    }

    [Fact]
    public async Task Template_agent_and_plugin_install_send_ids()
    {
        var handler = new FakeHandler(r => r.RequestUri!.AbsolutePath.EndsWith("/agents")
            ? FakeHandler.Json(AgentJson, HttpStatusCode.Created)
            : FakeHandler.Json("""{"id":"p","name":"P","description":"","version":"","source":"catalog","agents":[],"mcp_servers":[],"skills":[],"created_at":"2026-10-20T10:00:00Z"}""", HttpStatusCode.Created));
        using var client = new GatewayClient(Base, Token, handler);

        await client.CreateAgentFromTemplateAsync("travel-planner");
        await client.InstallPluginAsync("research-desk");

        Assert.Contains("\"template_id\":\"travel-planner\"", handler.Requests[0].Body);
        Assert.Contains("\"catalog_id\":\"research-desk\"", handler.Requests[1].Body);
    }

    [Fact]
    public async Task Chat_messages_and_conversation_detail()
    {
        var handler = new FakeHandler(r => r.Method == HttpMethod.Post
            ? FakeHandler.Json("""{"id":"msg_1","conversation_id":"cnv_1","role":"owner","content":"Hi","task_id":"tsk_1","status":"done","created_at":"2026-10-20T10:00:00Z"}""", HttpStatusCode.Created)
            : FakeHandler.Json("""
                {"id":"cnv_1","agent_id":"writer","title":"Hi","last_message":"Hello!","busy":false,
                 "created_at":"2026-10-20T10:00:00Z","updated_at":"2026-10-20T10:00:00Z",
                 "messages":[{"id":"m1","conversation_id":"cnv_1","role":"owner","content":"Hi","status":"done","created_at":"2026-10-20T10:00:00Z"},
                             {"id":"m2","conversation_id":"cnv_1","role":"agent","content":"boom","status":"failed","created_at":"2026-10-20T10:00:01Z"}]}
                """));
        using var client = new GatewayClient(Base, Token, handler);

        await client.SendMessageAsync("cnv_1", "Hi");
        var detail = await client.GetConversationAsync("cnv_1");

        Assert.Equal("/api/v1/conversations/cnv_1/messages", handler.Requests[0].Request.RequestUri!.AbsolutePath);
        Assert.True(detail.Messages[0].FromOwner);
        Assert.True(detail.Messages[1].Failed);
    }

    [Fact]
    public async Task Catalog_parses_templates()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"tools":[{"id":"files","name":"Files","description":"d"}],
             "agents":[{"id":"writer","category":"Writing","name":"Quill","title":"Writer","avatar":"✍️","description":"d","instructions":"i","tools":["files"],"skills":["email-writing"]}],
             "mcp_servers":[{"id":"github","name":"GitHub","description":"d","transport":"http","args":null,"env":null,"secret_env":null,"url":"https://x","auth_secret":"GITHUB_TOKEN"}],
             "skills":[{"id":"s","name":"s","description":"d","content":"c"}],
             "plugins":[{"id":"p","name":"P","description":"d","version":"1.0","agents":["writer"]}]}
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var catalog = await client.GetCatalogAsync();

        Assert.Equal(["GITHUB_TOKEN"], catalog.McpServers[0].RequiredSecrets);
        Assert.Equal("Quill", catalog.Agents[0].Name);
    }

    [Fact]
    public async Task Error_body_becomes_gateway_exception()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""{"error":"title is required"}""", HttpStatusCode.BadRequest));
        using var client = new GatewayClient(Base, Token, handler);

        var ex = await Assert.ThrowsAsync<GatewayException>(() => client.CreateAssignmentAsync("", ""));

        Assert.Equal(HttpStatusCode.BadRequest, ex.Status);
        Assert.Equal("title is required", ex.Message);
    }

    [Fact]
    public async Task Assignments_query_filters_top_level_tasks()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("[]"));
        using var client = new GatewayClient(Base, Token, handler);

        await client.GetAssignmentsAsync(limit: 20);

        Assert.Equal("?parent_id=&limit=20", handler.Requests[0].Request.RequestUri!.Query);
    }
}
