using System.Net;

namespace Cubicle.Client.Tests;

public class GatewayClientTests
{
    private static readonly string Token = new('t', 48); // fake test credential
    private static readonly Uri Base = new("http://127.0.0.1:7878");

    [Fact]
    public async Task Sends_bearer_token_and_parses_snake_case()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            [{"id":"manager","name":"Max","title":"Chief of Staff","role":"manager","avatar":"🧭",
              "model":"claude-opus-5-5","skills":"planning","status":"working",
              "current_task_id":"tsk_1","updated_at":"2026-10-20T10:00:00Z"}]
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var employees = await client.GetEmployeesAsync();

        var req = Assert.Single(handler.Requests).Request;
        Assert.Equal("Bearer", req.Headers.Authorization?.Scheme);
        Assert.Equal(Token, req.Headers.Authorization?.Parameter);
        var max = Assert.Single(employees);
        Assert.True(max.IsManager);
        Assert.Equal("tsk_1", max.CurrentTaskId);
    }

    [Fact]
    public async Task Create_assignment_posts_json_body()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"id":"tsk_9","kind":"assignment","title":"Plan my week","description":"","assignee":"manager",
             "status":"queued","depends_on":[],"created_at":"2026-10-20T10:00:00Z","updated_at":"2026-10-20T10:00:00Z"}
            """, HttpStatusCode.Created));
        using var client = new GatewayClient(Base, Token, handler);

        var task = await client.CreateAssignmentAsync("Plan my week", "");

        var (req, body) = Assert.Single(handler.Requests);
        Assert.Equal(HttpMethod.Post, req.Method);
        Assert.Equal("/api/v1/assignments", req.RequestUri!.AbsolutePath);
        Assert.Contains("\"title\":\"Plan my week\"", body);
        Assert.Equal("tsk_9", task.Id);
        Assert.True(task.IsAssignment);
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
