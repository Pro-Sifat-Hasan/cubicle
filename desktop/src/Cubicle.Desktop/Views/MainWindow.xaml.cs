using System.Windows;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Views;

public partial class MainWindow : Window
{
    public MainWindow() => InitializeComponent();

    // PasswordBox does not support binding by design; hand the key to the
    // view model directly and clear the box immediately afterwards.
    private async void SaveApiKey_Click(object sender, RoutedEventArgs e)
    {
        if (DataContext is not MainViewModel vm)
            return;
        var key = ApiKeyBox.Password;
        ApiKeyBox.Clear();
        await vm.SaveApiKeyAsync(key);
    }
}
