using System.Windows;
using System.Windows.Controls;

namespace Cubicle.Desktop.Controls;

/// <summary>
/// Lets a PasswordBox bind to a string: <c>controls:PasswordBoxAssistant.Password="{Binding Value, Mode=TwoWay}"</c>.
/// The value lives only in memory until the owner presses Connect, and the view model clears it afterwards.
/// </summary>
public static class PasswordBoxAssistant
{
    public static readonly DependencyProperty PasswordProperty = DependencyProperty.RegisterAttached(
        "Password", typeof(string), typeof(PasswordBoxAssistant),
        new FrameworkPropertyMetadata(null, FrameworkPropertyMetadataOptions.BindsTwoWayByDefault, OnPasswordChanged)); // null default: the first bound "" is a change, which attaches the handler

    private static readonly DependencyProperty UpdatingProperty = DependencyProperty.RegisterAttached(
        "Updating", typeof(bool), typeof(PasswordBoxAssistant), new PropertyMetadata(false));

    public static string GetPassword(DependencyObject d) => (string?)d.GetValue(PasswordProperty) ?? "";

    public static void SetPassword(DependencyObject d, string value) => d.SetValue(PasswordProperty, value);

    private static void OnPasswordChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        if (d is not PasswordBox box)
            return;
        box.PasswordChanged -= OnBoxChanged;
        if (!(bool)box.GetValue(UpdatingProperty) && box.Password != (string)e.NewValue)
            box.Password = (string?)e.NewValue ?? "";
        box.PasswordChanged += OnBoxChanged;
    }

    private static void OnBoxChanged(object sender, RoutedEventArgs e)
    {
        var box = (PasswordBox)sender;
        box.SetValue(UpdatingProperty, true);
        SetPassword(box, box.Password);
        box.SetValue(UpdatingProperty, false);
    }
}
