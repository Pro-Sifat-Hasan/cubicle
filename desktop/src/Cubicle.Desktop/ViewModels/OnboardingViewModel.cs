using System.Collections.ObjectModel;
using System.Net.Http;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>One line of the "hiring" animation.</summary>
public sealed partial class HireLine(string text) : ObservableObject
{
    public string Text { get; } = text;
    [ObservableProperty] private bool _isDone;
}

/// <summary>
/// First-run setup in five short screens: name your chief of staff, pick a profession, pick a passion
/// (or type your own), watch the team get hired, and start a first mission. The profession decides the team
/// and tools; the passion decides the voice and look.
/// </summary>
public sealed partial class OnboardingViewModel : ObservableObject
{
    public const int NameStep = 0, ProfessionStep = 1, PassionStep = 2, HiringStep = 3, MissionStep = 4;
    private const int PersonaTimeoutSeconds = 60;

    private readonly Func<GatewayClient?> _client;
    private readonly Action<string> _showError;
    private OnboardingResult? _result;

    public OnboardingViewModel(Func<GatewayClient?> client, Action<string> showError)
    {
        _client = client;
        _showError = showError;
    }

    /// <summary>Raised when onboarding is over (finished or skipped) with the final profile.</summary>
    public event Action<OwnerProfile?>? Finished;

    /// <summary>Raised when the owner picks a first mission.</summary>
    public event Action<string>? MissionChosen;

    public IReadOnlyList<string> NameSuggestions { get; } = ["Atlas", "Juno", "Echo", "Sage", "Orion", "Ember"];

