using System.Diagnostics;
using System.IO;
using Cubicle.Client;

namespace Cubicle.Desktop.Services;

/// <summary>
/// Starts the bundled gateway (which in turn supervises the agent worker) when it
/// is not already running. The gateway is a detached background process, so your
/// staff keep working after this window closes.
/// </summary>
public static class GatewayHost
{
    private const string ExeName = "cubicle-gateway.exe";

    public static string? FindGatewayExe()
    {
        var overridePath = Environment.GetEnvironmentVariable("CUBICLE_GATEWAY_EXE");
        if (!string.IsNullOrWhiteSpace(overridePath) && File.Exists(overridePath))
            return overridePath;
        var bundled = Path.Combine(AppContext.BaseDirectory, ExeName);
        return File.Exists(bundled) ? bundled : null;
    }

    public static async Task<bool> EnsureRunningAsync(Uri baseAddress, CancellationToken ct)
    {
        if (await GatewayClient.IsHealthyAsync(baseAddress, ct))
            return true;

        var exe = FindGatewayExe();
        if (exe is null)
            return false;

        Process.Start(new ProcessStartInfo(exe, "serve")
        {
            UseShellExecute = false,
            CreateNoWindow = true,
            WorkingDirectory = Path.GetDirectoryName(exe)!,
        });

        var deadline = DateTime.UtcNow.AddSeconds(15);
        while (DateTime.UtcNow < deadline && !ct.IsCancellationRequested)
        {
            if (await GatewayClient.IsHealthyAsync(baseAddress, ct))
                return true;
            await Task.Delay(300, ct);
        }
        return false;
    }

    /// <summary>Stops gateways started from this install (and their agent workers).</summary>
    public static void Stop()
    {
        var exe = FindGatewayExe();
        foreach (var p in Process.GetProcessesByName(Path.GetFileNameWithoutExtension(ExeName)))
        {
            using (p)
            {
                try
                {
                    if (exe is null || string.Equals(p.MainModule?.FileName, exe, StringComparison.OrdinalIgnoreCase))
                        p.Kill(entireProcessTree: true);
                }
                catch (Exception e) when (e is InvalidOperationException or System.ComponentModel.Win32Exception)
                {
                    // Not ours (e.g. a Windows service) or already exited.
                }
            }
        }
    }
}
