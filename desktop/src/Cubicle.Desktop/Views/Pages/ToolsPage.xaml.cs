using System.Windows;
using System.Windows.Controls;
using Cubicle.Desktop.ViewModels;
using Microsoft.Win32;

namespace Cubicle.Desktop.Views.Pages;

public partial class ToolsPage : UserControl
{
    public ToolsPage() => InitializeComponent();

    private ToolsViewModel? Tools => (DataContext as MainViewModel)?.Tools;

    // PasswordBox does not support binding by design; hand the value to the
    // view model directly and clear the box immediately afterwards.
    private async void SaveSecret_Click(object sender, RoutedEventArgs e)
    {
        if (Tools is not { } tools || sender is not Button { Tag: PasswordBox box, DataContext: SecretStatus secret })
            return;
        var value = box.Password;
        box.Clear();
        await tools.SaveSecretAsync(secret.Name, value);
    }

    private void ImportSkill_Click(object sender, RoutedEventArgs e)
    {
        var dialog = new OpenFileDialog { Title = "Import a skill", Filter = "Skill (SKILL.md)|*.md|All files|*.*" };
        if (dialog.ShowDialog(Window.GetWindow(this)) == true)
            Tools?.ImportSkillFile(dialog.FileName);
    }

    private async void ImportPlugin_Click(object sender, RoutedEventArgs e)
    {
        var dialog = new OpenFolderDialog { Title = "Choose a Claude Code plugin folder" };
        if (dialog.ShowDialog(Window.GetWindow(this)) == true && Tools is { } tools)
            await tools.ImportPluginFolderAsync(dialog.FolderName);
    }
}
