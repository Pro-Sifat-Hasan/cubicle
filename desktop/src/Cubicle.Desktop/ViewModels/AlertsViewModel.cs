using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// "Reach me elsewhere": a Discord message or an email when the staff need an answer, so nothing waits unseen
/// while the window is closed. The webhook and the mail password are sent once and never shown again.
/// </summary>
public sealed partial class AlertsViewModel(Func<GatewayClient?> client, Action<string> showError, Action<string, string> notify)
    : ObservableObject
{
    [ObservableProperty] private bool _waiting = true;
    [ObservableProperty] private bool _done;
    [ObservableProperty] private bool _discord;
    [ObservableProperty] private bool _email;
    [ObservableProperty] private string _emailTo = "";
    [ObservableProperty] private string _smtpHost = "";
    [ObservableProperty] private string _smtpPort = "587";
    [ObservableProperty] private string _smtpUser = "";
    [ObservableProperty] private string _emailFrom = "";
    [ObservableProperty] private string _webhook = "";
    [ObservableProperty] private string _mailPassword = "";
    [ObservableProperty] private bool _webhookSet;
    [ObservableProperty] private bool _mailPasswordSet;
    [ObservableProperty] private bool _isBusy;

    public async Task LoadAsync()
    {
        if (client() is not { } c)
            return;
        var a = await c.GetAlertsAsync();
        Waiting = a.Waiting;
        Done = a.Done;
        Discord = a.Discord;
        Email = a.Email;
        EmailTo = a.EmailTo;
        SmtpHost = a.SmtpHost;
        SmtpPort = a.SmtpPort.ToString();
        SmtpUser = a.SmtpUser;
        EmailFrom = a.EmailFrom;
        WebhookSet = a.DiscordWebhookSet;
        MailPasswordSet = a.SmtpPasswordSet;
    }

    [RelayCommand]
    private Task SaveAsync() => RunAsync(async c =>
    {
        await SaveCoreAsync(c);
        notify("Alerts saved", Discord || Email ? "You will be told outside Cubicle when your staff need you." : "Alerts outside Cubicle are off.");
    });

    [RelayCommand]
    private Task TestDiscordAsync() => RunAsync(async c =>
    {
        await SaveCoreAsync(c);
        await c.TestAlertAsync("discord");
        notify("Discord test sent", "Check your Discord channel.");
    });

    [RelayCommand]
    private Task TestEmailAsync() => RunAsync(async c =>
    {
        await SaveCoreAsync(c);
        await c.TestAlertAsync("email");
        notify("Test email sent", $"Check {EmailTo}.");
    });

    private async Task SaveCoreAsync(GatewayClient c)
    {
        if (!int.TryParse(SmtpPort.Trim(), out var port))
            throw new GatewayException(System.Net.HttpStatusCode.BadRequest, "The mail server port must be a number, usually 587.");
        var webhook = Webhook.Trim();
        var password = MailPassword.Trim();
        await c.SaveAlertsAsync(new AlertSettings(Waiting, Done, Discord, Email, EmailTo.Trim(), SmtpHost.Trim(), port,
            SmtpUser.Trim(), EmailFrom.Trim(), WebhookSet, MailPasswordSet), webhook, password);
        Webhook = "";
        MailPassword = "";
        await LoadAsync();
    }

    private async Task RunAsync(Func<GatewayClient, Task> action)
    {
        if (client() is not { } c || IsBusy)
            return;
        try
        {
            IsBusy = true;
            await action(c);
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException)
        {
            showError(e.Message);
        }
        finally
        {
            IsBusy = false;
        }
    }
}
