using System.IO;
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Runtime.InteropServices;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace Hardware.Music
{
    internal sealed partial class DesktopForm
    {
        internal DesktopPreferences Preferences { get; private set; }
        internal bool ShellReady { get; private set; }
        internal bool DarkChrome { get; private set; }
        internal bool ReducedMotion { get; private set; }
        private CaptionFallback fallbackCaption;
        private bool chromeInitialized;
        private Rectangle boundsBeforeMinimize;
        private FormWindowState stateBeforeMinimize;
        private bool minimizeSnapshot, restoreCorrectionQueued;
        private Rectangle boundsBeforeMaximize;
        private bool maximizeSnapshot, maximizeCorrectionQueued;
        private void CaptureMaximizeBounds()
        {
            if(WindowState==FormWindowState.Normal){boundsBeforeMaximize=Bounds;maximizeSnapshot=true;}
        }
        private void CorrectMaximizedRestore()
        {
            if(!maximizeSnapshot||maximizeCorrectionQueued||WindowState!=FormWindowState.Normal||!IsHandleCreated)return;
            maximizeCorrectionQueued=true;
            BeginInvoke((Action)delegate {
                maximizeCorrectionQueued=false;
                if(IsDisposed||!maximizeSnapshot||WindowState!=FormWindowState.Normal)return;
                maximizeSnapshot=false;Bounds=boundsBeforeMaximize;
            });
        }

        private void CaptureMinimizeBounds()
        {
            if(WindowState==FormWindowState.Minimized)return;
            boundsBeforeMinimize=Bounds;stateBeforeMinimize=WindowState;minimizeSnapshot=true;
        }
        private void CorrectRestoredBounds()
        {
            if(!minimizeSnapshot||restoreCorrectionQueued||WindowState==FormWindowState.Minimized||!IsHandleCreated)return;
            restoreCorrectionQueued=true;
            BeginInvoke((Action)delegate {
                restoreCorrectionQueued=false;
                if(IsDisposed||!minimizeSnapshot||WindowState==FormWindowState.Minimized)return;
                minimizeSnapshot=false;
                if(stateBeforeMinimize==FormWindowState.Maximized)WindowState=FormWindowState.Maximized;
                else {WindowState=FormWindowState.Normal;Bounds=boundsBeforeMinimize;}
            });
        }
#if QA
        internal string LastPreferenceChange;
#endif

        [StructLayout(LayoutKind.Sequential)]
        private struct FrameMargins { internal int Left, Right, Top, Bottom; }
        [StructLayout(LayoutKind.Sequential)]
        private struct NativeRect { internal int Left, Top, Right, Bottom; }
        [StructLayout(LayoutKind.Sequential)]
        private struct NcCalcSize { internal NativeRect First, Second, Third; internal IntPtr Position; }
        [DllImport("dwmapi.dll")] private static extern int DwmExtendFrameIntoClientArea(IntPtr window, ref FrameMargins margins);
        [DllImport("user32.dll")] private static extern bool IsZoomed(IntPtr window);
        [DllImport("user32.dll")] private static extern int GetSystemMetricsForDpi(int index, uint dpi);
        [DllImport("user32.dll")] private static extern uint GetDpiForWindow(IntPtr window);
        [DllImport("user32.dll")] private static extern bool ReleaseCapture();
        [DllImport("user32.dll")] private static extern bool GetCursorPos(out Point point);
        [DllImport("user32.dll")] private static extern IntPtr SendMessage(IntPtr window, int message, IntPtr wParam, IntPtr lParam);

        private void InitializeChrome()
        {
            Preferences = Options.ResetUi ? new DesktopPreferences() : DesktopPreferenceStore.Load(Options.Root);
            if (Options.ResetUi && !DesktopPreferenceStore.Save(Options.Root, Preferences)) throw new IOException("Could not reset preferences");
            DarkChrome = Preferences.Dark;
            TopMost = Preferences.Topmost;
            SetStyle(ControlStyles.OptimizedDoubleBuffer | ControlStyles.AllPaintingInWmPaint, true);
            fallbackCaption = new CaptionFallback(WindowCommand);
            Controls.Add(fallbackCaption);
            chromeInitialized = true;
            Resize += delegate { LayoutFallback(); PublishDesktopState(null); };
            LayoutFallback();
            ApplyChromeTheme(DarkChrome, false);
        }

        private void LayoutFallback()
        {
            if (fallbackCaption == null) return;
            float scale = IsHandleCreated ? WindowDpi() / 96f : 1;
            fallbackCaption.ScaleFactor = scale;
            fallbackCaption.Bounds = new Rectangle(0, 0, ClientSize.Width, (int)(42 * scale));
            fallbackCaption.Maximized = WindowState == FormWindowState.Maximized;
            fallbackCaption.Visible = !ShellReady || (messagePanel != null && messagePanel.Visible);
            if (fallbackCaption.Visible) fallbackCaption.BringToFront();
            fallbackCaption.Invalidate();
        }

        private uint WindowDpi()
        {
            try { return GetDpiForWindow(Handle); }
            catch (EntryPointNotFoundException) { using (Graphics graphics = CreateGraphics()) return (uint)graphics.DpiX; }
        }

        protected override void OnHandleCreated(EventArgs e)
        {
            base.OnHandleCreated(e);
            try
            {
                int rounded = 2;
                DwmSetWindowAttribute(Handle, 33, ref rounded, sizeof(int));
                FrameMargins margins = new FrameMargins { Left = 1, Right = 1, Top = 1, Bottom = 1 };
                DwmExtendFrameIntoClientArea(Handle, ref margins);
            }
            catch (DllNotFoundException) { }
            ApplyChromeTheme(DarkChrome, ReducedMotion);
        }

        // Keep WS_CAPTION/WS_THICKFRAME from FormBorderStyle.Sizable. Windows
        // still owns snap, resize, system shortcuts and minimize animations;
        // only its painted caption is replaced by the Music surface.
        private bool HandleChromeMessage(ref Message message)
        {
            if (message.Msg == 0x0083 && message.WParam != IntPtr.Zero)
            {
                if (IsZoomed(Handle))
                {
                    NcCalcSize size = (NcCalcSize)Marshal.PtrToStructure(message.LParam, typeof(NcCalcSize));
                    uint dpi = WindowDpi();
                    int x, y;
                    try { x = GetSystemMetricsForDpi(32, dpi) + GetSystemMetricsForDpi(92, dpi); y = GetSystemMetricsForDpi(33, dpi) + GetSystemMetricsForDpi(92, dpi); }
                    catch (EntryPointNotFoundException) { x = y = Math.Max(8, (int)(8 * dpi / 96)); }
                    size.First.Left += x; size.First.Right -= x;
                    size.First.Top += y; size.First.Bottom -= y;
                    Marshal.StructureToPtr(size, message.LParam, false);
                }
                message.Result = IntPtr.Zero;
                return true;
            }
            if (message.Msg == 0x0084)
            {
                long packed = message.LParam.ToInt64();
                Point point = PointToClient(new Point((short)(packed & 0xffff), (short)((packed >> 16) & 0xffff)));
                int hit = 1;
                if (WindowState == FormWindowState.Normal)
                {
                    int edge = Math.Max(5, (int)(6 * WindowDpi() / 96));
                    bool left = point.X < edge, right = point.X >= ClientSize.Width - edge;
                    bool top = point.Y < edge, bottom = point.Y >= ClientSize.Height - edge;
                    hit = top && left ? 13 : top && right ? 14 : bottom && left ? 16 : bottom && right ? 17 : left ? 10 : right ? 11 : top ? 12 : bottom ? 15 : 1;
                }
                message.Result = (IntPtr)hit;
                return true;
            }
            if (message.Msg == 0x02E0 && chromeInitialized)
                BeginInvoke((Action)delegate { LayoutFallback(); });
            return false;
        }

        private void ApplyChromeTheme(bool dark, bool reduced)
        {
            DarkChrome = dark; ReducedMotion = reduced;
            if (Preferences != null && Preferences.Dark != dark)
            {
                Preferences.Dark = dark;
                DesktopPreferenceStore.Save(Options.Root, Preferences);
            }
            BackColor = dark ? Color.FromArgb(27, 27, 27) : Color.White;
            if (Browser != null) Browser.DefaultBackgroundColor = BackColor;
            if (messagePanel != null)
            {
                messagePanel.BackColor = BackColor;
                messageTitle.ForeColor = dark ? Color.FromArgb(245, 245, 247) : Color.FromArgb(29, 29, 31);
                messageDetail.ForeColor = dark ? Color.FromArgb(161, 161, 166) : Color.FromArgb(110, 110, 115);
                retryButton.BackColor = dark ? Color.FromArgb(49, 49, 49) : Color.FromArgb(242, 242, 242);
                retryButton.ForeColor = messageTitle.ForeColor;
            }
            if (fallbackCaption != null) { fallbackCaption.Dark = dark; fallbackCaption.BackColor = BackColor; fallbackCaption.Invalidate(); }
            if (!IsHandleCreated) return;
            try
            {
                int value = dark ? 1 : 0;
                DwmSetWindowAttribute(Handle, 20, ref value, sizeof(int));
                Color edge = dark ? Color.FromArgb(60, 60, 60) : Color.FromArgb(218, 218, 218);
                value = edge.R | (edge.G << 8) | (edge.B << 16);
                DwmSetWindowAttribute(Handle, 34, ref value, sizeof(int));
            }
            catch (DllNotFoundException) { }
        }

        internal void WindowCommand(string action)
        {
            int hit = action == "drag" ? 2 : action == "resize-left" ? 10 : action == "resize-right" ? 11
                : action == "resize-top" ? 12 : action == "resize-top-left" ? 13 : action == "resize-top-right" ? 14
                : action == "resize-bottom" ? 15 : action == "resize-bottom-left" ? 16 : action == "resize-bottom-right" ? 17 : 0;
            if (hit != 0)
            {
                if (hit != 2 && WindowState != FormWindowState.Normal) return;
                Point cursor;
                if (!GetCursorPos(out cursor)) return;
                ReleaseCapture();
                IntPtr coordinates = (IntPtr)((cursor.X & 0xffff) | ((cursor.Y & 0xffff) << 16));
                // WebView2's child HWND consumes the normal frame hit-test.
                // Start the OS move/size loop on the real top-level HWND.
                SendMessage(Handle, 0x00A1, (IntPtr)hit, coordinates);
                return;
            }
            switch (action)
            {
                case "minimize": CaptureMinimizeBounds(); WindowState = FormWindowState.Minimized; break;
                case "maximize":
                    if(WindowState==FormWindowState.Maximized){WindowState=FormWindowState.Normal;CorrectMaximizedRestore();}
                    else {CaptureMaximizeBounds();WindowState=FormWindowState.Maximized;}
                    break;
                case "close": Close(); break;
            }
        }

        internal bool ChangeDesktopPreference(string name, object value)
        {
#if QA
            LastPreferenceChange = name + ":" + (value == null ? "null" : value.GetType().Name + ":" + Convert.ToString(value));
#endif
            DesktopPreferences next = Preferences.Copy();
            if (name == "closeToTray" && value is bool) next.CloseToTray = (bool)value;
            else if (name == "topmost" && value is bool) next.Topmost = (bool)value;
            else if (name == "lyricsEnabled" && value is bool) next.LyricsEnabled = (bool)value;
            else if (name == "lyricsSecondaryEnabled" && value is bool) next.LyricsSecondaryEnabled = (bool)value;
            else if (name == "lyricsLocked" && value is bool) next.LyricsLocked = (bool)value;
            else if (name == "lyricsSize" && value is int && (int)value >= next.LyricsMinFont && (int)value <= next.LyricsMaxFont) next.LyricsSize = (int)value;
            else if (name == "lyricsMinFont" && value is int && (int)value >= 12 && (int)value <= next.LyricsMaxFont && (int)value <= 72) next.LyricsMinFont = (int)value;
            else if (name == "lyricsMaxFont" && value is int && (int)value >= next.LyricsMinFont && (int)value <= 96) next.LyricsMaxFont = (int)value;
            else if (name == "lyricsBackdrop" && value is int && (int)value >= 0 && (int)value <= 80) next.LyricsBackdrop = (int)value;
            else if (name == "lyricsColor" && value is string && DesktopPreferenceStore.ValidColor((string)value)) next.LyricsColor = (string)value;
            else if (name == "lyricsSecondaryColor" && value is string && DesktopPreferenceStore.ValidColor((string)value)) next.LyricsSecondaryColor = (string)value;
            else if (name == "lyricsOrientation" && (value as string == "horizontal" || value as string == "vertical")) next.LyricsOrientation = (string)value;
            else if (name == "windowScale" && (value is int || value is decimal || value is double) && DesktopPreferenceStore.ValidWindowScale(Convert.ToDouble(value))) {
                next.WindowScale = Convert.ToDouble(value); next.SavedWindowWidth = 0; next.SavedWindowHeight = 0;
            }
            else if (name == "saveWindowSize" && value is bool && (bool)value) {
                Size size = WindowState == FormWindowState.Maximized ? Screen.FromControl(this).WorkingArea.Size : Bounds.Size;
                next.SavedWindowWidth = size.Width; next.SavedWindowHeight = size.Height;
            }
            else if (name == "zoom" && (value is double || value is decimal || value is int))
            {
                double zoom = Convert.ToDouble(value);
                if (!DesktopPreferenceStore.ValidZoom(zoom)) return false;
                next.Zoom = zoom;
            }
            else { PublishDesktopState("设置未改变，请检查字号范围或颜色。"); return false; }
            next.LyricsSize = Math.Max(next.LyricsMinFont, Math.Min(next.LyricsMaxFont, next.LyricsSize));
            if (!DesktopPreferenceStore.Save(Options.Root, next)) { PublishDesktopState("设置没有保存，请稍后重试。"); return false; }
            Preferences = next;
            if (TopMost != next.Topmost) TopMost = next.Topmost;
            if (Browser.CoreWebView2 != null && Math.Abs(Browser.ZoomFactor - next.Zoom) > .001) Browser.ZoomFactor = next.Zoom;
            if (name == "windowScale") { maximizeSnapshot=false;minimizeSnapshot=false;WindowState = FormWindowState.Normal; Bounds = WindowLayout.LaunchBounds(next); }
            ApplyLyricsPreferences();
            RefreshTray();
            PublishDesktopState(null);
            return true;
        }

        private void PublishDesktopState(string error)
        {
            if (!chromeInitialized || Preferences == null || Browser == null || Browser.CoreWebView2 == null || Browser.Source == null || !NavigationPolicy.Embedded(Browser.Source.AbsoluteUri, Options)) return;
            try
            {
                Browser.CoreWebView2.PostWebMessageAsJson(new JavaScriptSerializer().Serialize(new {
                    type = "music-desktop-state", version = "0.1.0", maximized = WindowState == FormWindowState.Maximized,
                    closeToTray = Preferences.CloseToTray, topmost = Preferences.Topmost, zoom = Preferences.Zoom, error = error
                    , lyricsEnabled = Preferences.LyricsEnabled, lyricsSecondaryEnabled = Preferences.LyricsSecondaryEnabled, lyricsLocked = Preferences.LyricsLocked, lyricsSize = Preferences.LyricsSize,
                    lyricsMinFont = Preferences.LyricsMinFont, lyricsMaxFont = Preferences.LyricsMaxFont, lyricsBackdrop = Preferences.LyricsBackdrop,
                    lyricsColor = Preferences.LyricsColor, lyricsSecondaryColor = Preferences.LyricsSecondaryColor, lyricsOrientation = Preferences.LyricsOrientation, lyricsAutoDock = Preferences.LyricsAutoDock,
                    windowScale = Preferences.WindowScale, customWindowSize = Preferences.SavedWindowWidth > 0, windowWidth = Width, windowHeight = Height,
                    notice = error == null && Preferences.SavedWindowWidth > 0 ? "已保存" : null
                }));
            }
            catch (InvalidOperationException) { }
        }

        private bool ReceiveChromeMessage(Dictionary<string, object> data, bool musicOrigin)
        {
            object type, value;
            if (!data.TryGetValue("type", out type) || !(type is string)) return false;
            if ((string)type == "desktop-shell-ready")
            {
                ShellReady = true; LayoutFallback(); PublishDesktopState(null); return true;
            }
            if ((string)type == "desktop-window-command")
            {
                if (data.TryGetValue("action", out value) && value is string)
                {
                    string action = (string)value;
                    if (action == "minimize" || action == "maximize" || action == "close" || action == "drag"
                        || action == "resize-left" || action == "resize-right" || action == "resize-top" || action == "resize-bottom"
                        || action == "resize-top-left" || action == "resize-top-right" || action == "resize-bottom-left" || action == "resize-bottom-right")
                    {
                        // Do not enter the Windows modal move/size loop from
                        // inside a WebView2 callback (unsupported reentrancy).
                        if (action == "drag" || action.StartsWith("resize-", StringComparison.Ordinal))
                            BeginInvoke((Action)delegate { if (!IsDisposed) WindowCommand(action); });
                        else WindowCommand(action);
                    }
                }
                return true;
            }
            if ((string)type == "desktop-settings-change" && musicOrigin)
            {
                object name;
                if (data.TryGetValue("name", out name) && name is string && data.TryGetValue("value", out value)) ChangeDesktopPreference((string)name, value);
                return true;
            }
            return false;
        }

        private sealed class CaptionFallback : Control
        {
            internal bool Dark, Maximized;
            internal float ScaleFactor = 1;
            private readonly Action<string> command;
            private readonly Button[] buttons = new Button[3];
            internal CaptionFallback(Action<string> command)
            {
                this.command = command;
                SetStyle(ControlStyles.OptimizedDoubleBuffer | ControlStyles.AllPaintingInWmPaint | ControlStyles.UserPaint, true);
                string[] names = { "最小化", "最大化", "关闭窗口" };
                string[] actions = { "minimize", "maximize", "close" };
                for (int i = 0; i < 3; i++)
                {
                    int index = i;
                    Button button = new Button { AccessibleName = names[i], FlatStyle = FlatStyle.Flat, TabStop = true, Text = "", BackColor = Color.Transparent };
                    button.FlatAppearance.BorderSize = 0;
                    button.FlatAppearance.MouseOverBackColor = Color.FromArgb(225, 225, 225);
                    button.FlatAppearance.MouseDownBackColor = Color.FromArgb(210, 210, 210);
                    button.Click += delegate { command(actions[index]); };
                    button.Paint += delegate(object sender, PaintEventArgs e) { PaintGlyph(e.Graphics, button.ClientRectangle, index); };
                    buttons[i] = button; Controls.Add(button);
                }
                MouseDown += delegate(object sender, MouseEventArgs e) { if (e.Button == MouseButtons.Left) command("drag"); };
                DoubleClick += delegate { command("maximize"); };
            }
            protected override void OnResize(EventArgs e)
            {
                base.OnResize(e);
                if (buttons[0] == null) return;
                int width = (int)(42 * ScaleFactor), height = (int)(32 * ScaleFactor), inset = (int)(6 * ScaleFactor);
                for (int i = 0; i < 3; i++) buttons[i].Bounds = new Rectangle(Width - inset - (3 - i) * width, inset, width, height);
            }
            private void PaintGlyph(Graphics graphics, Rectangle rectangle, int index)
            {
                graphics.SmoothingMode = SmoothingMode.AntiAlias;
                float s = ScaleFactor, x = rectangle.Width / 2f, y = rectangle.Height / 2f;
                using (Pen pen = new Pen(Dark ? Color.Gainsboro : Color.FromArgb(80, 80, 80), Math.Max(1, s)))
                {
                    if (index == 0) graphics.DrawLine(pen, x - 5 * s, y + 3 * s, x + 5 * s, y + 3 * s);
                    else if (index == 1) graphics.DrawRectangle(pen, x - 4.5f * s, y - 4.5f * s, 9 * s, 9 * s);
                    else { graphics.DrawLine(pen, x - 4.5f * s, y - 4.5f * s, x + 4.5f * s, y + 4.5f * s); graphics.DrawLine(pen, x + 4.5f * s, y - 4.5f * s, x - 4.5f * s, y + 4.5f * s); }
                }
            }
        }
    }
}
