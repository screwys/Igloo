using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.IO.Pipes;
using System.Linq;
using System.Reflection;
using System.ServiceProcess;
using System.Threading;
using System.Threading.Tasks;
using System.Text;
using System.Web.Script.Serialization;
using System.Windows.Forms;
using Microsoft.Win32;

namespace Igloo.Windows
{
    public sealed class UpdateStatus
    {
        public bool supported, checking, applying;
        public string current_app, current_runtime, available_app, available_runtime, last_error;

        public static bool Reached(string current, string target)
        {
            if (string.IsNullOrEmpty(target)) return true;
            if (string.IsNullOrEmpty(current)) return false;
            current = current.Trim().TrimStart('v');
            target = target.Trim().TrimStart('v');
            var installed = current.Split('.');
            var offered = target.Split('.');
            if (installed.Concat(offered).Any(p => p.Length == 0 || p.Any(c => c < '0' || c > '9')))
                return string.CompareOrdinal(current, target) >= 0;
            for (int i = 0; i < Math.Max(installed.Length, offered.Length); i++)
            {
                long a = i < installed.Length ? long.Parse(installed[i]) : 0;
                long b = i < offered.Length ? long.Parse(offered[i]) : 0;
                if (a != b) return a > b;
            }
            return true;
        }
    }

    public sealed class ServerController
    {
        private const string StartupKey = @"Software\Microsoft\Windows\CurrentVersion\Run";
        private readonly string root;
        public readonly string LogDirectory;
        public readonly bool ServiceMode;

        public ServerController()
        {
            using (var key = Registry.LocalMachine.OpenSubKey(@"Software\Igloo"))
            {
                if (key == null) throw new InvalidOperationException("Igloo is not installed. Run Setup first.");
                root = (string)key.GetValue("InstallDirectory");
                LogDirectory = Path.Combine((string)key.GetValue("DataDirectory"), "logs", "server");
                ServiceMode = (int)key.GetValue("RunMode", 2) == 0;
            }
        }

        public bool StartAtLogin
        {
            get
            {
                using (var key = Registry.CurrentUser.OpenSubKey(StartupKey))
                    return key != null && key.GetValue("Igloo") != null;
            }
            set
            {
                using (var key = Registry.CurrentUser.CreateSubKey(StartupKey))
                {
                    if (value) key.SetValue("Igloo", "\"" + Path.Combine(root, "igloo-tray.exe") + "\" --background");
                    else key.DeleteValue("Igloo", false);
                }
            }
        }

        private Process FindServer()
        {
            var path = Path.Combine(root, "app", "current", "igloo-user.exe");
            foreach (var process in Process.GetProcessesByName("igloo-user"))
            {
                if (process.SessionId == Process.GetCurrentProcess().SessionId &&
                    string.Equals(process.MainModule.FileName, path, StringComparison.OrdinalIgnoreCase))
                    return process;
                process.Dispose();
            }
            return null;
        }

        public bool Running
        {
            get
            {
                if (ServiceMode)
                    using (var service = new ServiceController("Igloo"))
                        return service.Status == ServiceControllerStatus.Running;
                using (var process = FindServer()) return process != null;
            }
        }

        public void Start()
        {
            if (Running) return;
            if (ServiceMode)
            {
                using (var service = new ServiceController("Igloo"))
                {
                    service.Start();
                    service.WaitForStatus(ServiceControllerStatus.Running, TimeSpan.FromSeconds(60));
                }
            }
            else
            {
                using (var process = Process.Start(new ProcessStartInfo(Path.Combine(root, "app", "current", "igloo-user.exe"))
                    { UseShellExecute = false, CreateNoWindow = true, WorkingDirectory = root }))
                {
                    if (process.WaitForExit(1000)) throw new InvalidOperationException("Igloo could not start. See the server logs in " + LogDirectory);
                }
            }
        }

