using System.Runtime.CompilerServices;
using System.Text;
using System.Text.Json;

namespace Cubicle.Client;

/// <summary>Minimal Server-Sent Events parser for the gateway's <c>data: {json}</c> frames.</summary>
public static class SseParser
{
    public static async IAsyncEnumerable<GatewayEvent> ParseAsync(Stream stream, [EnumeratorCancellation] CancellationToken ct = default)
    {
        using var reader = new StreamReader(stream, Encoding.UTF8);
        var data = new StringBuilder();
        while (!ct.IsCancellationRequested)
        {
            var line = await reader.ReadLineAsync(ct).ConfigureAwait(false);
            if (line is null)
                yield break; // server closed the stream

            if (line.Length == 0)
            {
                if (data.Length > 0)
                {
                    var evt = TryParse(data.ToString());
                    data.Clear();
                    if (evt is not null)
                        yield return evt;
                }
                continue;
            }
            if (line.StartsWith(':'))
                continue; // comment / heartbeat
            if (line.StartsWith("data:", StringComparison.Ordinal))
            {
                if (data.Length > 0)
                    data.Append('\n');
                data.Append(line.AsSpan(5).TrimStart());
            }
        }
    }

    private static GatewayEvent? TryParse(string json)
    {
        try
        {
            return JsonSerializer.Deserialize<GatewayEvent>(json, GatewayClient.Json);
        }
        catch (JsonException)
        {
            return null;
        }
    }
}
