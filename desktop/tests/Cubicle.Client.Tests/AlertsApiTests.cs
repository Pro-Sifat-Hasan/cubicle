using System.Net;
using System.Text.Json;

namespace Cubicle.Client.Tests;

public class AlertsApiTests
{
    private static readonly Uri Base = new("http://127.0.0.1:7878");
    private static readonly string Token = new('t', 48); // fake test credential

    [Fact]
    public async Task Reads_the_messages_the_staff_looked_at()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json("""
            [{"key":"gmail:1","source":"gmail","summary":"Invoice question","status":"proposed",
              "created_at":"2026-10-21T08:00:00Z","updated_at":"2026-10-21T08:05:00Z"}]
            """));
        using var client = new GatewayClient(Base, Token, handler);

        var items = await client.GetTriageAsync(25);

        Assert.Equal("/api/v1/triage", handler.Requests[0].Request.RequestUri!.AbsolutePath);
        Assert.Equal("limit=25", handler.Requests[0].Request.RequestUri!.Query.TrimStart('?'));
        Assert.Equal(("gmail:1", "proposed"), (items[0].Key, items[0].Status));
    }

    private const string CurrentAlerts = """
        {"waiting":true,"done":false,"discord":true,"email":false,"email_to":"","smtp_host":"","smtp_port":587,
         "smtp_user":"","email_from":"","discord_webhook_set":true,"smtp_password_set":false}
        """;

    [Fact]
    public async Task Saves_alerts_with_the_write_only_secrets_and_never_reads_them_back()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json(CurrentAlerts));
        using var client = new GatewayClient(Base, Token, handler);

        var current = await client.GetAlertsAsync();
        Assert.True(current.DiscordWebhookSet);
        await client.SaveAlertsAsync(current, "https://discord.com/api/webhooks/1/x", null);

        var sent = JsonDocument.Parse(handler.Requests[1].Body!).RootElement;
        Assert.Equal(HttpMethod.Put, handler.Requests[1].Request.Method);
        Assert.Equal("https://discord.com/api/webhooks/1/x", sent.GetProperty("discord_webhook").GetString());
        Assert.Equal("", sent.GetProperty("smtp_password").GetString());
        Assert.Equal(587, sent.GetProperty("smtp_port").GetInt32());
        Assert.True(sent.GetProperty("waiting").GetBoolean());
    }

    [Fact]
    public async Task A_failed_test_message_carries_the_reason()
    {
        var handler = new FakeHandler(_ => FakeHandler.Json(
            "{\"error\":\"the mail server refused the user name or password\"}", HttpStatusCode.BadRequest));
        using var client = new GatewayClient(Base, Token, handler);

        var error = await Assert.ThrowsAsync<GatewayException>(() => client.TestAlertAsync("email"));

        Assert.Contains("refused the user name", error.Message);
        Assert.Equal("email", JsonDocument.Parse(handler.Requests[0].Body!).RootElement.GetProperty("channel").GetString());
    }
}