        public void Stop()
        {
            if (ServiceMode)
            {
                using (var service = new ServiceController("Igloo"))
                {
                    if (service.Status == ServiceControllerStatus.Stopped) return;
                    service.Stop();
                    service.WaitForStatus(ServiceControllerStatus.Stopped, TimeSpan.FromSeconds(60));
                }
            }
            else
            {
                using (var process = FindServer())
                {
                    if (process == null) return;
                    using (var stop = EventWaitHandle.OpenExisting(@"Local\Igloo.Server.Stop")) stop.Set();
                    if (!process.WaitForExit(60000)) throw new System.TimeoutException("Igloo has not finished shutting down. See the server logs.");
                }
            }
        }

        public void Open()
        {
            using (Process.Start(Path.Combine(root, "app", "current", "igloo-launch.exe"))) { }
        }

        public UpdateStatus Update(string command)
        {
            using (var pipe = new NamedPipeClientStream(".", "Igloo.Updates", PipeDirection.InOut))
            {
                pipe.Connect(5000);
                using (var writer = new StreamWriter(pipe, new UTF8Encoding(false), 1024, true))
                using (var reader = new StreamReader(pipe, Encoding.UTF8, false, 1024, true))
                {
                    writer.WriteLine(command);
                    writer.Flush();
                    var response = reader.ReadLine();
                    if (response == null) throw new IOException("The server closed the update connection.");
                    return new JavaScriptSerializer().Deserialize<UpdateStatus>(response);
                }
            }
        }
    }

    internal sealed class TrayContext : ApplicationContext
    {
        private readonly ServerController server = new ServerController();
        private readonly NotifyIcon tray;
        private readonly ToolStripMenuItem start = new ToolStripMenuItem("Start Igloo");
        private readonly ToolStripMenuItem stop = new ToolStripMenuItem("Stop Igloo");
        private readonly ToolStripMenuItem login = new ToolStripMenuItem("Start at login");
        private readonly ToolStripMenuItem update = new ToolStripMenuItem("Check for updates…");
        private bool busy;

        public TrayContext(bool background)
        {
            if (server.ServiceMode) login.Text = "Show tray at login";
            var menu = new ContextMenuStrip();
            menu.Items.Add("Open Igloo", null, (s, e) => Perform(server.Open));
            menu.Items.Add(start);
            menu.Items.Add(stop);
            menu.Items.Add(new ToolStripSeparator());
            menu.Items.Add(update);
            menu.Items.Add(login);
            menu.Items.Add("Open logs", null, (s, e) => Perform(() =>
            {
                using (Process.Start("explorer.exe", "\"" + server.LogDirectory + "\"")) { }
            }));
            menu.Items.Add(new ToolStripSeparator());
            menu.Items.Add(server.ServiceMode ? "Exit tray" : "Exit Igloo", null, (s, e) => Perform(() =>
            {
                if (!server.ServiceMode) server.Stop();
            }, true));
            start.Click += (s, e) => Perform(server.Start);
            stop.Click += (s, e) => Perform(server.Stop);
            login.Click += (s, e) => Perform(() => server.StartAtLogin = !server.StartAtLogin);
            update.Click += (s, e) => CheckForUpdates();
            menu.Opening += (s, e) =>
            {
                try
                {
                    start.Enabled = !busy && !server.Running;
                    stop.Enabled = !busy && server.Running;
                    login.Checked = server.StartAtLogin;
                    update.Enabled = !busy;
                }
                catch (Exception error) { ShowError(error); }
            };
            using (var icon = Assembly.GetExecutingAssembly().GetManifestResourceStream("Igloo.ico"))
                tray = new NotifyIcon { Icon = new Icon(icon), Text = "Igloo", ContextMenuStrip = menu, Visible = true };
            tray.DoubleClick += (s, e) => Perform(server.Open);
            // Run after the Windows Forms message loop starts.
            var timer = new System.Windows.Forms.Timer { Interval = 1 };
            timer.Tick += (s, e) =>
            {
                timer.Dispose();
                Perform(() => { server.Start(); if (!background) server.Open(); });
            };
            timer.Start();
        }

