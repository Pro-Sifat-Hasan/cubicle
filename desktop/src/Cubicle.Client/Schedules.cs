namespace Cubicle.Client;

/// <summary>Builds and describes routine schedules (format defined in gateway/internal/store/routines.go).</summary>
public static class Schedules
{
    public static string Daily(TimeOnly at) => $"daily@{at:HH\\:mm}";

    public static string Weekdays(TimeOnly at) => $"weekdays@{at:HH\\:mm}";

    public static string Every(TimeSpan interval)
    {
        if (interval < TimeSpan.FromMinutes(5))
            throw new ArgumentOutOfRangeException(nameof(interval), "Interval must be at least 5 minutes.");
        return interval.TotalHours >= 1 && interval.Minutes == 0
            ? $"every:{(int)interval.TotalHours}h"
            : $"every:{(int)interval.TotalMinutes}m";
    }

    /// <summary>Human-friendly text, e.g. "Every weekday at 09:00".</summary>
    public static string Describe(string schedule)
    {
        var s = schedule.Trim().ToLowerInvariant();
        if (s.StartsWith("every:", StringComparison.Ordinal))
            return $"Every {s[6..]}";
        var parts = s.Split('@', 2);
        if (parts.Length == 2)
        {
            return parts[0] switch
            {
                "daily" => $"Every day at {parts[1]}",
                "weekdays" => $"Every weekday at {parts[1]}",
                _ => schedule,
            };
        }
        return schedule;
    }
}
