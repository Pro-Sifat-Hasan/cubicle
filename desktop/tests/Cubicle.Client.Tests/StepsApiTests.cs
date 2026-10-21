namespace Cubicle.Client.Tests;

public class StepsApiTests
{
    private static readonly Uri Base = new("http://127.0.0.1:7878");
    private static readonly string Token = new('t', 48); // fake test credential

    [Fact]
    public async Task Reads_the_steps_of_a_chat()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            [{"id":"s1","task_id":"tsk_1","conversation_id":"cnv_1","agent_id":"manager","kind":"thinking","name":"~232 tokens",
              "status":"done","created_at":"2026-10-21T08:00:00Z","updated_at":"2026-10-21T08:00:01Z"},
             {"id":"s2","task_id":"tsk_1","conversation_id":"cnv_1","agent_id":"engineer","kind":"tool","name":"Write",
              "input":"{\"file_path\":\"C:\\\\work\\\\site\\\\index.html\"}","output":"File created","status":"running",
              "created_at":"2026-10-21T08:00:02Z","updated_at":"2026-10-21T08:00:03Z"}]
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var steps = await client.GetConversationStepsAsync("cnv_1");

        Assert.Equal("conversation_id=cnv_1", handler.Requests[0].Request.RequestUri!.Query.TrimStart('?'));
        Assert.True(steps[0].IsThinking);
        Assert.Equal("Thinking", steps[0].Title);
        Assert.True(steps[1].IsTool && steps[1].IsRunning);
        Assert.Equal("Write file", steps[1].Title);
        Assert.Equal("site/index.html", steps[1].Summary);
        Assert.Equal("engineer", steps[1].AgentId);
    }

    [Theory]
    [InlineData("run_command", "{\"command\":\"pytest -q\"}", "pytest -q")]
    [InlineData("ask_teammate", "{\"teammate\":\"engineer\",\"request\":\"Build it\"}", "engineer")]
    [InlineData("WebSearch", "{\"query\":\"best portfolio sites\"}", "best portfolio sites")]
    [InlineData("custom_tool", "{\"unknown\":1}", "{\"unknown\":1}")]
    [InlineData("Write", "{\"file_path\":\"C:\\\\a\\\\b", "{\"file_path\":\"C:\\\\a\\\\b")] // still streaming in
    [InlineData("Write", "", "")]
    public void Summarises_the_input_of_a_call(string name, string input, string expected) =>
        Assert.Equal(expected, StepText.InputSummary(name, input));

    [Theory]
    [InlineData("ask_teammate", "Ask teammate")]
    [InlineData("github_create_issue", "Github create issue")]
    [InlineData(null, "Tool")]
    public void Titles_tools_in_plain_words(string? name, string expected) => Assert.Equal(expected, StepText.ToolTitle(name));

    [Fact]
    public async Task Reads_the_steps_of_a_task()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("[]"));
        using var client = new GatewayClient(Base, Token, handler);
        Assert.Empty(await client.GetTaskStepsAsync("tsk 1"));
        Assert.Contains("task_id=tsk%201", handler.Requests[0].Request.RequestUri!.Query);
    }
}

public class StepTextTests
{
    [Fact]
    public void Shows_fields_with_long_values_on_their_own_lines()
    {
        var text = StepText.DisplayInput("{\"file_path\":\"index.html\",\"content\":\"<h1>Hi</h1>\\n<p>There</p>\"}");
        Assert.Equal("file_path: index.html\ncontent:\n<h1>Hi</h1>\n<p>There</p>", text);
    }

    [Fact]
    public void Unescapes_input_that_is_still_streaming_in()
    {
        // A write's content arriving piece by piece: not valid JSON yet, but readable as it comes.
        Assert.Equal("{\"content\": \"line one\nline \"two", StepText.DisplayInput("{\"content\": \\\"line one\\nline \\\"two"));
        Assert.Equal("", StepText.DisplayInput(null));
    }

    [Fact]
    public void Clips_long_output_and_says_how_much_was_left_out()
    {
        Assert.Equal("short", StepText.Clip("short"));
        var clipped = StepText.Clip(new string('x', 4100));
        Assert.EndsWith("… 100 more characters", clipped);
    }
}
