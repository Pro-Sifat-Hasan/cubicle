using System.Collections.ObjectModel;
using CommunityToolkit.Mvvm.ComponentModel;
using Cubicle.Client;

namespace Cubicle.Desktop.ViewModels;

/// <summary>
/// One line of an agent's working timeline: a thought, a remark, or a tool call with its input and output.
/// It is updated in place while the agent works, so a call first shows its input and later gains its output.
/// </summary>
public sealed partial class StepViewModel : ObservableObject
{
    private readonly OpenSections _sections;

    public StepViewModel(AgentStep step, Agent? agent, string replyAgentId, OpenSections sections)
    {
        _sections = sections;
        Step = step;
        Id = agent?.Id ?? step.AgentId ?? "";
        Name = agent?.Name ?? "Agent";
        // A teammate's steps are marked with their face and name; the agent replying needs no label.
        ShowAgent = !string.IsNullOrEmpty(step.AgentId) && step.AgentId != replyAgentId;
        _isOpen = sections.IsOpen(Key);
    }

    public AgentStep Step { get; private set; }

    private string Key => "tool:" + Step.TaskId + ":" + Step.Id;

    /// <summary>The agent's id and name, for the avatar next to a teammate's steps.</summary>
    public string Id { get; }
    public string Name { get; }
    public bool ShowAgent { get; }

    public string StepId => Step.Id;
    public bool IsThinking => Step.IsThinking;
    public bool IsNote => Step.IsNote;
    public bool IsTool => Step.IsTool;
    public bool IsRunning => Step.IsRunning;
    public bool IsDone => !Step.IsRunning && !Step.Failed;
    public bool Failed => Step.Failed;

    public string Title => Step.IsThinking && !string.IsNullOrWhiteSpace(Step.Name) ? $"Thinking · {Step.Name}" : Step.Title;
    public string Summary => Step.Summary;

    /// <summary>A remark or a thought's text.</summary>
    public string Text => Step.Output ?? "";
    public bool HasText => (Step.IsNote || Step.IsThinking) && !string.IsNullOrWhiteSpace(Step.Output);

    public string InputText => StepText.DisplayInput(Step.Input);
    public bool HasInput => Step.IsTool && !string.IsNullOrWhiteSpace(Step.Input);
    public string OutputText => StepText.Clip(Step.Output);
    public bool HasOutput => Step.IsTool && !string.IsNullOrWhiteSpace(Step.Output);

    /// <summary>A symbol for the kind of tool (Segoe Fluent Icons).</summary>
    public string Glyph => Step.IsThinking ? "" : Step.IsNote ? "" : (Step.Name ?? "") switch
    {
        "Read" or "read_file" or "list_files" => "",
        "Write" or "write_file" or "Edit" or "edit_file" => "",
        "Glob" or "Grep" or "find_files" or "grep_files" => "",
        "WebSearch" or "web_search" or "WebFetch" or "web_fetch" => "",
        "run_command" => "",
        "ask_teammate" or "start_assignment" => "",
        "remember" or "forget" => "",
        "load_skill" => "",
        "ask_owner" or "request_approval" => "",
        _ => "",
    };

    /// <summary>How long a finished call took, e.g. "1.4s".</summary>
    public string Took
    {
        get
        {
            if (Step.IsRunning)
                return "";
            var seconds = (Step.UpdatedAt - Step.CreatedAt).TotalSeconds;
            return seconds < 0.1 ? "" : seconds < 60 ? $"{seconds:0.#}s" : $"{(int)(seconds / 60)}m {(int)(seconds % 60)}s";
        }
    }

    [ObservableProperty] private bool _isOpen;

    partial void OnIsOpenChanged(bool value) => _sections.Set(Key, value);

    /// <summary>Takes the newest state of the step and tells the view what changed.</summary>
    public void Update(AgentStep step)
    {
        Step = step;
        OnPropertyChanged(string.Empty);
    }
}

/// <summary>
/// The steps of every task in the open chat, kept up to date from live events. One collection per task, so the
/// live panel and the "How it was done" section under a finished reply show the very same lines.
/// </summary>
public sealed class StepStore(OpenSections sections)
{
    private readonly Dictionary<string, ObservableCollection<StepViewModel>> _byTask = [];

    public ObservableCollection<StepViewModel> For(string taskId)
    {
        if (!_byTask.TryGetValue(taskId, out var list))
            _byTask[taskId] = list = [];
        return list;
    }

    public void Clear() => _byTask.Clear();

    /// <summary>Adds the step, or updates it when it is already shown.</summary>
    public void Upsert(AgentStep step, IReadOnlyDictionary<string, Agent> agents, string replyAgentId)
    {
        var list = For(step.TaskId);
        var existing = list.FirstOrDefault(s => s.StepId == step.Id);
        if (existing is not null)
            existing.Update(step);
        else
            list.Add(new StepViewModel(step, step.AgentId is { } id ? agents.GetValueOrDefault(id) : null, replyAgentId, sections));
    }

    /// <summary>Replaces what is shown with a fresh load, keeping the lines already on screen (and their open state).</summary>
    public void Load(IEnumerable<AgentStep> steps, IReadOnlyDictionary<string, Agent> agents, string replyAgentId)
    {
        var loaded = steps.ToList();
        foreach (var taskId in loaded.Select(s => s.TaskId).Distinct())
        {
            var fresh = loaded.Where(s => s.TaskId == taskId).ToList();
            var list = For(taskId);
            for (var i = list.Count - 1; i >= 0; i--)
                if (fresh.All(s => s.Id != list[i].StepId))
                    list.RemoveAt(i);
            foreach (var step in fresh)
                Upsert(step, agents, replyAgentId);
        }
    }
}

/// <summary>Reveals text a few characters at a time, the way an agent seems to type it, then catches up if more arrives.</summary>
public sealed class TypeWriter
{
    private string _target = "";
    private int _shown;

    public string Shown => _target[.._shown];
    public bool Done => _shown >= _target.Length;

    /// <summary>Sets the full text so far. Text that continues what was shown keeps typing; anything else starts over.</summary>
    public void SetTarget(string text)
    {
        if (!text.StartsWith(_target[.._shown], StringComparison.Ordinal))
            _shown = 0;
        _target = text;
    }

    public void Finish() => _shown = _target.Length;

    /// <summary>Advances one frame; returns true when something new is shown.</summary>
    public bool Step()
    {
        if (Done)
            return false;
        var remaining = _target.Length - _shown;
        // Slow text types at a readable pace; a long backlog is caught up in about a second.
        var chunk = Math.Max(2, remaining / 18);
        _shown = Math.Min(_target.Length, _shown + chunk);
        // Never stop in the middle of a surrogate pair.
        if (_shown < _target.Length && char.IsHighSurrogate(_target[_shown - 1]))
            _shown++;
        return true;
    }
}