        private async void Perform(Action action, bool exit = false)
        {
            if (busy) return;
            busy = true;
            try { await Task.Run(action); if (exit) ExitThread(); }
            catch (Exception error) { ShowError(error); }
            finally { busy = false; }
        }

        private async void CheckForUpdates()
        {
            if (busy) return;
            busy = true;
            try
            {
                update.Text = "Checking for updates…";
                await Task.Run(server.Start);
                var status = await Task.Run(() => server.Update("check"));
                while (status.checking || status.applying)
                {
                    await Task.Delay(1000);
                    status = await Task.Run(() => server.Update("status"));
                }
                if (!string.IsNullOrEmpty(status.last_error)) throw new InvalidOperationException(status.last_error);
                if (!status.supported) throw new InvalidOperationException("Updates are unavailable in this server build.");
                if (string.IsNullOrEmpty(status.available_app) && string.IsNullOrEmpty(status.available_runtime))
                {
                    MessageBox.Show("Igloo is up to date.", "Igloo", MessageBoxButtons.OK, MessageBoxIcon.Information);
                    return;
                }
                string versions = string.IsNullOrEmpty(status.available_app) ? "" : "Igloo " + status.available_app + "\r\n";
                if (!string.IsNullOrEmpty(status.available_runtime)) versions += "Runtime " + status.available_runtime + "\r\n";
                if (MessageBox.Show(versions + "\r\nInstall these updates and restart Igloo?", "Igloo updates",
                    MessageBoxButtons.YesNo, MessageBoxIcon.Question) != DialogResult.Yes) return;

                var target = status;
                update.Text = "Installing updates…";
                status = await Task.Run(() => server.Update("apply"));
                DateTime lastResponse = DateTime.UtcNow;
                while (true)
                {
                    if (!string.IsNullOrEmpty(status.last_error)) throw new InvalidOperationException(status.last_error);
                    if (UpdateStatus.Reached(status.current_app, target.available_app) &&
                        UpdateStatus.Reached(status.current_runtime, target.available_runtime)) break;
                    if (status.applying) lastResponse = DateTime.UtcNow;
                    else if (DateTime.UtcNow - lastResponse > TimeSpan.FromSeconds(60))
                        throw new System.TimeoutException("Igloo did not restart with the update. See the update log in the installation's updates folder.");
                    await Task.Delay(1000);
                    try { status = await Task.Run(() => server.Update("status")); }
                    catch (Exception error)
                    {
                        if (!(error is IOException) && !(error is System.TimeoutException)) throw;
                        status = new UpdateStatus();
                    }
                }
                MessageBox.Show("Igloo was updated successfully.", "Igloo", MessageBoxButtons.OK, MessageBoxIcon.Information);
            }
            catch (Exception error) { ShowError(error); }
            finally { update.Text = "Check for updates…"; busy = false; }
        }

        private static void ShowError(Exception error)
        {
            MessageBox.Show(error.Message, "Igloo", MessageBoxButtons.OK, MessageBoxIcon.Error);
        }

        protected override void ExitThreadCore()
        {
            tray.Visible = false;
            tray.Icon.Dispose();
            tray.ContextMenuStrip.Dispose();
            tray.Dispose();
            base.ExitThreadCore();
        }
    }

    internal static class Program
    {
        [STAThread]
        private static void Main(string[] args)
        {
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            bool first;
            using (var instance = new Mutex(true, @"Local\Igloo.Tray", out first))
            {
                try
                {
                    if (!first) { if (!args.Contains("--background")) new ServerController().Open(); return; }
                    Application.Run(new TrayContext(args.Contains("--background")));
                }
                catch (Exception error)
                {
                    MessageBox.Show(error.Message, "Igloo", MessageBoxButtons.OK, MessageBoxIcon.Error);
                }
            }
        }
    }
}
