using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading.Tasks;
using System.Web.Script.Serialization;
using System.Windows.Forms;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace Hardware.Music
{
    internal static class DesktopApplication
    {
        [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
        private static extern int SetCurrentProcessExplicitAppUserModelID(string id);

        internal static int Run(string[] args)
        {
            DesktopOptions options = new DesktopOptions();
            options.ResetUi = Array.IndexOf(args, "--reset-ui") >= 0;
            SetCurrentProcessExplicitAppUserModelID(options.AppId);
            using (SingleInstance instance = new SingleInstance(options.AppId))
            {
                if (!instance.Primary)
                {
                    if (!instance.NotifyPrimary()) MessageBox.Show("Husic 正在打开，请稍等片刻再试。", "Husic");
                    return 0;
                }
                using (DesktopForm form = new DesktopForm(options))
                {
                    instance.Activate += delegate { if (form.IsHandleCreated && !form.IsDisposed) form.BeginInvoke((Action)form.RestoreWindow); };
                    Application.Run(form);
                }
            }
            return 0;
        }
    }

    internal sealed partial class DesktopForm : Form
    {
        internal readonly DesktopOptions Options;
        internal readonly WebView2 Browser;
        internal readonly TaskCompletionSource<bool> Ready = new TaskCompletionSource<bool>();
        internal bool Playing { get; private set; }
        internal bool MediaReady { get; private set; }
        internal string SongTitle { get; private set; }
        internal string SongArtist { get; private set; }
        internal MusicTrayMenu TrayMenu { get; private set; }
        internal DesktopLyrics LyricsWindow { get; private set; }
        internal bool TrayVisible { get { return tray.Visible; } }
        internal bool ErrorVisible { get { return messagePanel.Visible && retryButton.Visible; } }
        private readonly NotifyIcon tray;
        private readonly Panel messagePanel;
        private readonly Label messageTitle;
        private readonly Label messageDetail;
        private readonly Button retryButton;
        private readonly PictureBox loadingMark;
        private readonly Timer loadingPulse;
        private int loadingPhase;
        private bool initialized;
        private bool quitting;
        private bool allowClose;
        private bool closeHintShown;
        private bool audioSaveDialogOpen;
        private DateTime trayMenuClosedAt = DateTime.MinValue;

        [DllImport("user32.dll")] private static extern bool SetForegroundWindow(IntPtr handle);
        [DllImport("dwmapi.dll")] private static extern int DwmSetWindowAttribute(IntPtr handle, int attribute, ref int value, int size);

        internal DesktopForm(DesktopOptions options)
        {
            Options = options;
            Directory.CreateDirectory(options.Root);
            Text = "Husic";
            Icon = new Icon(new MemoryStream(Resources.Bytes("Music.Brand.ico")));
            AutoScaleMode = AutoScaleMode.Dpi;
            MinimumSize = new Size(800, 600);
            StartPosition = FormStartPosition.Manual;
            Bounds = WindowLayout.DefaultBounds();
            BackColor = Color.FromArgb(246, 246, 246);
            // Local files are delivered to the import page as browser File
            // objects. NavigationStarting still rejects every file:// URL.
            Browser = new WebView2 { Dock = DockStyle.Fill, DefaultBackgroundColor = BackColor, AllowExternalDrop = true };
            Controls.Add(Browser);

            TrayMenu = new MusicTrayMenu(TrayCommand);
            tray = new NotifyIcon { Icon = Icon, Text = "Husic", ContextMenuStrip = TrayMenu, Visible = true };
            TrayMenu.Closed += delegate { trayMenuClosedAt = DateTime.UtcNow; };
            tray.MouseClick += delegate(object sender, MouseEventArgs e) {
                // Ignore the menu-dismiss click relayed by NotifyIcon. Unlock
                // is a lyric action, not an instruction to restore the player.
                if (e.Button == MouseButtons.Left && !TrayMenu.Visible && (DateTime.UtcNow - trayMenuClosedAt).TotalMilliseconds > 350) RestoreWindow();
            };

            messagePanel = new Panel { Dock = DockStyle.Fill, BackColor = BackColor };
            loadingMark = new PictureBox { Image = Bitmap.FromStream(new MemoryStream(Resources.Bytes("Music.Brand.png"))), SizeMode = PictureBoxSizeMode.Zoom };
            messageTitle = new Label { Text="Husic",AutoSize=false,TextAlign=ContentAlignment.MiddleCenter,Font=new Font("Microsoft YaHei UI",22,FontStyle.Bold),ForeColor=Color.FromArgb(24,24,24) };
            messageDetail = new Label { Visible=false,AutoSize=false };
            retryButton = new Button { Text="重试",AccessibleName="重新连接 Husic",FlatStyle=FlatStyle.Flat,BackColor=Color.White,Visible=false };
            retryButton.FlatAppearance.BorderSize=0;
            retryButton.Click+=async delegate {if(Browser.CoreWebView2==null)await InitializeBrowser();else NavigateHome();};
            messagePanel.Controls.AddRange(new Control[]{loadingMark,messageTitle,messageDetail,retryButton});
            loadingPulse=new Timer {Interval=80};
            loadingPulse.Tick+=delegate {loadingPhase=(loadingPhase+1)%36;if(messagePanel.Visible&&!retryButton.Visible)messagePanel.Invalidate();};
            messagePanel.Resize+=delegate {LayoutStartup();};
            messagePanel.VisibleChanged+=delegate {loadingPulse.Enabled=messagePanel.Visible&&!retryButton.Visible;LayoutStartup();};
            messagePanel.Paint+=delegate(object sender,PaintEventArgs e) {
                if(retryButton.Visible)return;float scale=IsHandleCreated?WindowDpi()/96f:1;
                e.Graphics.SmoothingMode=System.Drawing.Drawing2D.SmoothingMode.AntiAlias;
                for(int i=0;i<3;i++) {
                    double wave=ReducedMotion?.5:(Math.Sin((loadingPhase-i*5)*Math.PI/18)+1)/2;
                    int alpha=80+(int)(wave*150);Color ink=DarkChrome?Color.White:Color.FromArgb(30,30,34);
                    using(Brush brush=new SolidBrush(Color.FromArgb(alpha,ink)))e.Graphics.FillEllipse(brush,messagePanel.Width/2f+(i-1)*14*scale-2.5f*scale,messageTitle.Bottom+18*scale,5*scale,5*scale);
                }
            };
            Controls.Add(messagePanel);
            messagePanel.BringToFront();
            InitializeChrome();
            LayoutStartup();
            LyricsWindow = new DesktopLyrics(TrayCommand);
            ApplyLyricsPreferences();
            RefreshTray();
            Shown += async delegate { if (!initialized) { initialized = true; await InitializeBrowser(); } };
        }

        private void LayoutStartup()
        {
            if(loadingMark==null||messageTitle==null||retryButton==null)return;
            float scale=IsHandleCreated?WindowDpi()/96f:1;
            int logo=(int)(84*scale),gap=(int)(18*scale),title=(int)(52*scale),tail=(int)((retryButton.Visible?60:36)*scale);
            int y=Math.Max((int)(64*scale),(messagePanel.Height-logo-gap-title-tail)/2);
            loadingMark.Bounds=new Rectangle((messagePanel.Width-logo)/2,y,logo,logo);
            messageTitle.Bounds=new Rectangle((int)(24*scale),y+logo+gap,messagePanel.Width-(int)(48*scale),title);
            retryButton.Bounds=new Rectangle((messagePanel.Width-(int)(144*scale))/2,messageTitle.Bottom+(int)(14*scale),(int)(144*scale),(int)(40*scale));
            messagePanel.Invalidate();
        }
#if QA
        internal void PreviewStartup() {ShowMessage("Husic","",false);}
        internal bool StartupCentered { get {return Math.Abs(loadingMark.Left+loadingMark.Width/2-messagePanel.Width/2)<=1 && Math.Abs(messageTitle.Left+messageTitle.Width/2-messagePanel.Width/2)<=1 && Math.Abs(retryButton.Left+retryButton.Width/2-messagePanel.Width/2)<=1 && messageTitle.TextAlign==ContentAlignment.MiddleCenter;}}
#endif

        protected override void OnLoad(EventArgs e)
        {
            base.OnLoad(e);
            // Apply after the DPI pass. Use the chosen default size, centered;
            // ordinary dragging never silently overwrites that preference.
            WindowState = FormWindowState.Normal;
            Bounds = WindowLayout.LaunchBounds(Preferences);
        }

        private async Task InitializeBrowser()
        {
            try
            {
                CoreWebView2EnvironmentOptions environmentOptions = new CoreWebView2EnvironmentOptions();
                // Keep recommendation/next-track timers alive while the native
                // window is hidden. Chromium's renderer sandbox stays enabled.
                environmentOptions.AdditionalBrowserArguments = "--disable-background-timer-throttling --disable-renderer-backgrounding --disable-backgrounding-occluded-windows --autoplay-policy=no-user-gesture-required";
                if (Options.Test) environmentOptions.AdditionalBrowserArguments += " --disable-features=HardwareMediaKeyHandling";
                CoreWebView2Environment environment = await CoreWebView2Environment.CreateAsync(null, Path.Combine(Options.Root, "WebView"), environmentOptions);
                await Browser.EnsureCoreWebView2Async(environment);
                CoreWebView2 core = Browser.CoreWebView2;
                if (Options.ResetUi)
                {
                    // Preserve only the site's gate cookies, not queue, covers,
                    // wallpaper, caches or previously saved UI preferences.
                    await core.Profile.ClearBrowsingDataAsync(CoreWebView2BrowsingDataKinds.AllDomStorage
                        | CoreWebView2BrowsingDataKinds.DiskCache | CoreWebView2BrowsingDataKinds.BrowsingHistory
                        | CoreWebView2BrowsingDataKinds.DownloadHistory);
                    Options.ResetUi = false;
                }
                core.Settings.AreHostObjectsAllowed = false;
                core.Settings.AreDevToolsEnabled = Options.Test;
                core.Settings.IsStatusBarEnabled = false;
                core.Settings.AreDefaultContextMenusEnabled = false;
                // Pointer regions send fixed move/resize actions to the root
                // window; CSS app-region alone does not reach it in WinForms.
                core.Settings.IsNonClientRegionSupportEnabled = false;
                core.Settings.IsZoomControlEnabled = false;
                Browser.ZoomFactor = Preferences.Zoom;
                core.PermissionRequested += delegate(object sender, CoreWebView2PermissionRequestedEventArgs e) { e.State = CoreWebView2PermissionState.Deny; };
                core.NavigationStarting += Starting;
                core.NavigationCompleted += Completed;
                core.WebMessageReceived += MessageReceived;
                core.NewWindowRequested += delegate(object sender, CoreWebView2NewWindowRequestedEventArgs e)
                {
                    e.Handled = true;
                    if (!e.IsUserInitiated) return;
                    if (NavigationPolicy.Embedded(e.Uri, Options)) core.Navigate(e.Uri);
                    else OpenExternal(e.Uri);
                };
                core.DownloadStarting += DownloadStarting;
                core.ProcessFailed += delegate { ShowMessage("播放器需要重新连接", "已保存的音乐库和账号不会删除，重新连接就能继续。", true); };
                if (Options.Test && Options.FixtureDirectory != null)
                    core.SetVirtualHostNameToFolderMapping(new Uri(Options.Origin).Host, Options.FixtureDirectory, CoreWebView2HostResourceAccessKind.DenyCors);
                string bridge = Resources.Text("Music.DesktopBridge.js").Replace("__MUSIC_ORIGIN__", new JavaScriptSerializer().Serialize(Options.Origin));
                await core.AddScriptToExecuteOnDocumentCreatedAsync(bridge);
                string chrome = Resources.Text("Music.DesktopChrome.js")
                    .Replace("__MUSIC_ORIGIN__", new JavaScriptSerializer().Serialize(Options.Origin))
                    .Replace("__MUSIC_LOGIN_ORIGIN__", new JavaScriptSerializer().Serialize(Options.LoginOrigin))
                    .Replace("__MUSIC_DESKTOP_CSS__", new JavaScriptSerializer().Serialize(Resources.Text("Music.DesktopChrome.css")));
                await core.AddScriptToExecuteOnDocumentCreatedAsync(chrome);
                Ready.TrySetResult(true);
                NavigateHome();
            }
            catch (Exception ex)
            {
                Ready.TrySetException(new InvalidOperationException(ex.GetType().Name));
                ShowMessage("Husic 暂时没有连上", "可以稍后重试；如果仍然无法打开，请在浏览器中使用 Husic。", true);
            }
        }

        internal void NavigateHome()
        {
            if (Browser.CoreWebView2 == null) return;
            ShowMessage("Husic", "正在接上你的音乐…", false);
            Browser.CoreWebView2.Navigate(Options.Entry);
        }

#if QA
        internal Func<SaveFileDialog, IWin32Window, DialogResult> AudioSaveDialogOverride;
        internal CoreWebView2DownloadOperation LastAudioDownload;
        internal string LastAudioDownloadDecision;
        private string audioDownloadStage = "not-started";
        private string audioDownloadMime;
        private string audioDownloadName;
        private string audioDownloadException;
        private string audioDownloadNavigation;
        private bool audioDownloadSameBlobOrigin;

        internal object AudioDownloadSnapshot()
        {
            return new {
                stage = audioDownloadStage, decision = LastAudioDownloadDecision,
                sawDownloadEvent = LastAudioDownload != null, mime = audioDownloadMime,
                safeName = audioDownloadName, sameBlobOrigin = audioDownloadSameBlobOrigin,
                pageIsMusic = Browser.Source != null && NavigationPolicy.OriginIs(Browser.Source.AbsoluteUri, Options.Origin),
                state = LastAudioDownload == null ? "none" : LastAudioDownload.State.ToString(),
                reason = LastAudioDownload == null ? "none" : LastAudioDownload.InterruptReason.ToString(),
                lastNavigation = audioDownloadNavigation, failureType = audioDownloadException
            };
        }
#endif

        private async void DownloadStarting(object sender, CoreWebView2DownloadStartingEventArgs e)
        {
            // A webpage cannot choose a destination or start an arbitrary
            // network/executable download. Only its own audio Blob is eligible
            // for the native, owner-bound Save As dialog.
            e.Cancel = true;
            e.Handled = true;
#if QA
            LastAudioDownload = e.DownloadOperation;
            LastAudioDownloadDecision = "blocked";
            audioDownloadStage = "download-event";
            audioDownloadMime = e.DownloadOperation.MimeType;
            audioDownloadName = AudioDownloadPolicy.SafeFileName(e.ResultFilePath);
            audioDownloadException = null;
            audioDownloadSameBlobOrigin = e.DownloadOperation.Uri != null && e.DownloadOperation.Uri.StartsWith("blob:", StringComparison.Ordinal)
                && NavigationPolicy.OriginIs(e.DownloadOperation.Uri.Substring(5), Options.Origin);
#endif
            string filename;
            if (!AudioDownloadPolicy.TryPrepare(Browser.Source == null ? null : Browser.Source.AbsoluteUri,
                e.DownloadOperation.Uri, e.ResultFilePath, e.DownloadOperation.MimeType, Options, out filename))
            {
#if QA
                audioDownloadStage = "policy-blocked";
#endif
                ShowMessage("在浏览器中完成下载", "Husic 的播放不受影响。连接扩展请在 Chrome 的 Husic 设置里下载。", true);
                return;
            }
            if (audioSaveDialogOpen) return;
            CoreWebView2Deferral deferral = e.GetDeferral();
            audioSaveDialogOpen = true;
            try
            {
#if QA
                audioDownloadStage = "deferral-await-ui";
#endif
                // Leave the WebView2 event callback before showing modal UI.
                await Task.Yield();
                if (IsDisposed || Disposing || Browser.CoreWebView2 == null || Browser.Source == null
                    || !NavigationPolicy.OriginIs(Browser.Source.AbsoluteUri, Options.Origin)) return;
                string extension = Path.GetExtension(filename);
                using (SaveFileDialog dialog = new SaveFileDialog())
                {
#if QA
                    audioDownloadStage = "save-dialog";
#endif
                    dialog.Title = "导出音频";
                    dialog.FileName = filename;
                    dialog.Filter = extension.Substring(1).ToUpperInvariant() + " 音频 (*" + extension + ")|*" + extension;
                    dialog.DefaultExt = extension.Substring(1);
                    dialog.AddExtension = true;
                    dialog.OverwritePrompt = true;
                    dialog.CheckPathExists = true;
                    dialog.ValidateNames = true;
                    dialog.RestoreDirectory = true;
                    dialog.FileOk += delegate(object dialogSender, System.ComponentModel.CancelEventArgs args) {
                        if (AudioDownloadPolicy.ValidSavePath(dialog.FileName, extension)) return;
                        args.Cancel = true;
                        MessageBox.Show(this, "请保留原音频的 " + extension + " 扩展名。", "Husic", MessageBoxButtons.OK, MessageBoxIcon.Information);
                    };
                    DialogResult choice;
#if QA
                    choice = Options.Test && AudioSaveDialogOverride != null ? AudioSaveDialogOverride(dialog, this) : dialog.ShowDialog(this);
                    LastAudioDownloadDecision = "canceled";
                    audioDownloadStage = "save-dialog-returned-" + choice.ToString();
#else
                    choice = dialog.ShowDialog(this);
#endif
                    if (choice != DialogResult.OK || !AudioDownloadPolicy.ValidSavePath(dialog.FileName, extension)
                        || Browser.Source == null || !NavigationPolicy.OriginIs(Browser.Source.AbsoluteUri, Options.Origin)) return;
                    e.ResultFilePath = Path.GetFullPath(dialog.FileName);
                    e.Cancel = false;
#if QA
                    LastAudioDownloadDecision = "saved";
#endif
                }
            }
            catch (ArgumentException ex) { CancelAudioDownload(e, ex); }
            catch (InvalidOperationException ex) { CancelAudioDownload(e, ex); }
            catch (IOException ex) { CancelAudioDownload(e, ex); }
            catch (UnauthorizedAccessException ex) { CancelAudioDownload(e, ex); }
            catch (System.ComponentModel.Win32Exception ex) { CancelAudioDownload(e, ex); }
            catch (COMException ex) { CancelAudioDownload(e, ex); }
            finally { audioSaveDialogOpen = false; deferral.Complete(); }
        }

        private void CancelAudioDownload(CoreWebView2DownloadStartingEventArgs e, Exception exception)
        {
            e.Cancel = true;
#if QA
            audioDownloadException = exception.GetType().Name;
#endif
        }

        private void Starting(object sender, CoreWebView2NavigationStartingEventArgs e)
        {
#if QA
            Uri audioNavigation;
            audioDownloadNavigation = Uri.TryCreate(e.Uri, UriKind.Absolute, out audioNavigation)
                ? audioNavigation.Scheme + ": allowed=" + NavigationPolicy.Embedded(e.Uri, Options).ToString() : "invalid-uri";
#endif
            if (!NavigationPolicy.Embedded(e.Uri, Options))
            {
                e.Cancel = true;
                if (e.IsUserInitiated) OpenExternal(e.Uri);
                return;
            }
            Browser.CoreWebView2.Settings.IsWebMessageEnabled = NavigationPolicy.Embedded(e.Uri, Options);
            ShellReady = false;
            LayoutFallback();
            MediaReady = false;
            if (LyricsWindow != null) LyricsWindow.Reset();
            RefreshTray();
        }

        private void Completed(object sender, CoreWebView2NavigationCompletedEventArgs e)
        {
            if (!e.IsSuccess || e.HttpStatusCode >= 400)
            {
                ShowMessage("Husic 暂时没有连上", "请稍后重试，账号和播放列表都会保留。", true);
                return;
            }
            messagePanel.Hide();
            LayoutFallback();
            PublishDesktopState(null);
        }

        private void ShowMessage(string title, string detail, bool retry)
        {
            messageTitle.Text = title;
            messageDetail.Text = detail;
            retryButton.Visible = retry;
            loadingPulse.Enabled=!retry;
            LayoutStartup();
            messagePanel.Show();
            messagePanel.BringToFront();
            LayoutFallback();
        }

        private void MessageReceived(object sender, CoreWebView2WebMessageReceivedEventArgs e)
        {
            if (Browser.Source == null || !NavigationPolicy.Embedded(Browser.Source.AbsoluteUri, Options) || !NavigationPolicy.OriginIs(e.Source, Browser.Source.AbsoluteUri)) return;
            string raw = e.WebMessageAsJson;
            if (raw == null || raw.Length > 65536) return;
            try
            {
                Dictionary<string, object> data = new JavaScriptSerializer().Deserialize<Dictionary<string, object>>(raw);
                object type;
                if (data == null || !data.TryGetValue("type", out type)) return;
                bool musicOrigin = NavigationPolicy.OriginIs(e.Source, Options.Origin);
                if (raw.Length <= 4096 && ReceiveChromeMessage(data, musicOrigin)) return;
                if (!musicOrigin) return;
                if ((string)type == "music-artwork") { ReceiveArtwork(data); return; }
                if (raw.Length > 4096) return;
                if ((string)type == "open-youtube-settings") { OpenYouTubeSettings(); return; }
                if ((string)type == "music-lyrics") { ReceiveLyrics(data); return; }
                if ((string)type != "music-status") return;
                object value;
                Playing = data.TryGetValue("playing", out value) && value is bool && (bool)value;
                MediaReady = data.TryGetValue("ready", out value) && value is bool && (bool)value;
                SongTitle = data.TryGetValue("title", out value) && value is string ? ((string)value).Substring(0, Math.Min(((string)value).Length, 160)) : "";
                SongArtist = data.TryGetValue("artist", out value) && value is string ? ((string)value).Substring(0, Math.Min(((string)value).Length, 160)) : "";
                string caption = String.IsNullOrEmpty(SongTitle) ? "Husic" : SongTitle + " · Husic";
                Text = caption;
                string hint = String.IsNullOrEmpty(SongArtist) ? caption : SongTitle + " · " + SongArtist;
                tray.Text = hint.Substring(0, Math.Min(62, hint.Length));
                bool dark = data.TryGetValue("dark", out value) && value is bool && (bool)value;
                bool reduced = data.TryGetValue("reduced", out value) && value is bool && (bool)value;
                ApplyChromeTheme(dark, reduced);
                ApplyLyricsPreferences();
                RefreshTray();
            }
            catch (ArgumentException) { }
            catch (InvalidOperationException) { }
            catch (InvalidCastException) { }
        }

        private void ReceiveArtwork(Dictionary<string, object> data)
        {
            object title, value;
            if (!data.TryGetValue("title", out title) || !(title is string) || (string)title != SongTitle || !data.TryGetValue("dataUrl", out value)) return;
            if (value == null) { TrayMenu.SetArtwork(null); return; }
            string encoded = value as string;
            const string prefix = "data:image/png;base64,";
            if (encoded == null || !encoded.StartsWith(prefix, StringComparison.Ordinal) || encoded.Length > 50000) return;
            try
            {
                byte[] bytes = Convert.FromBase64String(encoded.Substring(prefix.Length));
                using (MemoryStream buffer = new MemoryStream(bytes))
                using (Image image = Image.FromStream(buffer, false, true))
                    if (image.Width == 64 && image.Height == 64) TrayMenu.SetArtwork(image);
            }
            catch (ArgumentException) { }
            catch (FormatException) { }
            catch (OutOfMemoryException) { }
        }

        private void RefreshTray()
        {
            if (TrayMenu != null) {
                TrayMenu.UpdateState(SongTitle, SongArtist, MediaReady, Playing, DarkChrome, ReducedMotion, Preferences != null && Preferences.Topmost);
                if (Preferences != null) TrayMenu.UpdateLyricsState(Preferences.LyricsEnabled, Preferences.LyricsLocked);
            }
        }

        private void ReceiveLyrics(Dictionary<string, object> data)
        {
            object line, secondary, state;
            if (!Preferences.LyricsEnabled || !data.TryGetValue("line", out line) || !(line is string)
                || !data.TryGetValue("secondary", out secondary) || !(secondary is string)
                || !data.TryGetValue("state", out state) || !(state is string)) return;
            string kind = (string)state;
            if (kind != "idle" && kind != "loading" && kind != "empty" && kind != "plain" && kind != "synced" && kind != "concealed") return;
            LyricsWindow.UpdateLyrics(((string)line).Substring(0, Math.Min(500, ((string)line).Length)),
                ((string)secondary).Substring(0, Math.Min(500, ((string)secondary).Length)), kind);
        }

        private void ApplyLyricsPreferences()
        {
            if (LyricsWindow == null) return;
            LyricsWindow.Apply(Preferences, ReducedMotion, Playing);
            LyricsWindow.Present(Preferences.LyricsEnabled);
        }

        private async void TrayCommand(string command)
        {
            if (command == "open") RestoreWindow();
            else if (command == "quit") await ExitMusic();
            else if (command == "pin") ChangeDesktopPreference("topmost", !Preferences.Topmost);
            else if (command == "lyrics") ChangeDesktopPreference("lyricsEnabled", !Preferences.LyricsEnabled);
            else if (command == "lyrics-lock") ChangeDesktopPreference("lyricsLocked", !Preferences.LyricsLocked);
            else if (command == "lyrics-smaller") ChangeDesktopPreference("lyricsSize", Math.Max(Preferences.LyricsMinFont, Preferences.LyricsSize - 2));
            else if (command == "lyrics-larger") ChangeDesktopPreference("lyricsSize", Math.Min(Preferences.LyricsMaxFont, Preferences.LyricsSize + 2));
            else if (command == "settings")
            {
                RestoreWindow();
                if (Browser.CoreWebView2 == null) return;
                if (Browser.Source != null && NavigationPolicy.OriginIs(Browser.Source.AbsoluteUri, Options.Origin) && ShellReady)
                    Browser.CoreWebView2.PostWebMessageAsJson("{\"type\":\"music-desktop-open-settings\"}");
                else Browser.CoreWebView2.Navigate(Options.Origin + "/settings");
            }
            else await Media(command);
        }

        internal Task Media(string action)
        {
            if (Browser.CoreWebView2 == null || Browser.Source == null || !NavigationPolicy.OriginIs(Browser.Source.AbsoluteUri, Options.Origin)) return Task.FromResult(false);
            if (action != "toggle" && action != "play" && action != "pause" && action != "nexttrack" && action != "previoustrack") return Task.FromResult(false);
            Browser.CoreWebView2.PostWebMessageAsJson(new JavaScriptSerializer().Serialize(new { type = "music-desktop-control", action = action }));
            return Task.FromResult(true);
        }

        internal void HideToTray()
        {
            Hide();
            if (!closeHintShown && !Options.Test)
            {
                closeHintShown = true;
                tray.ShowBalloonTip(2500, "音乐继续播放", "点击托盘图标打开 Husic；关闭行为可以在设置里调整。", ToolTipIcon.None);
            }
        }

        internal void RestoreWindow()
        {
            Show();
            if (WindowState == FormWindowState.Minimized) WindowState = FormWindowState.Normal;
            CorrectRestoredBounds();
            if (!Options.Test) { Activate(); SetForegroundWindow(Handle); }
        }

        protected override bool ShowWithoutActivation { get { return Options != null && Options.Test; } }

        internal async Task ExitMusic()
        {
            if (quitting) return;
            quitting = true;
            try { await Media("pause"); await Task.Delay(180); }
            catch (InvalidOperationException) { }
            finally { allowClose = true; Close(); }
        }

        protected override void OnFormClosing(FormClosingEventArgs e)
        {
            if (!allowClose && e.CloseReason == CloseReason.UserClosing)
            {
                e.Cancel = true;
                if (Preferences.CloseToTray) HideToTray();
                else BeginInvoke((Action)(async delegate { await ExitMusic(); }));
                return;
            }
            base.OnFormClosing(e);
        }

        protected override void OnFormClosed(FormClosedEventArgs e)
        {
            loadingPulse.Dispose();
            loadingMark.Image.Dispose();
            tray.Visible = false;
            tray.Dispose();
            TrayMenu.Dispose();
            LyricsWindow.Dispose();
            Browser.Dispose();
            base.OnFormClosed(e);
        }

        protected override void WndProc(ref Message m)
        {
            if(m.Msg==0x0112 && (m.WParam.ToInt64()&0xfff0)==0xf020)CaptureMinimizeBounds();
            if(m.Msg==0x0112 && (m.WParam.ToInt64()&0xfff0)==0xf030)CaptureMaximizeBounds();
            if (HandleChromeMessage(ref m)) return;
            if (m.Msg == 0x0319 && MediaReady)
            {
                int command = (int)((m.LParam.ToInt64() >> 16) & 0x7FFF);
                string action = command == 14 ? "toggle" : command == 11 ? "nexttrack" : command == 12 ? "previoustrack" : command == 46 ? "play" : command == 13 || command == 47 ? "pause" : null;
                if (action != null) { Media(action); m.Result = (IntPtr)1; return; }
            }
            base.WndProc(ref m);
            if(m.Msg==0x0005 && m.WParam.ToInt64()!=1){CorrectRestoredBounds();CorrectMaximizedRestore();}
        }

        private void OpenYouTubeSettings()
        {
            string target = Options.Origin + "/settings";
            if (Options.Test) return;
            try
            {
                string chrome = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles), "Google", "Chrome", "Application", "chrome.exe");
                if (!File.Exists(chrome)) chrome = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFilesX86), "Google", "Chrome", "Application", "chrome.exe");
                if (File.Exists(chrome)) Process.Start(new ProcessStartInfo(chrome, target) { UseShellExecute = true });
                else OpenExternal(target);
            }
            catch (System.ComponentModel.Win32Exception) { ShowMessage("请在浏览器中连接 YouTube", "打开浏览器里的 Husic 设置，完成连接后，这里会共用同一份连接。", true); }
        }

        private void OpenExternal(string target)
        {
            if (Options.Test || !(NavigationPolicy.External(target) || NavigationPolicy.OriginIs(target, Options.Origin))) return;
            try { Process.Start(new ProcessStartInfo(target) { UseShellExecute = true }); }
            catch (System.ComponentModel.Win32Exception) { }
        }
    }

    internal static class AudioDownloadPolicy
    {
        private static readonly HashSet<string> Extensions = new HashSet<string>(StringComparer.OrdinalIgnoreCase) {
            ".wav", ".mp3", ".flac", ".aac", ".m4a", ".ogg", ".opus", ".webm"
        };
        private static readonly HashSet<string> MimeTypes = new HashSet<string>(StringComparer.OrdinalIgnoreCase) {
            "audio/wav", "audio/wave", "audio/x-wav", "audio/vnd.wave", "audio/mpeg", "audio/mp3", "audio/x-mp3",
            "audio/flac", "audio/x-flac", "audio/aac", "audio/aacp", "audio/mp4", "audio/x-m4a",
            "audio/ogg", "audio/opus", "audio/webm", "application/ogg", "application/octet-stream"
        };

        internal static bool TryPrepare(string page, string download, string suggestedName, string mime,
            DesktopOptions options, out string filename)
        {
            filename = null;
            Uri blob;
            if (!NavigationPolicy.OriginIs(page, options.Origin) || String.IsNullOrEmpty(download)
                || !download.StartsWith("blob:", StringComparison.Ordinal)
                || !Uri.TryCreate(download.Substring(5), UriKind.Absolute, out blob)
                || !NavigationPolicy.OriginIs(blob.AbsoluteUri, options.Origin) || blob.AbsolutePath.Length <= 1
                || blob.Query.Length != 0 || blob.Fragment.Length != 0 || String.IsNullOrEmpty(mime)
                || !MimeTypes.Contains(mime.Split(';')[0].Trim())) return false;
            filename = SafeFileName(suggestedName);
            return filename != null;
        }

        internal static string SafeFileName(string value)
        {
            if (String.IsNullOrWhiteSpace(value)) return null;
            int separator = Math.Max(value.LastIndexOf('/'), value.LastIndexOf('\\'));
            value = value.Substring(separator + 1).Trim().TrimEnd('.', ' ');
            System.Text.StringBuilder clean = new System.Text.StringBuilder();
            foreach (char ch in value)
            {
                if (Char.IsControl(ch) || Char.GetUnicodeCategory(ch) == System.Globalization.UnicodeCategory.Format) continue;
                clean.Append("<>:\"/\\|?*".IndexOf(ch) >= 0 ? '_' : ch);
            }
            value = clean.ToString().Trim().TrimEnd('.', ' ');
            string extension = Path.GetExtension(value);
            if (!Extensions.Contains(extension)) return null;
            string stem = value.Substring(0, value.Length - extension.Length).Trim().TrimEnd('.', ' ');
            if (stem.Length == 0) stem = "Husic";
            if (stem.Length > 120) stem = stem.Substring(0, 120).TrimEnd();
            if (stem.Length > 0 && Char.IsHighSurrogate(stem[stem.Length - 1])) stem = stem.Substring(0, stem.Length - 1);
            string device = stem.Split('.')[0].TrimEnd(' ').ToUpperInvariant();
            if (device == "CON" || device == "PRN" || device == "AUX" || device == "NUL"
                || (device.Length == 4 && (device.StartsWith("COM", StringComparison.Ordinal) || device.StartsWith("LPT", StringComparison.Ordinal))
                    && device[3] >= '1' && device[3] <= '9')) stem = "_" + stem;
            return stem + extension.ToLowerInvariant();
        }

        internal static bool ValidSavePath(string path, string extension)
        {
            if (String.IsNullOrWhiteSpace(path) || !Extensions.Contains(extension)) return false;
            try { return Path.IsPathRooted(path) && Path.GetPathRoot(path).Length >= 3
                && String.Equals(Path.GetExtension(Path.GetFullPath(path)), extension, StringComparison.OrdinalIgnoreCase); }
            catch (ArgumentException) { return false; }
            catch (NotSupportedException) { return false; }
            catch (PathTooLongException) { return false; }
        }
    }
}
