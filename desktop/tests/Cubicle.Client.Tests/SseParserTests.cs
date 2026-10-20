using System.Text;

namespace Cubicle.Client.Tests;

public class SseParserTests
{
    [Fact]
    public async Task Parses_events_and_skips_heartbeats_and_garbage()
    {
        const string stream = """
            : connected

            data: {"type":"task.updated","data":{"id":"tsk_1"},"at":"2026-10-20T10:00:00Z"}

            : ping

            data: not-json

            data: {"type":"inbox.new","data":{"id":"ask_1"},"at":"2026-10-20T10:00:01Z"}


            """;
        await using var ms = new MemoryStream(Encoding.UTF8.GetBytes(stream.Replace("\r\n", "\n")));

        var events = new List<GatewayEvent>();
        await foreach (var e in SseParser.ParseAsync(ms))
            events.Add(e);

        Assert.Equal(["task.updated", "inbox.new"], events.Select(e => e.Type));
        Assert.Equal("tsk_1", events[0].Data.GetProperty("id").GetString());
    }
}
