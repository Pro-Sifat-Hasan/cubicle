using System.Globalization;

namespace Cubicle.Client;

/// <summary>Builds and describes automation schedules (format defined in gateway/internal/store/routines.go).</summary>
public static class Schedules
{
    private static readonly string[] DayCodes = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

    public static string Daily(TimeOnly at) => $"daily@{at:HH\\:mm}";

    public static string Weekdays(TimeOnly at) => $"weekdays@{at:HH\\:mm}";

    /// <summary>On the given days, e.g. weekly@mon,thu@08:30.</summary>
    public static string Weekly(IEnumerable<DayOfWeek> days, TimeOnly at)
    {
        var list = days.Distinct().OrderBy(d => ((int)d + 6) % 7).Select(d => DayCodes[(int)d]).ToList();
        if (list.Count == 0)
            throw new ArgumentException("Pick at least one day.", nameof(days));
        return $"weekly@{string.Join(',', list)}@{at:HH\\:mm}";
    }

    /// <summary>A single run at a local date and time.</summary>
    public static string Once(DateTime at) => $"once@{at.ToString("yyyy-MM-dd'T'HH:mm", CultureInfo.InvariantCulture)}";

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
        if (s.StartsWith("once@", StringComparison.Ordinal) &&
            DateTime.TryParseExact(s[5..], "yyyy-MM-dd't'HH:mm", CultureInfo.InvariantCulture, DateTimeStyles.None, out var at))
            return string.Create(CultureInfo.InvariantCulture, $"Once on {at:ddd d MMM yyyy} at {at:HH:mm}");
        var parts = s.Split('@');
        if (parts.Length == 3 && parts[0] == "weekly")
        {
            var days = parts[1].Split(',').Select(d => CultureInfo.InvariantCulture.TextInfo.ToTitleCase(d));
            return $"Every {string.Join(", ", days)} at {parts[2]}";
        }
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
