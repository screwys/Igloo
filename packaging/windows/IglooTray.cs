using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Reflection;
using System.ServiceProcess;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Forms;
using Microsoft.Win32;

namespace Igloo.Windows
{
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
    }

    internal sealed class TrayContext : ApplicationContext
    {
        private readonly ServerController server = new ServerController();
        private readonly NotifyIcon tray;
        private readonly ToolStripMenuItem start = new ToolStripMenuItem("Start Igloo");
        private readonly ToolStripMenuItem stop = new ToolStripMenuItem("Stop Igloo");
        private readonly ToolStripMenuItem login = new ToolStripMenuItem("Start at login");
        private bool busy;

        public TrayContext(bool background)
        {
            if (server.ServiceMode) login.Text = "Show tray at login";
            var menu = new ContextMenuStrip();
            menu.Items.Add("Open Igloo", null, (s, e) => Perform(server.Open));
            menu.Items.Add(start);
            menu.Items.Add(stop);
            menu.Items.Add(new ToolStripSeparator());
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
            menu.Opening += (s, e) =>
            {
                try
                {
                    start.Enabled = !busy && !server.Running;
                    stop.Enabled = !busy && server.Running;
                    login.Checked = server.StartAtLogin;
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
