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
    }

    [Fact]
    public void Rejects_too_frequent_intervals() =>
        Assert.Throws<ArgumentOutOfRangeException>(() => Schedules.Every(TimeSpan.FromMinutes(1)));

    [Theory]
    [InlineData("weekdays@09:00", "Every weekday at 09:00")]
    [InlineData("daily@18:15", "Every day at 18:15")]
    [InlineData("every:30m", "Every 30m")]
    public void Describes_schedules(string schedule, string expected) =>
        Assert.Equal(expected, Schedules.Describe(schedule));
}
