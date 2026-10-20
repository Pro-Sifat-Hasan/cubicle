using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Views.Pages;

public partial class InboxPage : UserControl
{
    private ChatViewModel? _chat;

    public InboxPage()
    {
        InitializeComponent();
        DataContextChanged += (_, _) => Attach((DataContext as MainViewModel)?.Chat);
    }

    private void Attach(ChatViewModel? chat)
    {
        if (_chat is not null)
            _chat.ThreadUpdated -= ScrollToNewest;
        _chat = chat;
        if (_chat is not null)
            _chat.ThreadUpdated += ScrollToNewest;
    }

    // Wait for the new messages to be laid out, then show the newest one.
    private void ScrollToNewest() =>
        Dispatcher.BeginInvoke(() => ThreadScroll.ScrollToEnd(), System.Windows.Threading.DispatcherPriority.Background);

    // Enter sends; Shift+Enter inserts a new line.
    private void Composer_PreviewKeyDown(object sender, KeyEventArgs e)
    {
        if (e.Key != Key.Enter || Keyboard.Modifiers.HasFlag(ModifierKeys.Shift) || _chat is null)
            return;
        e.Handled = true;
        if (_chat.SendCommand.CanExecute(null))
            _chat.SendCommand.Execute(null);
    }
}
