using System;
using System.Drawing;
using System.IO;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace Hardware.Music
{
    internal sealed class DesktopOptions
    {
        internal string Root = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Music");
        internal string Origin = BuildSettings.Load().ServerOrigin;
        internal string LoginOrigin = BuildSettings.Load().LoginOrigin;
        internal string Entry { get { return entry ?? Origin + "/"; } set { entry = value; } }
        private string entry;
        internal string AppId = "com.hdwde.music";
        internal string FixtureDirectory = null;
        internal bool Test = false;
        internal bool ResetUi = false;
    }

    public sealed class BuildSettings
    {
        public string ServerOrigin = "http://127.0.0.1:18788";
        public string LoginOrigin = "";
        internal static BuildSettings Load()
        {
            BuildSettings settings = new JavaScriptSerializer().Deserialize<BuildSettings>(Resources.Text("Music.BuildSettings.json"));
            if (settings == null || !NavigationPolicy.ValidOrigin(settings.ServerOrigin)
                || (settings.LoginOrigin.Length > 0 && !NavigationPolicy.ValidOrigin(settings.LoginOrigin)))
                throw new InvalidDataException("Invalid build origins");
            return settings;
        }
    }

    public sealed class DesktopPreferences
    {
        public int Version = 1;
        public bool CloseToTray = true;
        public bool Topmost;
        public bool Dark;
        public double Zoom = 1;
        public bool LyricsEnabled;
        public bool LyricsSecondaryEnabled = true;
        public bool LyricsLocked;
        public int LyricsSize = 28;
        public int LyricsMinFont = 18;
        public int LyricsMaxFont = 42;
        public int LyricsBackdrop = 0;
        public string LyricsColor = "#ffffff";
        public string LyricsSecondaryColor = "#d8d8e2";
        public string LyricsOrientation = "horizontal";
        public bool LyricsAutoDock = false;
        public double WindowScale = 1;
        public int SavedWindowWidth;
        public int SavedWindowHeight;
        internal DesktopPreferences Copy() { return (DesktopPreferences)MemberwiseClone(); }
    }

    internal static class DesktopPreferenceStore
    {
        internal static bool ValidZoom(double value)
        {
            return value == .85 || value == 1 || value == 1.15 || value == 1.25;
        }
        internal static bool ValidColor(string value) { return value != null && System.Text.RegularExpressions.Regex.IsMatch(value, "^#[0-9a-fA-F]{6}$"); }

        internal static DesktopPreferences Load(string root)
        {
            try
            {
                string file = Path.Combine(root, "desktop.json");
                if (!File.Exists(file) || new FileInfo(file).Length > 8192) return new DesktopPreferences();
                DesktopPreferences value = new JavaScriptSerializer().Deserialize<DesktopPreferences>(File.ReadAllText(file));
                if (value == null || value.Version != 1 || !ValidZoom(value.Zoom)) return new DesktopPreferences();
                if (value.LyricsMinFont < 12 || value.LyricsMinFont > 72 || value.LyricsMaxFont < value.LyricsMinFont || value.LyricsMaxFont > 96) { value.LyricsMinFont = 18; value.LyricsMaxFont = 42; }
                value.LyricsSize = Math.Max(value.LyricsMinFont, Math.Min(value.LyricsMaxFont, value.LyricsSize));
                if (value.LyricsBackdrop < 0 || value.LyricsBackdrop > 80) value.LyricsBackdrop = 0;
                if (!ValidColor(value.LyricsColor)) value.LyricsColor = "#ffffff";
                if (!ValidColor(value.LyricsSecondaryColor)) value.LyricsSecondaryColor = "#d8d8e2";
                if (value.LyricsOrientation != "horizontal" && value.LyricsOrientation != "vertical") value.LyricsOrientation = "horizontal";
                value.LyricsAutoDock = false;
                if (!ValidWindowScale(value.WindowScale)) value.WindowScale = 1;
                if (value.SavedWindowWidth < 320 || value.SavedWindowWidth > 10000 || value.SavedWindowHeight < 240 || value.SavedWindowHeight > 10000) { value.SavedWindowWidth = 0; value.SavedWindowHeight = 0; }
                return value;
            }
            catch (Exception) { return new DesktopPreferences(); }
        }

        internal static bool Save(string root, DesktopPreferences value)
        {
            if (value == null || !ValidZoom(value.Zoom)) return false;
            try
            {
                string file = Path.Combine(root, "desktop.json");
                string temporary = file + ".tmp";
                File.WriteAllText(temporary, new JavaScriptSerializer().Serialize(value));
                if (File.Exists(file)) File.Replace(temporary, file, null);
                else File.Move(temporary, file);
                return true;
            }
            catch (IOException) { return false; }
            catch (UnauthorizedAccessException) { return false; }
        }
        internal static bool ValidWindowScale(double value) { return value >= .7 && value <= 1.25 && Math.Abs(value * 20 - Math.Round(value * 20)) < .001; }
    }

    internal static class WindowLayout
    {
        internal static Rectangle LaunchBounds(DesktopPreferences p)
        {
            Rectangle normal = DefaultBounds(), area = Screen.PrimaryScreen.WorkingArea;
            int width = p.SavedWindowWidth > 0 ? p.SavedWindowWidth : (int)Math.Round(normal.Width * p.WindowScale);
            int height = p.SavedWindowHeight > 0 ? p.SavedWindowHeight : (int)Math.Round(normal.Height * p.WindowScale);
            width = Math.Min(area.Width, Math.Max(800, width)); height = Math.Min(area.Height, Math.Max(600, height));
            return new Rectangle(area.X + (area.Width - width) / 2, area.Y + (area.Height - height) / 2, width, height);
        }
        internal static Rectangle DefaultBounds()
        {
            Rectangle area = Screen.PrimaryScreen.WorkingArea;
            int width = Math.Min(area.Width, Math.Min(1920, Math.Max(1120, (int)Math.Round(area.Width * .76))));
            int height = Math.Min(area.Height, Math.Min(1200, Math.Max(780, (int)Math.Round(area.Height * .84))));
            return new Rectangle(area.X + (area.Width - width) / 2, area.Y + (area.Height - height) / 2, width, height);
        }
    }

    internal static class NavigationPolicy
    {
        internal static bool ValidOrigin(string value)
        {
            Uri uri;
            return Uri.TryCreate(value, UriKind.Absolute, out uri) && uri.UserInfo.Length == 0
                && uri.AbsolutePath == "/" && uri.Query.Length == 0 && uri.Fragment.Length == 0
                && (uri.Scheme == Uri.UriSchemeHttps || (uri.Scheme == Uri.UriSchemeHttp && uri.IsLoopback));
        }
        internal static bool OriginIs(string value, string expected)
        {
            Uri uri, origin;
            return Uri.TryCreate(value, UriKind.Absolute, out uri) && Uri.TryCreate(expected, UriKind.Absolute, out origin)
                && (origin.Scheme == Uri.UriSchemeHttps || (origin.Scheme == Uri.UriSchemeHttp && origin.IsLoopback))
                && origin.UserInfo.Length == 0 && uri.Scheme == origin.Scheme && uri.Port == origin.Port && uri.UserInfo.Length == 0
                && String.Equals(uri.Host, origin.Host, StringComparison.OrdinalIgnoreCase);
        }
        internal static bool Embedded(string value, DesktopOptions options)
        {
            return OriginIs(value, options.Origin) || (!options.Test && !String.IsNullOrEmpty(options.LoginOrigin) && OriginIs(value, options.LoginOrigin));
        }
        internal static bool External(string value)
        {
            Uri uri;
            return Uri.TryCreate(value, UriKind.Absolute, out uri) && uri.Scheme == Uri.UriSchemeHttps
                && uri.IsDefaultPort && uri.UserInfo.Length == 0 && uri.Host.Length > 0;
        }
    }
}