    public ObservableCollection<Profession> Professions { get; } = [];
    public ObservableCollection<Persona> Passions { get; } = [];
    public ObservableCollection<HireLine> HireLines { get; } = [];
    public ObservableCollection<string> Missions { get; } = [];

    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(IsNameStep), nameof(IsProfessionStep), nameof(IsPassionStep), nameof(IsHiringStep),
        nameof(IsMissionStep), nameof(StepText), nameof(CanGoBack), nameof(ShowNext))]
    private int _step;

    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(NextCommand))]
    private string _agentName = "";

    [ObservableProperty] private string _ownerName = "";

    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(NextCommand))]
    private Profession? _selectedProfession;

    [ObservableProperty] private string _stackText = "";

    [ObservableProperty, NotifyPropertyChangedFor(nameof(IsCustomPassion)), NotifyCanExecuteChangedFor(nameof(NextCommand))]
    private Persona? _selectedPassion;

    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(NextCommand))]
    private string _customPassionText = "";

    /// <summary>The persona the model wrote for the custom passion, once generated.</summary>
    [ObservableProperty, NotifyPropertyChangedFor(nameof(HasCustomPersona))]
    private Persona? _customPersona;

    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(NextCommand))]
    private bool _isBusy;

    [ObservableProperty] private string _busyText = "";
    [ObservableProperty] private string _note = "";
    [ObservableProperty] private bool _hiringDone;
    [ObservableProperty] private bool _hiringFailed;

    public bool IsNameStep => Step == NameStep;
    public bool IsProfessionStep => Step == ProfessionStep;
    public bool IsPassionStep => Step == PassionStep;
    public bool IsHiringStep => Step == HiringStep;
    public bool IsMissionStep => Step == MissionStep;
    public bool IsCustomPassion => SelectedPassion?.Id == Persona.CustomId;
    public bool HasCustomPersona => CustomPersona is not null;
    public bool CanGoBack => Step is ProfessionStep or PassionStep && !IsBusy;
    public bool ShowNext => Step <= PassionStep;
    public string StepText => Step switch
    {
        <= PassionStep => $"Step {Step + 1} of 5",
        HiringStep => "Step 4 of 5",
        _ => "Step 5 of 5",
    };

    public string NextText => Step == PassionStep ? "Hire my team" : "Continue";

    partial void OnStepChanged(int value) => OnPropertyChanged(nameof(NextText));
    partial void OnIsBusyChanged(bool value) => OnPropertyChanged(nameof(CanGoBack));

    // Typing a different passion invalidates the persona written for the previous one.
    partial void OnCustomPassionTextChanged(string value) => CustomPersona = null;

    /// <summary>Starts (or restarts) onboarding with the packs from the gateway.</summary>
    public void Begin(OnboardingPacks packs, string ownerName)
    {
        Professions.Clear();
        foreach (var p in packs.Professions)
            Professions.Add(p);
        Passions.Clear();
        foreach (var p in packs.Passions)
            Passions.Add(p);
        Passions.Add(new Persona(Persona.CustomId, "Custom…", "✨", "", [], [], "#7C5CFF", []));
        HireLines.Clear();
        Missions.Clear();
        _result = null;
        AgentName = packs.Profile.AgentName;
        OwnerName = ownerName;
        SelectedProfession = null;
        SelectedPassion = null;
        CustomPassionText = "";
        StackText = "";
        Note = "";
        HiringDone = HiringFailed = false;
        Step = NameStep;
    }

    [RelayCommand]
    private void PickName(string name) => AgentName = name;

    private bool CanNext() => !IsBusy && Step switch
    {
        NameStep => !string.IsNullOrWhiteSpace(AgentName),
        ProfessionStep => SelectedProfession is not null,
        PassionStep => SelectedPassion is not null
            && (!IsCustomPassion || !string.IsNullOrWhiteSpace(CustomPassionText)),
        _ => false,
    };

    [RelayCommand(CanExecute = nameof(CanNext))]
    private async Task NextAsync()
    {
        switch (Step)
        {
            case NameStep:
                Step = ProfessionStep;
                break;
            case ProfessionStep:
                Step = PassionStep;
                break;
            case PassionStep:
                if (IsCustomPassion && CustomPersona is null)
                    await GeneratePersonaAsync();
                Step = HiringStep;
                await HireAsync();
                break;
        }
    }

    [RelayCommand]
    private void Back()
    {
        if (CanGoBack)
            Step--;
    }

    /// <summary>Lets the main agent write a persona for the typed passion. Falls back quietly if no model can.</summary>
    private async Task GeneratePersonaAsync()
    {
        var client = _client();
        if (client is null)
            return;
        IsBusy = true;
        BusyText = $"{AgentName.Trim()} is working out their personality…";
        Note = "";
        try
        {
            var taskId = await client.StartPersonaAsync(CustomPassionText.Trim());
            var deadline = DateTime.UtcNow.AddSeconds(PersonaTimeoutSeconds);
            while (DateTime.UtcNow < deadline)
            {
                await Task.Delay(1000);
                var status = await client.GetPersonaAsync(taskId);
                if (status.IsDone)
                {
                    CustomPersona = status.Persona;
                    return;
                }
                if (status.IsFailed)
                    break;
            }
            Note = "No AI answered in time, so your chief of staff starts with a simple personality. " +
                   "Connect an AI in Settings and run setup again to have one written.";
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException or OperationCanceledException)
        {
            Note = $"Could not write a personality ({e.Message}). Continuing with a simple one.";
        }
        finally
        {
            IsBusy = false;
            BusyText = "";
        }
    }

    private async Task HireAsync()
    {
        var client = _client();
        var profession = SelectedProfession;
        var passion = SelectedPassion;
        if (client is null || profession is null || passion is null)
            return;

        HiringDone = HiringFailed = false;
        HireLines.Clear();
        var custom = IsCustomPassion;
        var request = new OnboardingRequest(
            AgentName.Trim(), OwnerName.Trim(), profession.Id, custom ? Persona.CustomId : passion.Id,
            custom ? CustomPassionText.Trim() : null, custom ? CustomPersona : null, ParseStack(StackText));

        var call = Task.Run(() => client.CompleteOnboardingAsync(request));
        foreach (var line in Script(profession))
        {
            var item = new HireLine(line);
            HireLines.Add(item);
            await Task.Delay(650);
            item.IsDone = true;
        }

        try
        {
            _result = await call;
        }
        catch (Exception e) when (e is GatewayException or HttpRequestException or OperationCanceledException)
        {
            HiringFailed = true;
            _showError($"Setup failed: {e.Message}");
            return;
        }
        HiringDone = true;
        foreach (var m in _result.StarterMissions)
            Missions.Add(m);
    }

    private IEnumerable<string> Script(Profession profession)
    {
        yield return $"Welcoming {AgentName.Trim()} as your chief of staff…";
        foreach (var t in profession.Team)
            yield return $"Hiring your {t.Title}…";
        foreach (var r in profession.Routines.Where(r => r.Enabled))
            yield return $"Setting up your {r.Title.ToLowerInvariant()}…";
        yield return "Writing down what they should know about you…";
    }

    internal static List<string> ParseStack(string text) =>
        text.Split([',', ';', '\n'], StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries)
            .Distinct(StringComparer.OrdinalIgnoreCase).Take(12).ToList();

    /// <summary>After a failed hire, goes back to the passion screen so the owner can try again.</summary>
    [RelayCommand]
    private void RetryHiring()
    {
        HiringFailed = false;
        Step = PassionStep;
    }

    [RelayCommand]
    private void Continue() => Step = MissionStep;

    [RelayCommand]
    private void ChooseMission(string mission)
    {
        MissionChosen?.Invoke(mission);
        Finished?.Invoke(_result?.Profile);
    }

    [RelayCommand]
    private void Finish() => Finished?.Invoke(_result?.Profile);
}
