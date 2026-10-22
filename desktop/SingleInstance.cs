using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Runtime.InteropServices;
using System.Security.AccessControl;
using System.Security.Principal;
using System.Threading;
using System.Threading.Tasks;
using System.Text;

namespace Hardware.Music
{
    internal sealed class SingleInstance : IDisposable
    {
        private readonly Mutex mutex;
        private readonly string name;
        private NamedPipeServerStream pipe;
        private volatile bool stopped;
        internal readonly bool Primary;
        internal event Action Activate;

        [DllImport("user32.dll")] private static extern bool AllowSetForegroundWindow(int processId);

        internal SingleInstance(string appId)
        {
            string sid = WindowsIdentity.GetCurrent().User.Value;
            name = appId + "." + sid;
            bool created;
            mutex = new Mutex(true, "Local\\" + name, out created);
            Primary = created;
            if (Primary) Task.Run((Action)Listen);
        }

        private void Listen()
        {
            while (!stopped)
            {
                try
                {
                    PipeSecurity security = new PipeSecurity();
                    security.SetAccessRuleProtection(true, false);
                    security.AddAccessRule(new PipeAccessRule(WindowsIdentity.GetCurrent().User, PipeAccessRights.FullControl, AccessControlType.Allow));
                    using (NamedPipeServerStream server = new NamedPipeServerStream(name, PipeDirection.InOut, 1, PipeTransmissionMode.Message, PipeOptions.Asynchronous, 1024, 1024, security))
                    {
                        pipe = server;
                        server.WaitForConnection();
                        if (stopped) return;
                        // The outer pipe owns the connection. Closing the
                        // reader must not dispose the writer's stream while
                        // it flushes, or the listener dies after one launch.
                        using (StreamWriter writer = new StreamWriter(server, new UTF8Encoding(false), 1024, true))
                        using (StreamReader reader = new StreamReader(server, Encoding.UTF8, false, 1024, true))
                        {
                            writer.AutoFlush = true;
                            writer.WriteLine(Process.GetCurrentProcess().Id);
                            // Read a bounded enum, not paths, URLs or commands.
                            string message = "";
                            for (int i = 0; i < 16; i++)
                            {
                                int next = reader.Read();
                                if (next < 0 || next == '\n') break;
                                message += (char)next;
                            }
                            if (message.Trim() == "activate" && Activate != null) Activate();
                        }
                    }
                }
                catch (IOException) { if (!stopped) Thread.Sleep(100); }
                catch (ObjectDisposedException) { if (stopped) return; }
                catch (UnauthorizedAccessException) { return; }
            }
        }

        internal bool NotifyPrimary()
        {
            try
            {
                using (NamedPipeClientStream client = new NamedPipeClientStream(".", name, PipeDirection.InOut, PipeOptions.Asynchronous))
                {
                    client.Connect(2000);
                    using (StreamReader reader = new StreamReader(client, Encoding.UTF8, false, 1024, true))
                    using (StreamWriter writer = new StreamWriter(client, new UTF8Encoding(false), 1024, true))
                    {
                        int pid;
                        Task<string> greeting = reader.ReadLineAsync();
                        if (!greeting.Wait(1500)) return false;
                        if (Int32.TryParse(greeting.Result, out pid) && pid > 0) AllowSetForegroundWindow(pid);
                        writer.AutoFlush = true;
                        writer.WriteLine("activate");
                    }
                    return true;
                }
            }
            catch (TimeoutException) { return false; }
            catch (IOException) { return false; }
        }

        public void Dispose()
        {
            stopped = true;
            if (pipe != null) { try { pipe.Dispose(); } catch (IOException) { } }
            if (Primary) { try { mutex.ReleaseMutex(); } catch (ApplicationException) { } }
            mutex.Dispose();
        }
    }
}
