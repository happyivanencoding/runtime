param([Parameter(Mandatory=$true)][string]$StateDir)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName PresentationFramework, PresentationCore, WindowsBase
[xml]$xaml = @'
<Window xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation" xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml" Title="Runtime Core Accessibility Fixture" Width="520" Height="440" WindowStartupLocation="CenterScreen">
  <StackPanel Margin="20">
    <TextBlock Text="Runtime-owned accessibility test window" Margin="0,0,0,12" />
    <TextBox x:Name="Input" AutomationProperties.AutomationId="runtime-input" AutomationProperties.Name="Runtime input" Text="initial" Height="30" />
    <Button x:Name="Apply" AutomationProperties.AutomationId="runtime-apply" Content="Apply fixture" Height="30" Margin="0,8,0,0" />
    <CheckBox x:Name="Enabled" AutomationProperties.AutomationId="runtime-toggle" Content="Fixture enabled" Margin="0,8,0,0" />
    <TextBlock x:Name="Status" AutomationProperties.AutomationId="runtime-status" Text="not applied" Margin="0,8,0,8" />
    <ListBox x:Name="Items" AutomationProperties.AutomationId="runtime-list" Height="100" />
    <Button AutomationProperties.AutomationId="duplicate-a" AutomationProperties.Name="Duplicate" Content="Duplicate" />
    <Button AutomationProperties.AutomationId="duplicate-b" AutomationProperties.Name="Duplicate" Content="Duplicate" />
  </StackPanel>
</Window>
'@
$reader = New-Object System.Xml.XmlNodeReader $xaml
$window = [Windows.Markup.XamlReader]::Load($reader)
$inputControl = $window.FindName('Input')
$status = $window.FindName('Status')
$items = $window.FindName('Items')
for ($i=1; $i -le 40; $i++) { [void]$items.Items.Add("Fixture item $i") }
$window.FindName('Apply').Add_Click({
    $status.Text = 'applied: ' + $inputControl.Text
    [IO.File]::WriteAllText((Join-Path $StateDir 'applied.txt'), $inputControl.Text, [Text.UTF8Encoding]::new($false))
})
$window.Add_SourceInitialized({
    $helper = New-Object System.Windows.Interop.WindowInteropHelper $window
    [IO.File]::WriteAllText((Join-Path $StateDir 'window.txt'), $helper.Handle.ToInt64().ToString())
})
[void]$window.ShowDialog()
