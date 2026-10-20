namespace Cubicle.Client.Tests;

public class SettingsApiTests
{
    private static readonly Uri Base = new("http://127.0.0.1:7878");
    private static readonly string Token = new('t', 48); // fake test credential

    [Fact]
    public async Task Saves_settings_as_snake_case_json()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            {"provider":"ollama","model":"llama3.3","effort":"low","base_url":"http://127.0.0.1:11434/v1",
             "owner_name":"Sam","web_access":false,"daily_token_budget":null}
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var saved = await client.SaveSettingsAsync(new OfficeSettings("ollama", "llama3.3", "low",
            "http://127.0.0.1:11434/v1", "Sam", WebAccess: false, DailyTokenBudget: null));

        var (req, body) = Assert.Single(handler.Requests);
        Assert.Equal(HttpMethod.Put, req.Method);
        Assert.Contains("\"base_url\":\"http://127.0.0.1:11434/v1\"", body);
        Assert.Contains("\"web_access\":false", body);
        Assert.Null(saved.DailyTokenBudget);
    }

    [Fact]
    public async Task Parses_providers_and_model_lists()
    {
        var handler = new FakeHandler(req => req.RequestUri!.AbsolutePath.EndsWith("/models")
            ? FakeHandler.Json("""{"models":["gpt-5","gpt-5-mini"],"live":true}""")
            : FakeHandler.Json("""
                [{"id":"openai","name":"OpenAI","kind":"openai_compatible","description":"d","secret_name":"OPENAI_API_KEY",
                  "key_required":true,"base_url":"https://api.openai.com/v1","base_url_editable":false,"default_model":"gpt-5",
                  "suggested_models":["gpt-5"],"web_search":false,"configured":true,"active":true}]
                """));
        using var client = new GatewayClient(Base, Token, handler);

        var provider = Assert.Single(await client.GetProvidersAsync());
        var models = await client.GetModelsAsync(provider.Id, "http://x y");

        Assert.True(provider.NeedsKey && provider.Active);
        Assert.True(models.Live);
        Assert.Equal(["gpt-5", "gpt-5-mini"], models.Models);
        Assert.Equal("?base_url=http%3A%2F%2Fx%20y", handler.Requests[1].Request.RequestUri!.Query);
    }
}
