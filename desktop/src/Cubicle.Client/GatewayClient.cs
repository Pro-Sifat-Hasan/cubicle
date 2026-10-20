using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Runtime.CompilerServices;
using System.Text.Json;

namespace Cubicle.Client;

public sealed class GatewayException(HttpStatusCode status, string message) : Exception(message)
{
    public HttpStatusCode Status { get; } = status;
}

/// <summary>Typed client for the gateway's owner API (<c>/api/v1</c>).</summary>
public sealed class GatewayClient : IDisposable
{
    public static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web)
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
    };

    private readonly HttpClient _http;

    public GatewayClient(Uri baseAddress, string token, HttpMessageHandler? handler = null)
    {
        _http = handler is null ? new HttpClient() : new HttpClient(handler, disposeHandler: true);
        _http.BaseAddress = baseAddress;
        _http.Timeout = Timeout.InfiniteTimeSpan; // per-call timeouts; the event stream is long-lived
        _http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", token);
        _http.DefaultRequestHeaders.UserAgent.ParseAdd("Cubicle-Desktop/0.1");
    }

    public Uri BaseAddress => _http.BaseAddress!;

    /// <summary>True if a gateway answers /healthz at <paramref name="baseAddress"/>.</summary>
    public static async Task<bool> IsHealthyAsync(Uri baseAddress, CancellationToken ct = default)
    {
        using var http = new HttpClient { BaseAddress = baseAddress, Timeout = TimeSpan.FromSeconds(2) };
        try
        {
            using var resp = await http.GetAsync("/healthz", ct).ConfigureAwait(false);
            return resp.IsSuccessStatusCode;
        }
        catch (Exception e) when (e is HttpRequestException or TaskCanceledException)
        {
            return false;
        }
    }

    // ---- office ----

    public Task<OfficeStatus> GetStatusAsync(CancellationToken ct = default) =>
        GetAsync<OfficeStatus>("/api/v1/status", ct);

    public Task<List<Employee>> GetEmployeesAsync(CancellationToken ct = default) =>
        GetAsync<List<Employee>>("/api/v1/employees", ct);

    public Task<List<ActivityItem>> GetActivityAsync(int limit = 100, CancellationToken ct = default) =>
        GetAsync<List<ActivityItem>>($"/api/v1/activity?limit={limit}", ct);

    public Task ResetBreakerAsync(CancellationToken ct = default) => SendAsync(HttpMethod.Post, "/api/v1/breaker/reset", null, ct);

    // ---- tasks ----

    public Task<List<TaskItem>> GetTasksAsync(string? status = null, int limit = 200, CancellationToken ct = default)
    {
        var q = $"/api/v1/tasks?limit={limit}";
        if (!string.IsNullOrEmpty(status))
            q += $"&status={Uri.EscapeDataString(status)}";
        return GetAsync<List<TaskItem>>(q, ct);
    }

    /// <summary>Only top-level assignments (what you asked for), newest first.</summary>
    public Task<List<TaskItem>> GetAssignmentsAsync(int limit = 100, CancellationToken ct = default) =>
        GetAsync<List<TaskItem>>($"/api/v1/tasks?parent_id=&limit={limit}", ct);

    public Task<TaskDetail> GetTaskAsync(string id, CancellationToken ct = default) =>
        GetAsync<TaskDetail>($"/api/v1/tasks/{Uri.EscapeDataString(id)}", ct);

    public Task<TaskItem> CreateAssignmentAsync(string title, string description, CancellationToken ct = default) =>
        SendAsync<TaskItem>(HttpMethod.Post, "/api/v1/assignments", new { title, description }, ct);

    public Task CancelTaskAsync(string id, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, $"/api/v1/tasks/{Uri.EscapeDataString(id)}/cancel", null, ct);

    // ---- inbox ----

    public Task<List<InboxItem>> GetInboxAsync(string? status = "open", CancellationToken ct = default) =>
        GetAsync<List<InboxItem>>(string.IsNullOrEmpty(status) ? "/api/v1/inbox" : $"/api/v1/inbox?status={status}", ct);

    public Task AnswerAsync(string inboxId, string answer, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, $"/api/v1/inbox/{Uri.EscapeDataString(inboxId)}/answer", new { answer }, ct);

    // ---- routines ----

    public Task<List<Routine>> GetRoutinesAsync(CancellationToken ct = default) =>
        GetAsync<List<Routine>>("/api/v1/routines", ct);

    public Task<Routine> CreateRoutineAsync(string title, string description, string schedule, CancellationToken ct = default) =>
        SendAsync<Routine>(HttpMethod.Post, "/api/v1/routines", new { title, description, schedule }, ct);

    public Task SetRoutineEnabledAsync(string id, bool enabled, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, $"/api/v1/routines/{Uri.EscapeDataString(id)}/{(enabled ? "enable" : "disable")}", null, ct);

    public Task DeleteRoutineAsync(string id, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Delete, $"/api/v1/routines/{Uri.EscapeDataString(id)}", null, ct);

    // ---- secrets (write-only) ----

    public Task<List<string>> GetSecretNamesAsync(CancellationToken ct = default) =>
        GetAsync<List<string>>("/api/v1/secrets", ct);

    public Task SetSecretAsync(string name, string value, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Put, $"/api/v1/secrets/{Uri.EscapeDataString(name)}", new { value }, ct);

    public Task DeleteSecretAsync(string name, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Delete, $"/api/v1/secrets/{Uri.EscapeDataString(name)}", null, ct);

    // ---- live events ----

    /// <summary>Streams Server-Sent Events until cancelled or the connection drops.</summary>
    public async IAsyncEnumerable<GatewayEvent> StreamEventsAsync([EnumeratorCancellation] CancellationToken ct = default)
    {
        using var req = new HttpRequestMessage(HttpMethod.Get, "/api/v1/events");
        req.Headers.Accept.ParseAdd("text/event-stream");
        using var resp = await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct).ConfigureAwait(false);
        await EnsureSuccess(resp, ct).ConfigureAwait(false);
        await using var stream = await resp.Content.ReadAsStreamAsync(ct).ConfigureAwait(false);
        await foreach (var evt in SseParser.ParseAsync(stream, ct).ConfigureAwait(false))
            yield return evt;
    }

    // ---- plumbing ----

    private async Task<T> GetAsync<T>(string path, CancellationToken ct)
    {
        using var cts = WithTimeout(ct);
        using var resp = await _http.GetAsync(path, cts.Token).ConfigureAwait(false);
        await EnsureSuccess(resp, ct).ConfigureAwait(false);
        return (await resp.Content.ReadFromJsonAsync<T>(Json, cts.Token).ConfigureAwait(false))!;
    }

    private async Task<T> SendAsync<T>(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        using var resp = await SendRaw(method, path, body, ct).ConfigureAwait(false);
        return (await resp.Content.ReadFromJsonAsync<T>(Json, ct).ConfigureAwait(false))!;
    }

    private async Task SendAsync(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        using var resp = await SendRaw(method, path, body, ct).ConfigureAwait(false);
    }

    private async Task<HttpResponseMessage> SendRaw(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        using var cts = WithTimeout(ct);
        using var req = new HttpRequestMessage(method, path);
        if (body is not null)
            req.Content = JsonContent.Create(body, options: Json);
        var resp = await _http.SendAsync(req, cts.Token).ConfigureAwait(false);
        await EnsureSuccess(resp, ct).ConfigureAwait(false);
        return resp;
    }

    private static CancellationTokenSource WithTimeout(CancellationToken ct)
    {
        var cts = CancellationTokenSource.CreateLinkedTokenSource(ct);
        cts.CancelAfter(TimeSpan.FromSeconds(15));
        return cts;
    }

    private static async Task EnsureSuccess(HttpResponseMessage resp, CancellationToken ct)
    {
        if (resp.IsSuccessStatusCode)
            return;
        var message = resp.ReasonPhrase ?? "request failed";
        try
        {
            var err = await resp.Content.ReadFromJsonAsync<ErrorBody>(Json, ct).ConfigureAwait(false);
            if (!string.IsNullOrWhiteSpace(err?.Error))
                message = err.Error;
        }
        catch (JsonException) { }
        resp.Dispose();
        throw new GatewayException(resp.StatusCode, message);
    }

    private sealed record ErrorBody(string? Error);

    public void Dispose() => _http.Dispose();
}
