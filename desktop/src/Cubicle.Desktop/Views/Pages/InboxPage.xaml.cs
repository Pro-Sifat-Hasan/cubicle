using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using Cubicle.Desktop.ViewModels;

namespace Cubicle.Desktop.Views.Pages;

public partial class InboxPage : UserControl
{
    private ChatViewModel? _chat;
    private bool _atBottom = true; // follow new messages only while the owner is reading the newest one

    public InboxPage()
    {
        InitializeComponent();
        DataContextChanged += (_, _) => Attach((DataContext as MainViewModel)?.Chat);
    }

    private void Attach(ChatViewModel? chat)
    {
        if (_chat is not null)
        {
            _chat.ThreadUpdated -= ScrollToNewest;
            _chat.OwnerSent -= ForceScrollToNewest;
            _chat.PropertyChanged -= ChatPropertyChanged;
        }
        _chat = chat;
        if (_chat is not null)
        {
            _chat.ThreadUpdated += ScrollToNewest;
            _chat.OwnerSent += ForceScrollToNewest;
            _chat.PropertyChanged += ChatPropertyChanged;
        }
    }

    // Opening another chat starts at its newest message.
    private void ChatPropertyChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(ChatViewModel.Selected))
            _atBottom = true;
    }

    // A scroll by the owner (not content growing) decides whether new messages pull the view down.
    private void ThreadScroll_ScrollChanged(object sender, ScrollChangedEventArgs e)
    {
        if (Math.Abs(e.ExtentHeightChange) < 0.5 && Math.Abs(e.ViewportHeightChange) < 0.5)
            _atBottom = ThreadScroll.VerticalOffset >= ThreadScroll.ScrollableHeight - 24;
    }

    // Wait for the new content to be laid out, then show the newest message, unless the owner scrolled up to read.
    private void ScrollToNewest()
    {
        if (_atBottom)
            ForceScrollToNewest();
    }

    private void ForceScrollToNewest()
    {
        _atBottom = true;
        Dispatcher.BeginInvoke(() => ThreadScroll.ScrollToEnd(), System.Windows.Threading.DispatcherPriority.Background);
    }

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
