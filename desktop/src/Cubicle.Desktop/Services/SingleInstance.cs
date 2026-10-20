namespace Cubicle.Desktop.Services;

/// <summary>
/// Ensures one Cubicle window per user. A second launch signals the first one to
/// show itself and then exits.
/// </summary>
public sealed class SingleInstance : IDisposable
{
    private const string MutexName = @"Local\Cubicle.Desktop";
    private const string ShowEventName = @"Local\Cubicle.Desktop.Show";

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
        var showEvent = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEventName);
        if (!createdNew)
        {
            showEvent.Set();
            showEvent.Dispose();
            mutex.Dispose();
            return null;
        }
        return new SingleInstance(mutex, showEvent, onShowRequested);
    }

    public void Dispose()
    {
        _wait?.Unregister(null);
        _showEvent.Dispose();
        _mutex.ReleaseMutex();
        _mutex.Dispose();
    }
}
