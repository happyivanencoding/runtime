using System.Threading;
using System.Windows;

namespace Runtime.Control;

public partial class App : System.Windows.Application
{
    private const string InstanceMutexName = @"Local\Runtime.Control.SingleInstance";
    private const string ActivateEventName = @"Local\Runtime.Control.Activate";
    private Mutex? _instanceMutex;
    private EventWaitHandle? _activateEvent;
    private RegisteredWaitHandle? _activateRegistration;

    protected override void OnStartup(StartupEventArgs e)
    {
        try
        {
            _instanceMutex = new Mutex(false, InstanceMutexName, out var firstInstance);
            if (!firstInstance)
            {
                if (!e.Args.Contains("--tray", StringComparer.OrdinalIgnoreCase))
                {
                    try
                    {
                        using var activate = EventWaitHandle.OpenExisting(ActivateEventName);
                        activate.Set();
                    }
                    catch (WaitHandleCannotBeOpenedException) { }
                    catch (UnauthorizedAccessException) { }
                }

                Shutdown();
                return;
            }

            _activateEvent = new EventWaitHandle(false, EventResetMode.AutoReset, ActivateEventName);
            _activateRegistration = ThreadPool.RegisterWaitForSingleObject(
                _activateEvent,
                (_, _) =>
                {
                    if (!Dispatcher.HasShutdownStarted)
                    {
                        Dispatcher.BeginInvoke((Action)(() =>
                        {
                            if (MainWindow is Runtime.Control.MainWindow window)
                            {
                                window.RestoreFromTray();
                            }
                        }));
                    }
                },
                null,
                Timeout.Infinite,
                executeOnlyOnce: false);

            base.OnStartup(e);
        }
        catch (UnauthorizedAccessException)
        {
            // An elevated instance may prevent a standard process from opening its mutex.
            Shutdown();
        }
    }

    protected override void OnExit(ExitEventArgs e)
    {
        _activateRegistration?.Unregister(null);
        _activateEvent?.Dispose();
        _instanceMutex?.Dispose();
        base.OnExit(e);
    }
}