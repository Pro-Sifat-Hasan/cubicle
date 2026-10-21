namespace Cubicle.Client.Tests;

public class SchedulesTests
{
    [Fact]
    public void Builds_gateway_schedule_strings()
    {
        Assert.Equal("daily@07:30", Schedules.Daily(new TimeOnly(7, 30)));
        Assert.Equal("weekdays@09:00", Schedules.Weekdays(new TimeOnly(9, 0)));
        Assert.Equal("every:2h", Schedules.Every(TimeSpan.FromHours(2)));
        Assert.Equal("every:90m", Schedules.Every(TimeSpan.FromMinutes(90)));
        Assert.Equal("weekly@mon,thu,sun@08:15",
            Schedules.Weekly([DayOfWeek.Sunday, DayOfWeek.Thursday, DayOfWeek.Monday], new TimeOnly(8, 15)));
        Assert.Equal("once@2026-10-21T09:05", Schedules.Once(new DateTime(2026, 10, 21, 9, 5, 0)));
    }

    [Fact]
    public void Rejects_too_frequent_intervals_and_empty_weeks()
    {
        Assert.Throws<ArgumentOutOfRangeException>(() => Schedules.Every(TimeSpan.FromMinutes(1)));
        Assert.Throws<ArgumentException>(() => Schedules.Weekly([], new TimeOnly(9, 0)));
    }

    [Theory]
    [InlineData("weekdays@09:00", "Every weekday at 09:00")]
    [InlineData("daily@18:15", "Every day at 18:15")]
    [InlineData("every:30m", "Every 30m")]
    [InlineData("weekly@mon,fri@08:00", "Every Mon, Fri at 08:00")]
    [InlineData("once@2026-10-21T09:05", "Once on Wed 21 Oct 2026 at 09:05")]
    public void Describes_schedules(string schedule, string expected) =>
        Assert.Equal(expected, Schedules.Describe(schedule));
}
