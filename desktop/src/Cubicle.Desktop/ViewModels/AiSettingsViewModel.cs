using System.Collections.ObjectModel;
using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// The "AI provider" settings: which provider and model the staff use, its key,
/// effort, web access and daily budget. Saving restarts the staff with the new setup.
/// </summary>
public sealed partial class AiSettingsViewModel(Func<GatewayClient?> client, Action<string> showError, Action<string, string> notify)
    : ObservableObject
{
    public ObservableCollection<ProviderInfo> Providers { get; } = [];
    public ObservableCollection<string> Models { get; } = [];
    public IReadOnlyList<string> Efforts { get; } = ["low", "medium", "high", "xhigh", "max"];

    [ObservableProperty] private ProviderInfo? _selectedProvider;
    [ObservableProperty] private string _model = "";
    [ObservableProperty] private string _baseUrl = "";
    [ObservableProperty] private string _effort = "medium";
    [ObservableProperty] private string _ownerName = "";
    [ObservableProperty] private bool _webAccess = true;
    [ObservableProperty] private string _dailyBudget = "";
    [ObservableProperty] private string _connectionStatus = "";
    [ObservableProperty] private bool _isBusy;

    /// <summary>True while the active provider is the offline demo.</summary>
    [ObservableProperty] private bool _isDemo = true;

    public async Task LoadAsync()
    {
        if (client() is not { } c)
            return;
        var settingsTask = c.GetSettingsAsync();
        var providersTask = c.GetProvidersAsync();
        await Task.WhenAll(settingsTask, providersTask);
        var settings = settingsTask.Result;

        Providers.Clear();
        foreach (var p in providersTask.Result)
            Providers.Add(p);
        var active = Providers.FirstOrDefault(p => p.Active);
        IsDemo = active?.Kind == "demo";

        // Set the selection first: changing provider resets the model and URL below.
        SelectedProvider = Providers.FirstOrDefault(p => p.Id == settings.Provider) ?? active;
        Model = string.IsNullOrEmpty(settings.Model) ? SelectedProvider?.DefaultModel ?? "" : settings.Model;
        BaseUrl = string.IsNullOrEmpty(settings.BaseUrl) ? SelectedProvider?.BaseUrl ?? "" : settings.BaseUrl;
        Effort = settings.Effort;
        OwnerName = settings.OwnerName == "the owner" ? "" : settings.OwnerName;
        WebAccess = settings.WebAccess;
        DailyBudget = settings.DailyTokenBudget?.ToString() ?? "";
        ConnectionStatus = "";
    }

    partial void OnSelectedProviderChanged(ProviderInfo? value)
    {
        Models.Clear();
        if (value is null)
            return;
        foreach (var m in value.SuggestedModels)
            Models.Add(m);
        Model = value.DefaultModel;
        BaseUrl = value.BaseUrl ?? "";
        ConnectionStatus = value.NeedsKey && !value.Configured ? "Add your API key to connect." : "";
    }

    [RelayCommand]
    private async Task TestConnectionAsync()
    {
        if (client() is not { } c || SelectedProvider is not { } p)
            return;
        IsBusy = true;
        ConnectionStatus = "Connecting…";
        try
        {
            var result = await c.GetModelsAsync(p.Id, p.BaseUrlEditable ? BaseUrl : null);
            Models.Clear();
            foreach (var m in result.Models)
                Models.Add(m);
            ConnectionStatus = result switch
            {
                { Error: { Length: > 0 } error } => $"✗ {error}",
                { Live: true } => $"✓ Connected · {result.Models.Count} models available",
                _ => "Showing suggested models (live list unavailable while running from source).",
            };
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException or OperationCanceledException)
        {
            ConnectionStatus = $"✗ {e.Message}";
        }
        finally
        {
            IsBusy = false;
        }
    }

    /// <summary>Stores the provider's API key (encrypted by the gateway). Called with the PasswordBox value.</summary>
    public async Task SaveKeyAsync(string key)
    {
        if (client() is not { } c || SelectedProvider is not { SecretName: { Length: > 0 } secret })
            return;
        if (string.IsNullOrWhiteSpace(key))
        {
            showError("Paste the API key first.");
            return;
        }
        try
        {
            await c.SetSecretAsync(secret, key.Trim());
            var selected = SelectedProvider.Id;
            var model = Model;
            await LoadAsync();
            SelectedProvider = Providers.FirstOrDefault(p => p.Id == selected);
            Model = model;
            await TestConnectionAsync();
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException)
        {
            showError(e.Message);
        }
    }

    [RelayCommand]
    private async Task SaveAsync()
    {
        if (client() is not { } c || SelectedProvider is not { } p)
            return;
        long? budget = null;
        if (!string.IsNullOrWhiteSpace(DailyBudget))
        {
            if (!long.TryParse(DailyBudget.Replace(",", ""), out var parsed) || parsed < 0)
            {
                showError("Daily token budget must be a whole number (0 = unlimited), or empty for the default.");
                return;
            }
            budget = parsed;
        }
        try
        {
            IsBusy = true;
            await c.SaveSettingsAsync(new OfficeSettings(p.Id, Model.Trim(), Effort, p.BaseUrlEditable ? BaseUrl.Trim() : "",
                OwnerName.Trim(), WebAccess, budget));
            await LoadAsync();
            notify("Settings saved", $"Your staff now use {p.Name}{(string.IsNullOrEmpty(Model) ? "" : $" · {Model}")}.");
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
