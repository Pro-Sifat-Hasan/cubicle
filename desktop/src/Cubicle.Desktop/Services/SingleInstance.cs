namespace Cubicle.Desktop.Services;

/// <summary>
/// Ensures one Cubicle window per user. A second launch signals the first one to
/// show itself and then exits.
/// </summary>
public sealed class SingleInstance : IDisposable
{
    // CUBICLE_INSTANCE (set by portable mode or for development) gives a separate lock,
    // so that copy can run next to the installed app. Read on use, after startup set it.
    private static string Suffix => Environment.GetEnvironmentVariable("CUBICLE_INSTANCE") is { Length: > 0 } s ? "." + s : "";
    private static string MutexName => @"Local\Cubicle.Desktop" + Suffix;
    private static string ShowEventName => @"Local\Cubicle.Desktop.Show" + Suffix;

    private readonly Mutex _mutex;
    private readonly EventWaitHandle _showEvent;
    private readonly RegisteredWaitHandle? _wait;

    private SingleInstance(Mutex mutex, EventWaitHandle showEvent, Action onShowRequested)
    {
        _mutex = mutex;
        _showEvent = showEvent;
        _wait = ThreadPool.RegisterWaitForSingleObject(showEvent, (_, _) => onShowRequested(), null, Timeout.Infinite, executeOnlyOnce: false);
    }

    /// <summary>Returns null if another instance owns the lock (after asking it to show itself).</summary>
    public static SingleInstance? TryAcquire(Action onShowRequested)
    {
        var mutex = new Mutex(initiallyOwned: true, MutexName, out var createdNew);
        EventWaitHandle? showEvent = null;
        try
        {
            showEvent = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEventName);
            if (createdNew)
                return new SingleInstance(mutex, showEvent, onShowRequested);
            showEvent.Set(); // ask the running instance to show itself
        }
        catch
        {
            showEvent?.Dispose();
            mutex.Dispose();
            throw;
        }
        showEvent.Dispose();
        mutex.Dispose();
        return null;
    }

    public void Dispose()
    {
        _wait?.Unregister(null);
        _showEvent.Dispose();
        _mutex.ReleaseMutex();
        _mutex.Dispose();
    }
}
