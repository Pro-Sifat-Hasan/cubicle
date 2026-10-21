namespace Cubicle.Client.Tests;

public class OnboardingApiTests
{
    private static readonly Uri Base = new("http://127.0.0.1:7878");
    private static readonly string Token = new('t', 48); // fake test credential

    private const string PersonaJson = """
        {"id":"gaming","title":"Gaming","emoji":"🎮","system_prompt_style":"Speak like a gamer.",
         "greeting_templates":["Hi {name}, {agent} here.","Welcome back, {name}."],"daily_feed_queries":["patch notes"],
         "accent_color":"#7C5CFF","avatar_set":["🎮","🕹️"]}
        """;

    [Fact]
    public async Task Reads_the_packs()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json($$"""
            {"profile":{"onboarded":false,"agent_name":"","profession":"","passion":null,"stack":null},
             "passions":[{{PersonaJson}}],
             "professions":[{"id":"software-engineer","title":"Software Engineer","emoji":"💻","description":"d",
               "team":[{"id":"pr-reviewer","name":"Reva","title":"PR Reviewer","avatar":"🔍","description":"x"}],
               "routines":[{"title":"Morning brief","schedule":"weekdays@08:30","agent":"pr-reviewer","enabled":true}],
               "starter_missions":["Review my latest PR"],"stack_suggestions":["Go"]}]}
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var packs = await client.GetOnboardingAsync();

        Assert.False(packs.Profile.Onboarded);
        Assert.Equal("Gaming", Assert.Single(packs.Passions).Title);
        var profession = Assert.Single(packs.Professions);
        Assert.Equal("PR Reviewer", Assert.Single(profession.Team).Title);
        Assert.True(Assert.Single(profession.Routines).Enabled);
        Assert.Equal(["Review my latest PR"], profession.StarterMissions);
    }

    [Fact]
    public async Task Completes_onboarding_with_snake_case_body()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"profile":{"onboarded":true,"agent_name":"Nova","profession":"founder","passion":null,"stack":["Go"]},
             "hired":[],"routines":[],"starter_missions":["Plan my week"]}
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var result = await client.CompleteOnboardingAsync(
            new OnboardingRequest("Nova", "Sam", "founder", "custom", "urban foxes", Stack: ["Go"]));

        var (req, body) = Assert.Single(handler.Requests);
        Assert.Equal(HttpMethod.Post, req.Method);
        Assert.Equal("/api/v1/onboarding", req.RequestUri!.AbsolutePath);
        Assert.Contains("\"agent_name\":\"Nova\"", body);
        Assert.Contains("\"custom_passion\":\"urban foxes\"", body);
        Assert.Contains("\"stack\":[\"Go\"]", body);
        Assert.True(result.Profile.Onboarded);
        Assert.Equal(["Plan my week"], result.StarterMissions);
    }

    [Fact]
    public async Task Polls_a_persona_until_it_is_done()
    {
        var handler = new FakeHandler(req => req.Method == HttpMethod.Post
            ? FakeHandler.Json("""{"task_id":"tsk_9"}""", System.Net.HttpStatusCode.Accepted)
            : FakeHandler.Json($$"""{"status":"done","persona":{{PersonaJson}}}"""));
        using var client = new GatewayClient(Base, Token, handler);

        var id = await client.StartPersonaAsync("urban foxes");
        var status = await client.GetPersonaAsync(id);

        Assert.Equal("tsk_9", id);
        Assert.Contains("\"passion\":\"urban foxes\"", handler.Requests[0].Body);
        Assert.Equal("/api/v1/persona/tsk_9", handler.Requests[1].Request.RequestUri!.AbsolutePath);
        Assert.True(status.IsDone);
        Assert.Equal("#7C5CFF", status.Persona!.AccentColor);
    }

    [Fact]
    public async Task Reports_a_failed_persona()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""{"status":"failed","error":"no provider"}"""));
        using var client = new GatewayClient(Base, Token, handler);

        var status = await client.GetPersonaAsync("tsk_1");

        Assert.True(status.IsFailed);
        Assert.False(status.IsDone);
        Assert.Equal("no provider", status.Error);
    }

    [Fact]
    public void Greets_with_the_owner_and_agent_names()
    {
        var persona = new Persona("p", "P", "", "", ["Hi {name}, {agent} here.", "Welcome back, {name}."], [], "#000000", []);

        Assert.Equal("Hi Sam, Nova here.", persona.Greet("Sam", "Nova", 0));
        Assert.Equal("Welcome back, Sam.", persona.Greet("Sam", "Nova", 3));
        Assert.Equal("Hello, Sam.", (persona with { GreetingTemplates = [] }).Greet("Sam", "Nova", 1));
    }
}
