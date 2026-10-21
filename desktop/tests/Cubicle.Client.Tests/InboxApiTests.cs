namespace Cubicle.Client.Tests;

public class InboxApiTests
{
    private static readonly Uri Base = new("http://127.0.0.1:7878");
    private static readonly string Token = new('t', 48); // fake test credential

    [Fact]
    public async Task Reads_questions_and_approvals_with_their_origin()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            [{"id":"ask_1","task_id":"tsk_1","employee_id":"manager","kind":"question","question":"Which date?","status":"open",
              "created_at":"2026-10-21T08:00:00Z","task_title":"Plan the launch"},
             {"id":"ask_2","task_id":"tsk_2","employee_id":"writer","kind":"approval","question":"**Email Bob**","status":"open",
              "created_at":"2026-10-21T08:05:00Z","conversation_id":"cnv_1"}]
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var items = await client.GetInboxAsync(null);

        Assert.Equal("/api/v1/inbox", handler.Requests[0].Request.RequestUri!.AbsolutePath);
        Assert.False(items[0].IsApproval);
        Assert.False(items[0].FromChat);
        Assert.Equal("Plan the launch", items[0].TaskTitle);
        Assert.True(items[1].IsApproval);
        Assert.True(items[1].FromChat);
    }

    [Fact]
    public async Task Sends_an_approval_decision_with_an_optional_note()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("{}"));
        using var client = new GatewayClient(Base, Token, handler);

        await client.AnswerAsync("ask_2", "Use the short version", "approve");
        await client.AnswerAsync("ask_1", "1 March");

        Assert.Contains("\"decision\":\"approve\"", handler.Requests[0].Body);
        Assert.Contains("\"answer\":\"Use the short version\"", handler.Requests[0].Body);
        Assert.Contains("\"decision\":null", handler.Requests[1].Body);
    }

    [Fact]
    public async Task Reads_a_chat_with_its_started_assignments_and_waiting_state()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"id":"cnv_1","agent_id":"manager","title":"","last_message":"","busy":false,"waiting":true,
             "created_at":"2026-10-21T08:00:00Z","updated_at":"2026-10-21T08:00:00Z",
             "messages":[{"id":"m1","conversation_id":"cnv_1","role":"agent","content":"Started it","task_id":"tsk_chat",
                          "status":"done","created_at":"2026-10-21T08:00:00Z"}],
             "started":[{"source_task_id":"tsk_chat","assignment":{"id":"tsk_asg","kind":"assignment","title":"Research","description":"",
                         "assignee":"manager","status":"in_progress","depends_on":[],"created_at":"2026-10-21T08:00:00Z",
                         "updated_at":"2026-10-21T08:00:00Z"}}]}
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var detail = await client.GetConversationAsync("cnv_1");

        Assert.True(detail.Waiting);
        var started = Assert.Single(detail.Started!);
        Assert.Equal("tsk_chat", started.SourceTaskId);
        Assert.Equal("Research", started.Assignment.Title);
    }

    [Fact]
    public async Task Asks_for_the_trail_of_a_whole_chat()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            [{"id":1,"employee_id":"manager","task_id":"tsk_chat","message":"Tool: Searching the web…","created_at":"2026-10-21T08:00:00Z"}]
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var trail = await client.GetConversationActivityAsync("cnv 1");

        Assert.Equal("?conversation_id=cnv%201&limit=1000", handler.Requests[0].Request.RequestUri!.Query);
        Assert.Equal("tsk_chat", Assert.Single(trail).TaskId);
    }
}
