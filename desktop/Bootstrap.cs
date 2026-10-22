using System;
using System.IO;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Windows.Forms;

[assembly: AssemblyTitle("Husic")]
[assembly: AssemblyProduct("Husic")]
[assembly: AssemblyCompany("hardware")]
[assembly: AssemblyDescription("Husic · hardware")]
[assembly: AssemblyVersion("0.1.0.0")]
[assembly: AssemblyFileVersion("0.1.0.0")]
[assembly: ComVisible(false)]

namespace Hardware.Music
{
    internal static class Program
    {
        [STAThread]
        private static int Main(string[] args)
        {
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            AppDomain.CurrentDomain.AssemblyResolve += Resolve;
            try { return Run(args); }
            catch (Exception ex)
            {
                // Error types are useful; exception messages can contain URLs.
#if QA
                if (args.Length > 0) File.WriteAllText(args[0], "{\"ok\":false,\"errorType\":\"" + ex.GetType().Name + "\"}");
#else
                MessageBox.Show("Husic 暂时没有打开。请重新启动一次。\n\n错误类型：" + ex.GetType().Name,
                    "Husic", MessageBoxButtons.OK, MessageBoxIcon.Information);
#endif
                return 1;
            }
        }

        [MethodImpl(MethodImplOptions.NoInlining)]
        private static int Run(string[] args)
        {
            Microsoft.Web.WebView2.Core.CoreWebView2Environment.SetLoaderDllFolderPath(Resources.LoaderDirectory());
            try { Microsoft.Web.WebView2.Core.CoreWebView2Environment.GetAvailableBrowserVersionString(); }
            catch (Microsoft.Web.WebView2.Core.WebView2RuntimeNotFoundException)
            {
                MessageBox.Show("这台电脑还没有 WebView2。安装 Microsoft Edge WebView2 后就能打开 Husic。", "Husic");
                return 2;
            }
#if QA
            return DesktopTests.Run(args);
#else
            return DesktopApplication.Run(args);
#endif
        }

        private static Assembly Resolve(object sender, ResolveEventArgs args)
        {
            string name = new AssemblyName(args.Name).Name;
            if (name != "Microsoft.Web.WebView2.Core" && name != "Microsoft.Web.WebView2.WinForms") return null;
            return Assembly.Load(Resources.Bytes("Music.SDK." + name + ".dll"));
        }
    }

    internal static class Resources
    {
        internal const string SdkVersion = "1.0.4258.31";
        internal static byte[] Bytes(string name)
        {
            using (Stream stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(name))
            using (MemoryStream buffer = new MemoryStream())
            {
                if (stream == null) throw new InvalidDataException("Missing embedded resource");
                stream.CopyTo(buffer); return buffer.ToArray();
            }
        }
        internal static string Text(string name) { return System.Text.Encoding.UTF8.GetString(Bytes(name)); }
        internal static string LoaderDirectory()
        {
            string directory = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Music", "Runtime", SdkVersion, "x64");
            Directory.CreateDirectory(directory);
            string file = Path.Combine(directory, "WebView2Loader.dll");
            byte[] expected = Bytes("Music.SDK.WebView2Loader.dll");
            if (File.Exists(file) && Equal(File.ReadAllBytes(file), expected)) return directory;
            string temporary = file + "." + Guid.NewGuid().ToString("N") + ".tmp";
            File.WriteAllBytes(temporary, expected);
            try
            {
                if (File.Exists(file)) File.Replace(temporary, file, null);
                else File.Move(temporary, file);
            }
            catch (IOException)
            {
                if (!File.Exists(file) || !Equal(File.ReadAllBytes(file), expected)) throw;
            }
            finally { if (File.Exists(temporary)) File.Delete(temporary); }
            if (!Equal(File.ReadAllBytes(file), expected)) throw new InvalidDataException("Runtime checksum mismatch");
            return directory;
        }
        private static bool Equal(byte[] a, byte[] b)
        {
            using (SHA256 sha = SHA256.Create())
                return Convert.ToBase64String(sha.ComputeHash(a)) == Convert.ToBase64String(sha.ComputeHash(b));
        }
    }
}
