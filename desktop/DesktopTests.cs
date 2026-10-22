#if QA
using System;
using System.Collections.Generic;
using System.Drawing;
using System.IO;
using System.Net;
using System.Net.Sockets;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading.Tasks;
using System.Web.Script.Serialization;
using System.Windows.Forms;
using Microsoft.Web.WebView2.Core;

namespace Hardware.Music
{
    // Built only into Music.QA.exe. The shipping app has no test origins,
    // profile override, arbitrary navigation or remote-debugging argument.
    internal static class DesktopTests
    {
        [DllImport("user32.dll")] private static extern IntPtr SendMessage(IntPtr handle, int message, IntPtr wParam, IntPtr lParam);
        [DllImport("user32.dll")] private static extern bool PrintWindow(IntPtr window, IntPtr dc, uint flags);
        [DllImport("user32.dll")] private static extern bool PostMessage(IntPtr window, int message, IntPtr wParam, IntPtr lParam);
        [DllImport("user32.dll")] private static extern IntPtr GetFocus();
        [DllImport("user32.dll")] private static extern bool SetForegroundWindow(IntPtr window);
        [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
        [DllImport("user32.dll")] private static extern bool SetCursorPos(int x, int y);
        [DllImport("user32.dll")] private static extern bool GetCursorPos(out Point point);
        [DllImport("user32.dll")] private static extern void mouse_event(uint flags, uint x, uint y, uint data, UIntPtr extra);
        [DllImport("user32.dll")] private static extern IntPtr WindowFromPoint(Point point);
        [DllImport("user32.dll")] private static extern IntPtr GetAncestor(IntPtr window, uint flags);
        private static readonly Dictionary<string, object> results = new Dictionary<string, object>();
        private static DesktopForm form;
        private static string evidenceDirectory;

        internal static int Run(string[] args)
        {
            if (args.Length != 2) return 3;
            string report = Path.GetFullPath(args[0]);
            string phase = args[1];
            string work = Path.GetDirectoryName(report);
            evidenceDirectory = work;
            string localWork = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Music", "QA", Path.GetFileName(work));
            string fixtures = Path.Combine(localWork, "fixtures");
            Directory.CreateDirectory(fixtures);
            File.WriteAllText(Path.Combine(fixtures, "index.html"), Fixture());
            string webSource = Environment.GetEnvironmentVariable("HUSIC_QA_WEB_SOURCE");
            if (String.IsNullOrWhiteSpace(webSource)) webSource = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "..", "..", "..", "web", "src");
            webSource = Path.GetFullPath(webSource);
            foreach (string css in new string[] { "styles.css", "settings.css" }) File.Copy(Path.Combine(webSource, css), Path.Combine(fixtures, css), true);
            File.WriteAllBytes(Path.Combine(fixtures, "brand.png"), Resources.Bytes("Music.Brand.png"));
            using (AudioFixtureServer audioServer = phase == "audio" ? new AudioFixtureServer(fixtures) : null)
            {
            DesktopOptions options = new DesktopOptions { Root = Path.Combine(localWork, "profile"), Origin = "https://music.desktop.test", Entry = "https://music.desktop.test/index.html", AppId = "com.hdwde.music.qa", FixtureDirectory = fixtures, Test = true };
            if (audioServer != null)
            {
                options.Origin = audioServer.Origin;
                options.Entry = audioServer.Origin + "/index.html";
                options.FixtureDirectory = null;
                results["audioFixtureTransport"] = "loopback-http";
            }
            options.ResetUi = phase == "reset-verify";
            if (phase == "first")
            {
                Directory.CreateDirectory(options.Root);
                File.WriteAllText(Path.Combine(options.Root, "window.json"), "{\"Version\":1,\"X\":70,\"Y\":90,\"Width\":980,\"Height\":720,\"Maximized\":true}");
            }
            int exit = 0;
            Application.ThreadException += delegate(object sender, System.Threading.ThreadExceptionEventArgs e) {
                results["uiException"] = e.Exception.ToString();
            };
            using (SingleInstance primary = new SingleInstance(options.AppId))
            using (form = new DesktopForm(options))
            {
                Assert(primary.Primary, "primary instance lock");
                primary.Activate += delegate { if (form.IsHandleCreated) form.BeginInvoke((Action)form.RestoreWindow); };
                form.Shown += async delegate
                {
                    try
                    {
                        await Within(form.Ready.Task, 20000);
                        await UntilScript("window.fixtureReady === true", 12000);
                        await Until(delegate { return form.ShellReady; }, 4000);
                        if (phase == "reset-seed") {
                            var cookie=form.Browser.CoreWebView2.CookieManager.CreateCookie("husic-gate-fixture","allowed","music.desktop.test","/");
                            cookie.IsSecure=true;cookie.IsHttpOnly=true;cookie.Expires=DateTime.UtcNow.AddDays(1);
                            form.Browser.CoreWebView2.CookieManager.AddOrUpdateCookie(cookie);
                            await form.Browser.CoreWebView2.ExecuteScriptAsync("localStorage.setItem('music-reset-fixture','old');window.seedDone=false;const r=indexedDB.open('music-reset-fixture',1);r.onupgradeneeded=()=>r.result.createObjectStore('pictures');r.onsuccess=()=>{r.result.close();window.seedDone=true}");
                            await UntilScript("window.seedDone",3000);
                            form.ChangeDesktopPreference("lyricsEnabled",true);form.ChangeDesktopPreference("zoom",1.25);
                            form.ChangeDesktopPreference("lyricsOrientation","vertical");
                            Assert(DesktopPreferenceStore.Load(options.Root).LyricsEnabled,"seed preferences persisted");
                            results["seededCookieStorageAndPreferences"]=true;
                        }
                        else if(phase == "reset-verify") {
                            Assert(!form.Preferences.LyricsEnabled&&form.Preferences.Zoom==1&&form.Preferences.LyricsOrientation=="horizontal","native preferences reset");
                            await UntilScript("localStorage.getItem('music-reset-fixture')===null",2000);
                            await form.Browser.CoreWebView2.ExecuteScriptAsync("window.databasesGone=false;indexedDB.databases().then(d=>window.databasesGone=!d.some(x=>x.name==='music-reset-fixture'))");
                            await UntilScript("window.databasesGone",3000);
                            var cookies=await form.Browser.CoreWebView2.CookieManager.GetCookiesAsync(options.Origin);
                            Assert(cookies.Exists(c=>c.Name=="husic-gate-fixture"&&c.Value=="allowed"),"gate cookie preserved");
                            DesktopOptions publicOptions=new DesktopOptions();
                            Assert(publicOptions.Origin=="http://127.0.0.1:18788"&&publicOptions.LoginOrigin=="","public defaults have no private gate");
                            Assert(!NavigationPolicy.Embedded("https://auth.hdwde.com/",publicOptions),"private gate not implicitly trusted");
                            Assert(NavigationPolicy.OriginIs("http://127.0.0.1:18788/settings",publicOptions.Origin)&&!NavigationPolicy.OriginIs("http://127.0.0.1:18789/",publicOptions.Origin),"loopback port exact match");
                            Assert(!NavigationPolicy.ValidOrigin("http://example.com")&&!NavigationPolicy.OriginIs("https://user@music.desktop.test/",options.Origin),"unsafe origins rejected");
                            results["resetStorageAndPreferencesGateCookieRetained"]=true;results["publicDefaultIsSelfHosted"]=true;
                        }
                        else if (phase == "drag") await DragWindow();
                        else if (phase == "audio") await LocalAudio();
                        else if (phase == "first") await First(primary);
                        else if (phase == "reopen") await Reopen();
                        else if (phase == "window") await WindowPreferences();
                        else if(phase=="secondary") {
                            results["secondaryStage"]="enable lyrics";
                            form.Activate();SetForegroundWindow(form.Handle);await PaintReady();
                            await form.Browser.CoreWebView2.ExecuteScriptAsync("window.secondaryClicks=[];document.addEventListener('click',e=>secondaryClicks.push({target:e.target.closest('button')?.dataset.setting,trusted:e.isTrusted}),true)");
                            await ClickWeb("[data-setting='lyricsEnabled']");
                            results["secondaryDiagnostic"]=new{nativeVersion=System.Reflection.Assembly.GetExecutingAssembly().GetName().Version.ToString(),request=form.LastPreferenceChange,enabled=form.Preferences.LyricsEnabled,dom=await form.Browser.CoreWebView2.ExecuteScriptAsync("({checked:document.querySelector('[data-setting=lyricsEnabled]').getAttribute('aria-checked'),footer:document.querySelector('.desktop-version')?.textContent})")};
                            await Until(delegate{return form.Preferences.LyricsEnabled;},2000);
                            await UntilScript("document.querySelector('.desktop-lyrics-disclosure').classList.contains('is-open')",2000);
                            await Task.Delay(340);
                            await ClickWeb("[data-lyric-pref='lyricsOrientation'][value='vertical']");
                            await Until(delegate{return form.Preferences.LyricsOrientation=="vertical";},2000);
                            await UntilScript("document.querySelector('[data-lyric-pref=lyricsOrientation][value=vertical]').closest('label').classList.contains('is-selected')",2000);
                            await ClickWeb("[data-lyric-secondary-color='#c4aaff']");
                            await Until(delegate{return form.Preferences.LyricsSecondaryColor=="#c4aaff";},2000);
                            await UntilScript("document.querySelector('[data-lyric-secondary-color=\"#c4aaff\"]').getAttribute('aria-pressed')==='true'",2000);
                            results["nativeOrientationAndSecondaryPalette"]=true;
                            results["secondaryStage"]="disable secondary";
                            await ClickWeb("[data-setting='lyricsSecondaryEnabled']");
                            await Until(delegate{return !form.Preferences.LyricsSecondaryEnabled;},2000);
                            form.LyricsWindow.UpdateLyrics("保留主歌词","隐藏这一行","synced");
                            using(Bitmap bitmap=form.LyricsWindow.RenderBitmap())Assert(!form.LyricsWindow.RenderedSecondary,"real settings switch removes secondary text");
                            Capture(form,"secondary-settings.png");results["secondarySwitchAndDisclosure"]=true;
                        }
                        else if(phase=="secondary-reopen") {
                            Assert(form.Preferences.LyricsEnabled&&!form.Preferences.LyricsSecondaryEnabled,"secondary-off persists across restart");
                            Assert(form.Preferences.LyricsOrientation=="vertical"&&form.Preferences.LyricsSecondaryColor=="#c4aaff","orientation and secondary palette persist across restart");
                            results["secondaryPreferencePersists"]=true;
                        }
                        else if (phase == "restore") {
                            form.Bounds=new Rectangle(180,160,1040,760);
                            Rectangle expected=form.Bounds;List<string> samples=new List<string>();
                            for(int i=0;i<8;i++){
                                form.WindowCommand("minimize");await Until(delegate{return form.WindowState==FormWindowState.Minimized;},1500);
                                await Task.Delay(100);form.RestoreWindow();await Task.Delay(220);
                                samples.Add(form.Bounds.ToString());results["restoreSamples"]=samples;
                                Assert(form.Bounds==expected,"minimize/restore cycle "+(i+1)+" must preserve exact bounds");
                            }
                            List<string> maximizedSamples=new List<string>();
                            for(int i=0;i<8;i++){
                                form.WindowCommand("maximize");await Until(delegate{return form.WindowState==FormWindowState.Maximized;},1500);
                                await Task.Delay(100);form.WindowCommand("maximize");await Task.Delay(220);
                                maximizedSamples.Add(form.Bounds.ToString());results["maximizeRestoreSamples"]=maximizedSamples;
                                Assert(form.Bounds==expected,"maximize/restore cycle "+(i+1)+" must preserve exact bounds");
                            }
                            List<string> mixedSamples=new List<string>();
                            for(int i=0;i<4;i++){
                                SendMessage(form.Handle,0x0112,(IntPtr)0xf030,IntPtr.Zero);
                                await Task.Delay(180);
                                Assert(form.WindowState==FormWindowState.Maximized,"system maximize accepted");
                                SendMessage(form.Handle,0x0112,(IntPtr)0xf020,IntPtr.Zero);
                                await Task.Delay(180);
                                if(i%2==0)form.RestoreWindow();
                                else SendMessage(form.Handle,0x0112,(IntPtr)0xf120,IntPtr.Zero);
                                await Task.Delay(220);
                                Assert(form.WindowState==FormWindowState.Maximized,"restore from minimized keeps maximized state");
                                SendMessage(form.Handle,0x0112,(IntPtr)0xf120,IntPtr.Zero);
                                await Task.Delay(220);
                                mixedSamples.Add(form.Bounds.ToString());results["mixedRestoreSamples"]=mixedSamples;
                                Assert(form.WindowState==FormWindowState.Normal&&form.Bounds==expected,"mixed system restore preserves exact bounds");
                            }
                            results["restoreStable"]=true;
                        }
                        else if (phase == "startup") {
                            form.PreviewStartup();await Task.Delay(200);Assert(form.StartupCentered,"startup logo, name and retry share the center line");
                            Capture(form,"startup.png");results["startupCentered"]=true;
                        }
                        else if (phase == "font") {
                            form.ChangeDesktopPreference("lyricsEnabled",true);
                            Rectangle area=Screen.PrimaryScreen.WorkingArea;
                            form.LyricsWindow.Top=area.Bottom-form.LyricsWindow.Height-5;
                            Assert(form.ChangeDesktopPreference("lyricsMaxFont",80) && form.ChangeDesktopPreference("lyricsMinFont",72),"large font limits accepted");
                            Assert(area.Contains(form.LyricsWindow.Bounds),"expanded minimum-size lyrics stay on screen");
                            using(Bitmap bitmap=form.LyricsWindow.RenderBitmap())Assert(form.LyricsWindow.RenderedFontSize>=72 && form.LyricsWindow.RenderedFontSize<=80,"large font remains inside custom limits");
                            results["largeFontBoundary"]=new{bounds=form.LyricsWindow.Bounds.ToString(),size=form.LyricsWindow.RenderedFontSize,onScreen=true};
                        }
                        else if (phase == "font-reopen") {
                            Assert(form.Preferences.LyricsMinFont==72 && form.Preferences.LyricsMaxFont==80,"large range persists");
                            Assert(Screen.PrimaryScreen.WorkingArea.Contains(form.LyricsWindow.Bounds),"large initial lyric window fits work area");
                            results["largeFontReopen"]=true;
                        }
                        else if (phase == "window-reopen") {
                            Rectangle expected=WindowLayout.LaunchBounds(form.Preferences);
                            Assert(form.Preferences.SavedWindowWidth==1040 && form.Preferences.SavedWindowHeight==760,"explicitly saved window dimensions survive restart");
                            Assert(form.Bounds==expected && form.WindowState==FormWindowState.Normal,"saved size reopens centered, not at previous position");
                            results["savedWindowReopen"]=new{actual=form.Bounds.ToString(),centered=true};
                        }
                        else if (phase == "lyrics") { AssertDefaultWindow(); await Lyrics(); await PersonalLyrics(); }
                        else if (phase == "lyrics-reopen") {
                            AssertDefaultWindow();
                            Assert(form.Preferences.LyricsEnabled && !form.Preferences.LyricsLocked && !form.Preferences.LyricsSecondaryEnabled && form.Preferences.LyricsSize == 30 && form.LyricsWindow.Visible, "lyric preferences, secondary-off and visible overlay survive a new process");
                            results["lyricPreferencesSurvive"] = true;
                        }
                        else throw new InvalidOperationException("Unknown test phase");
                        Assert(!results.ContainsKey("uiException"), "native UI must render without painting exceptions");
                        results["ok"] = true;
                    }
                    catch (Exception ex) {
                        results["ok"] = false; results["errorType"] = ex.GetType().Name; results["failure"] = ex.Message;
                        if (phase == "audio") {
                            results["audioNativeDiagnostic"] = form.AudioDownloadSnapshot();
                            results["audioFixtureServerFailure"] = audioServer == null ? null : audioServer.FailureType;
                        }
                        Capture(form, "failure-window.png"); exit = 1;
                    }
                    finally
                    {
                        File.WriteAllText(report, new JavaScriptSerializer().Serialize(results));
                    }
                    await form.ExitMusic();
                };
                Application.Run(form);
            }
            return exit;
            }
        }

        // Audio export QA uses a real HTTP origin rather than a virtual-host
        // resource mapping. This listener is QA-only, ephemeral, loopback-only,
        // and serves four in-memory fixture files, never arbitrary paths.
        private sealed class AudioFixtureServer : IDisposable
        {
            private readonly TcpListener listener;
            private readonly Dictionary<string, byte[]> files = new Dictionary<string, byte[]>(StringComparer.Ordinal);
            private volatile bool stopping;
            private volatile TcpClient activeClient;
            internal string Origin { get; private set; }
            internal string FailureType { get; private set; }

            internal AudioFixtureServer(string directory)
            {
                foreach (string file in new string[] { "index.html", "styles.css", "settings.css", "brand.png" })
                    files["/" + file] = File.ReadAllBytes(Path.Combine(directory, file));
                files["/"] = files["/index.html"];
                listener = new TcpListener(IPAddress.Loopback, 0);
                listener.Start();
                Origin = "http://127.0.0.1:" + ((IPEndPoint)listener.LocalEndpoint).Port;
                Task.Run((Action)Serve);
            }

            private void Serve()
            {
                while (!stopping)
                {
                    try
                    {
                        using (TcpClient client = listener.AcceptTcpClient())
                        {
                            activeClient = client;
                            client.ReceiveTimeout = 2000; client.SendTimeout = 2000;
                            using (NetworkStream stream = client.GetStream())
                            {
                                StringBuilder request = new StringBuilder();
                                while (request.Length < 8192)
                                {
                                    int value = stream.ReadByte();
                                    if (value < 0) break;
                                    request.Append((char)value);
                                    int n = request.Length;
                                    if (n >= 4 && request[n - 4] == '\r' && request[n - 3] == '\n'
                                        && request[n - 2] == '\r' && request[n - 1] == '\n') break;
                                }
                                string[] firstLine = request.ToString().Split('\r')[0].Split(' ');
                                string method = firstLine.Length >= 2 ? firstLine[0] : "";
                                string target = firstLine.Length >= 2 ? firstLine[1].Split('?')[0] : "";
                                byte[] content;
                                bool found = (method == "GET" || method == "HEAD") && files.TryGetValue(target, out content);
                                if (!found) content = new byte[0];
                                else content = files[target];
                                string mime = target.EndsWith(".png", StringComparison.Ordinal) ? "image/png"
                                    : target.EndsWith(".css", StringComparison.Ordinal) ? "text/css; charset=utf-8" : "text/html; charset=utf-8";
                                string header = "HTTP/1.1 " + (found ? "200 OK" : "404 Not Found") + "\r\nContent-Type: " + mime
                                    + "\r\nContent-Length: " + content.Length + "\r\nConnection: close\r\nCache-Control: no-store\r\n\r\n";
                                byte[] response = Encoding.ASCII.GetBytes(header);
                                stream.Write(response, 0, response.Length);
                                if (method != "HEAD") stream.Write(content, 0, content.Length);
                            }
                        }
                    }
                    catch (SocketException ex) { if (!stopping) FailureType = ex.GetType().Name; }
                    catch (IOException ex) { if (!stopping) FailureType = ex.GetType().Name; }
                    catch (ObjectDisposedException) { if (!stopping) FailureType = "ObjectDisposedException"; }
                    finally { activeClient = null; }
                }
            }

            public void Dispose()
            {
                stopping = true;
                listener.Stop();
                TcpClient client = activeClient;
                if (client != null) client.Close();
            }
        }

        private static async Task LocalAudio()
        {
            results["audioStage"] = "policy";
            form.Activate();
            SetForegroundWindow(form.Handle);
            form.Browser.Focus();
            await PaintReady();
            string name;
            string origin = form.Options.Origin;
            string blob = "blob:" + origin + "/4846ad6a-e2a3-4c8a-9ef2-828dc2e552a1";
            foreach (string extension in new string[] { "wav", "mp3", "flac", "aac", "m4a", "ogg", "opus", "webm" })
                Assert(AudioDownloadPolicy.TryPrepare(origin + "/library/local", blob, "曲名." + extension,
                    "application/octet-stream", form.Options, out name), "audio extension allowed: " + extension);
            Assert(!AudioDownloadPolicy.TryPrepare("https://login.desktop.test/", blob, "test.wav", "audio/wav", form.Options, out name), "gate page cannot export");
            Assert(!AudioDownloadPolicy.TryPrepare(null, blob, "test.wav", "audio/wav", form.Options, out name), "missing current origin cannot export");
            foreach (string uri in new string[] {
                origin + "/track.wav", "file:///C:/track.wav", "blob:null/fixture", "blob:https://music.desktop.test.evil.test/fixture",
                "blob:https://music.desktop.test:444/fixture", "blob:https://user@music.desktop.test/fixture", blob + "?download=1", blob + "#test"
            }) Assert(!AudioDownloadPolicy.TryPrepare(origin, uri, "test.wav", "audio/wav", form.Options, out name), "foreign or non-blob download rejected: " + uri);
            Assert(!AudioDownloadPolicy.TryPrepare(origin, blob, "test.exe", "application/octet-stream", form.Options, out name), "executable rejected");
            Assert(!AudioDownloadPolicy.TryPrepare(origin, blob, "test.wav", "text/html", form.Options, out name), "non-audio content rejected");
            Assert(AudioDownloadPolicy.SafeFileName("C:\\not-a-destination\\曲<名>.WAV") == "曲_名_.wav", "directory stripped and invalid filename characters sanitized");
            Assert(AudioDownloadPolicy.SafeFileName("CON.wav") == "_CON.wav", "reserved Windows device name escaped");
            Assert(AudioDownloadPolicy.SafeFileName(new String('a', 400) + ".flac").Length <= 126, "filename length bounded");
            Assert(!AudioDownloadPolicy.ValidSavePath("C:\\test.exe", ".wav") && !AudioDownloadPolicy.ValidSavePath("test.wav", ".wav")
                && !AudioDownloadPolicy.ValidSavePath("C:test.wav", ".wav") && !AudioDownloadPolicy.ValidSavePath("\\test.wav", ".wav"),
                "chosen path must be fully qualified and keep audio extension");
            Assert(form.Browser.AllowExternalDrop && !NavigationPolicy.Embedded("file:///C:/track.wav", form.Options), "File drops enabled without allowing file navigation");
            results["localAudioDownloadBoundary"] = true;
            await VerifyLocalAudioCodecs();

            // No real song or user directory: this is a tiny, self-built silent
            // PCM wave written only within the isolated native QA profile.
            byte[] wave = SilentWave();
            string audioWork = Path.Combine(form.Options.Root, "audio-export");
            Directory.CreateDirectory(audioWork);
            string cancelPath = Path.Combine(audioWork, "canceled.wav");
            string savePath = Path.Combine(audioWork, "saved.wav");
            await form.Browser.CoreWebView2.ExecuteScriptAsync(@"window.qaExportBytes=Uint8Array.from(atob(" + new JavaScriptSerializer().Serialize(Convert.ToBase64String(wave)) + @"),c=>c.charCodeAt(0));
                const b=document.createElement('button');b.id='qa-audio-export';b.textContent='导出隔离音频';b.style.cssText='position:fixed;left:380px;top:230px;z-index:1000';
                window.qaExportClicks=[];
                b.onclick=e=>{qaExportClicks.push({trusted:e.isTrusted,active:navigator.userActivation.isActive,focused:document.hasFocus()});const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([qaExportBytes],{type:'audio/wav'}));a.download='隔离音频.wav';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(a.href),30000)};document.body.append(b)");
            int prompts = 0;
            bool dialogUi = false, dialogOwner = false, dialogOptions = false;
            form.AudioSaveDialogOverride = delegate(SaveFileDialog dialog, IWin32Window owner) {
                prompts++;
                dialogUi = !form.InvokeRequired; dialogOwner = Object.ReferenceEquals(owner, form);
                dialogOptions = dialog.FileName == "隔离音频.wav" && dialog.OverwritePrompt && dialog.CheckPathExists && dialog.AddExtension;
                results["audioDialogDiagnostic"] = new { uiThread = dialogUi, owner = dialogOwner,
                    safeName = AudioDownloadPolicy.SafeFileName(dialog.FileName), options = dialogOptions };
                dialog.FileName = cancelPath;
                return DialogResult.Cancel;
            };
            results["audioStage"] = "cancel-click";
            form.LastAudioDownload = null;
            await ClickAudioExport();
            results["audioCancelClickDiagnostic"] = await form.Browser.CoreWebView2.ExecuteScriptAsync("({clicks:window.qaExportClicks,focused:document.hasFocus(),atButton:(()=>{const e=document.querySelector('#qa-audio-export'),r=e.getBoundingClientRect();return document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.id})(),blobSupport:typeof URL.createObjectURL})");
            results["audioStage"] = "cancel-wait";
            await Until(delegate { return form.LastAudioDownload != null && form.LastAudioDownloadDecision == "canceled"
                && form.LastAudioDownload.State == CoreWebView2DownloadState.Interrupted; }, 4000);
            Assert(dialogUi && dialogOwner, "Save As runs on UI thread with player owner");
            Assert(dialogOptions, "Save As has sanitized audio name and normal user confirmation");
            Assert(prompts == 1 && !File.Exists(cancelPath), "canceling Save As writes no file");
            results["audioSaveCancelWritesNothing"] = true;
            form.AudioSaveDialogOverride = delegate(SaveFileDialog dialog, IWin32Window owner) {
                prompts++;
                dialogUi = !form.InvokeRequired; dialogOwner = Object.ReferenceEquals(owner, form);
                dialog.FileName = savePath;
                return DialogResult.OK;
            };
            results["audioStage"] = "save-click";
            form.LastAudioDownload = null;
            await ClickAudioExport();
            results["audioStage"] = "save-wait";
            await Until(delegate { return form.LastAudioDownload != null && form.LastAudioDownloadDecision == "saved"
                && form.LastAudioDownload.State == CoreWebView2DownloadState.Completed && File.Exists(savePath); }, 5000);
            Assert(prompts == 2 && Convert.ToBase64String(File.ReadAllBytes(savePath)) == Convert.ToBase64String(wave), "confirmed Blob export preserves exact WAV bytes");
            Assert(dialogUi && dialogOwner, "save owner and UI thread preserved");
            results["audioBlobExportSavedExactBytes"] = true;
            await form.Browser.CoreWebView2.ExecuteScriptAsync(@"document.querySelector('#qa-audio-export').onclick=()=>{
                const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([qaExportBytes],{type:'application/octet-stream'}));a.download='must-not-save.exe';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(a.href),30000)}");
            form.LastAudioDownload = null;
            results["audioStage"] = "blocked-click";
            await ClickAudioExport();
            results["audioStage"] = "blocked-wait";
            await Until(delegate { return form.LastAudioDownload != null && form.LastAudioDownloadDecision == "blocked"
                && form.LastAudioDownload.State == CoreWebView2DownloadState.Interrupted; }, 4000);
            Assert(prompts == 2, "an executable Blob never reaches Save As");
            results["executableBlobBlockedBeforeDialog"] = true;
            form.AudioSaveDialogOverride = null;

        }

        private static async Task ClickAudioExport()
        {
            form.Activate();
            SetForegroundWindow(form.Handle);
            bool focused = form.Browser.Focus();
            await PaintReady();
            string phase = Convert.ToString(results["audioStage"]);
            results[phase + "-before"] = new { browserFocusAccepted = focused,
                foregroundIsQa = GetForegroundWindow() == form.Handle,
                document = await form.Browser.CoreWebView2.ExecuteScriptAsync("({focused:document.hasFocus(),visibility:document.visibilityState,active:navigator.userActivation.isActive,hadActivation:navigator.userActivation.hasBeenActive})") };
            await ClickWeb("#qa-audio-export");
            results[phase + "-after"] = new { foregroundIsQa = GetForegroundWindow() == form.Handle,
                document = await form.Browser.CoreWebView2.ExecuteScriptAsync("({focused:document.hasFocus(),visibility:document.visibilityState,active:navigator.userActivation.isActive,hadActivation:navigator.userActivation.hasBeenActive,clicks:window.qaExportClicks})") };
        }

        private static async Task VerifyLocalAudioCodecs()
        {
            string fixtureDirectory = Environment.GetEnvironmentVariable("HUSIC_AUDIO_FIXTURE_DIR");
            if (!String.IsNullOrWhiteSpace(fixtureDirectory))
            {
                Dictionary<string, object> codecs = new Dictionary<string, object>();
                results["localAudioCodecs"] = codecs;
                string[] formats = { "WAV.wav", "MP3.mp3", "FLAC.flac", "AAC.aac" };
                string[] mimeTypes = { "audio/wav", "audio/mpeg", "audio/flac", "audio/aac" };
                for (int i = 0; i < formats.Length; i++)
                {
                    byte[] data = File.ReadAllBytes(Path.Combine(fixtureDirectory, "Fixture-" + formats[i]));
                    results["audioStage"] = "codec-" + formats[i];
                    Assert(data.Length > 0 && data.Length < 16 * 1024 * 1024, "codec fixture is a bounded local QA file");
                    string encoded = new JavaScriptSerializer().Serialize(Convert.ToBase64String(data));
                    string mimeType = new JavaScriptSerializer().Serialize(mimeTypes[i]);
                    await form.Browser.CoreWebView2.ExecuteScriptAsync(@"(()=>{window.qaCodec={ok:false,error:null};
                        const data=Uint8Array.from(atob(" + encoded + @"),c=>c.charCodeAt(0));const src=URL.createObjectURL(new Blob([data],{type:" + mimeType + @"}));
                        window.qaCodecAudio=new Audio(src);qaCodecAudio.muted=true;
                        qaCodecAudio.ontimeupdate=()=>{if(qaCodecAudio.currentTime>.08){qaCodec.ok=true;qaCodec.time=qaCodecAudio.currentTime;qaCodec.duration=qaCodecAudio.duration;qaCodecAudio.pause();URL.revokeObjectURL(src)}};
                        qaCodecAudio.onerror=()=>{qaCodec.error=qaCodecAudio.error?.code||'decode-error';URL.revokeObjectURL(src)};
                        qaCodecAudio.play().catch(e=>{qaCodec.error=e.name;URL.revokeObjectURL(src)})})()");
                    await UntilScript("window.qaCodec && (qaCodec.ok || qaCodec.error !== null)", 7000);
                    string status = await form.Browser.CoreWebView2.ExecuteScriptAsync("window.qaCodec");
                    results["codecLastStatus"] = new { format = formats[i], state = status };
                    Assert(await form.Browser.CoreWebView2.ExecuteScriptAsync("window.qaCodec.ok === true && window.qaCodec.time > .08") == "true",
                        "real WebView audio decoded and advanced: " + formats[i]);
                    codecs[formats[i]] = new { decoded = true, status = status };
                }
            }
            else results["localAudioCodecs"] = "not requested; set HUSIC_AUDIO_FIXTURE_DIR for WAV MP3 FLAC AAC playback checks";
        }

        private static byte[] SilentWave()
        {
            using (MemoryStream memory = new MemoryStream())
            using (BinaryWriter writer = new BinaryWriter(memory))
            {
                writer.Write(System.Text.Encoding.ASCII.GetBytes("RIFF")); writer.Write(36 + 4000);
                writer.Write(System.Text.Encoding.ASCII.GetBytes("WAVEfmt ")); writer.Write(16);
                writer.Write((short)1); writer.Write((short)1); writer.Write(8000); writer.Write(16000);
                writer.Write((short)2); writer.Write((short)16); writer.Write(System.Text.Encoding.ASCII.GetBytes("data"));
                writer.Write(4000); writer.Write(new byte[4000]); return memory.ToArray();
            }
        }

        private static async Task First(SingleInstance primary)
        {
            AssertDefaultWindow();
            Assert(!NavigationPolicy.OriginIs("https://music-g1.hdwde.com.evil.test/", "https://music-g1.hdwde.com"), "lookalike origin rejected");
            Assert(!NavigationPolicy.OriginIs("https://music-g1.hdwde.com:444/", "https://music-g1.hdwde.com"), "alternate origin port rejected");
            Assert(!NavigationPolicy.External("file:///C:/Windows/System32/cmd.exe"), "custom/local protocol rejected");
            results["originBoundary"] = true;
            await Until(delegate { return form.MediaReady && form.SongTitle == "Fixture 1"; }, 4000);
            await Chrome();
            await Lyrics();
            Assert(form.TrayVisible, "tray available");
            results["trayAndBrand"] = true;
            await form.Media("toggle");
            await Until(delegate { return form.Playing; }, 4000);
            await form.Media("pause");
            await Until(delegate { return !form.Playing; }, 3000);
            await form.Media("pause");
            await Task.Delay(150);
            Assert(!form.Playing, "duplicate pause must not resume");
            await form.Media("nexttrack");
            await Until(delegate { return form.SongTitle == "Fixture 2"; }, 3000);
            await form.Media("previoustrack");
            await Until(delegate { return form.SongTitle == "Fixture 1"; }, 3000);
            results["sharedTransport"] = true;
            SendMessage(form.Handle, 0x0319, form.Handle, (IntPtr)(14 << 16));
            await Until(delegate { return form.Playing; }, 3000);
            results["nativeMediaCommand"] = true;
            form.Bounds = new Rectangle(form.Bounds.X, form.Bounds.Y, 980, 720);
            await Task.Delay(400);
            double before = await Number("window.fixtureAudio.currentTime");
            form.Close(); // User close, not application exit: the audio survives.
            Assert(!form.Visible && form.TrayVisible, "close minimizes to tray");
            await Task.Delay(1500);
            double after = await Number("window.fixtureAudio.currentTime");
            Assert(form.Playing && after > before + .6, "hidden audio clock keeps advancing");
            results["hiddenAudioContinues"] = true;
            for (int attempt = 0; attempt < 2; attempt++)
            {
                form.HideToTray();
                bool signaled = await Task.Run(delegate
                {
                    using (SingleInstance secondary = new SingleInstance(form.Options.AppId))
                    { return !secondary.Primary && secondary.NotifyPrimary(); }
                });
                Assert(signaled, "repeated second launch notifies primary");
                await Until(delegate { return form.Visible; }, 2000);
                results["secondaryActivations"] = attempt + 1;
            }
            results["singleInstanceActivation"] = true;
            Assert(form.Width == 980 && form.Height == 720, "tray restore keeps current-session size");
            results["trayRestoreKeepsSessionGeometry"] = true;
            Assert(await Number("window.fixtureAudioCount") == 1, "no second audio instance");
            CoreWebView2Cookie cookie = form.Browser.CoreWebView2.CookieManager.CreateCookie("music_qa_session", "fixture-only", "music.desktop.test", "/");
            cookie.IsHttpOnly = true; cookie.IsSecure = true; cookie.Expires = DateTime.Now.AddDays(1);
            form.Browser.CoreWebView2.CookieManager.AddOrUpdateCookie(cookie);
            await form.Browser.CoreWebView2.ExecuteScriptAsync("localStorage.setItem('music-qa-preference','dark');localStorage.setItem('music-qa-queue','saved')");
            Assert(form.ChangeDesktopPreference("zoom", .85), "desktop zoom saved before reopen");
            Rectangle visible = WindowLayout.DefaultBounds();
            Assert(Screen.PrimaryScreen.WorkingArea.Contains(visible), "default window fits current work area");
            results["defaultFitsWorkArea"] = true;
            await Navigate(delegate { form.Browser.CoreWebView2.Navigate(form.Options.Origin + "/missing.html"); });
            Assert(form.ErrorVisible, "offline/error recovery UI");
            await Navigate(delegate { form.NavigateHome(); });
            await UntilScript("window.fixtureReady === true", 3000);
            Assert(!form.ErrorVisible, "retry restores fixture");
            results["failedNavigationAndRetry"] = true;
            await form.Browser.CoreWebView2.ExecuteScriptAsync("location.href='https://untrusted.desktop.invalid/'");
            await Task.Delay(350);
            Assert(NavigationPolicy.OriginIs(form.Browser.Source.AbsoluteUri, form.Options.Origin), "untrusted navigation blocked");
            results["untrustedNavigationBlocked"] = true;
            // Chromium flushes persistent cookies before the whole QA process
            // exits; the next phase is a new Windows process and WebView.
            await Task.Delay(1200);
            Assert(File.ReadAllText(Path.Combine(form.Options.Root, "window.json")).Contains("\"X\":70"), "legacy geometry file is not overwritten");
            form.Bounds = new Rectangle(90, 110, 980, 720);
            results["changedBoundsBeforeExit"] = true;
        }

        private static async Task Reopen()
        {
            IList<CoreWebView2Cookie> cookies = await form.Browser.CoreWebView2.CookieManager.GetCookiesAsync(form.Options.Origin);
            bool found = false;
            foreach (CoreWebView2Cookie cookie in cookies) if (cookie.Name == "music_qa_session" && cookie.IsHttpOnly && cookie.Value == "fixture-only") found = true;
            Assert(found, "HTTP-only login cookie survives process restart");
            await UntilScript("localStorage.getItem('music-qa-preference') === 'dark' && localStorage.getItem('music-qa-queue') === 'saved'", 2000);
            AssertDefaultWindow();
            results["persistentLoginCookie"] = true;
            results["preferencesAndQueueSurvive"] = true;
            results["windowGeometryResetsAcrossProcess"] = true;
            Assert(form.Preferences.Zoom == .85 && Math.Abs(form.Browser.ZoomFactor - .85) < .001, "desktop zoom survives process restart");
            results["desktopPreferencesSurvive"] = true;
            Assert(form.Preferences.LyricsEnabled && !form.Preferences.LyricsLocked && form.Preferences.LyricsSize == 30, "lyric display preferences survive restart");
            Assert(form.LyricsWindow.Visible, "enabled lyric window reappears without opening another player");
            results["lyricPreferencesSurvive"] = true;
        }

        private static async Task Lyrics()
        {
            form.Activate(); SetForegroundWindow(form.Handle);
            await ClickWeb("[data-setting='lyricsEnabled']");
            await Until(delegate { return form.LyricsWindow.Visible; }, 2000);
            Assert(GetForegroundWindow() != form.LyricsWindow.Handle, "lyric window must not steal focus from the clicked settings page");
            await form.Browser.CoreWebView2.ExecuteScriptAsync("fixtureAudio.currentTime=.4;fixtureLyrics()");
            await Until(delegate { return form.LyricsWindow.CurrentLine == "第一句，风从身边经过"; }, 2000);
            await form.Browser.CoreWebView2.ExecuteScriptAsync("fixtureAudio.currentTime=3;fixtureLyrics()");
            await Until(delegate { return form.LyricsWindow.CurrentLine == "第二句，把日子慢慢唱"; }, 2000);
            await form.Browser.CoreWebView2.ExecuteScriptAsync("fixtureAudio.currentTime=.4;fixtureLyrics()");
            await Until(delegate { return form.LyricsWindow.CurrentLine == "第一句，风从身边经过"; }, 2000);
            await Task.Delay(200);
            Capture(form.LyricsWindow, "lyrics-light.png");
            await ClickWeb("[data-lyric-size='more']");
            await Until(delegate { return form.Preferences.LyricsSize == 30; }, 1500);
            Assert(!form.ChangeDesktopPreference("lyricsSize", 100), "invalid lyric size rejected");
            Rectangle original = form.LyricsWindow.Bounds;
            bool topmost = form.TopMost;
            Point cursor; GetCursorPos(out cursor);
            try {
                form.TopMost = true;
                form.LyricsWindow.Present(true);
                form.LyricsWindow.Location = form.PointToScreen(new Point(40, 240));
                Point from = form.LyricsWindow.PointToScreen(new Point(form.LyricsWindow.Width / 2, form.LyricsWindow.Height - 20));
                SetCursorPos(from.X, from.Y); await Task.Delay(120);
                Assert(GetAncestor(WindowFromPoint(from), 2) == form.LyricsWindow.Handle, "lyric drag input targets only the isolated overlay");
                Capture(form.LyricsWindow, "lyrics-tools-before-drag.png");
                Rectangle before = form.LyricsWindow.Bounds;
                mouse_event(0x0002, 0, 0, 0, UIntPtr.Zero); await Task.Delay(100);
                for (int step = 1; step <= 5; step++) { SetCursorPos(from.X + step * 10, from.Y + step * 5); await Task.Delay(30); }
                mouse_event(0x0004, 0, 0, 0, UIntPtr.Zero); await Task.Delay(150);
                results["lyricDragMovement"] = new { before = before.ToString(), after = form.LyricsWindow.Bounds.ToString() };
                Assert(Math.Abs(form.LyricsWindow.Left - before.Left) >= 20 || Math.Abs(form.LyricsWindow.Top - before.Top) >= 15, "lyric window drag moves its own surface");
                Capture(form.LyricsWindow, "lyrics-tools.png");
                Point center = form.LyricsWindow.PointToScreen(new Point(form.LyricsWindow.Width / 2, form.LyricsWindow.Height / 2));
                Assert(GetAncestor(WindowFromPoint(center), 2) == form.LyricsWindow.Handle, "unlocked lyric window accepts pointer input");
                Assert(form.ChangeDesktopPreference("lyricsLocked", true), "lock lyric window");
                await Task.Delay(100);
                Assert(GetAncestor(WindowFromPoint(center), 2) == form.Handle, "locked lyric window actually passes pointer input to underlying Music window");
                // The menu remains a recovery path when the overlay is click-through.
                ((ToolStripMenuItem)form.TrayMenu.Items[5]).PerformClick();
                await Until(delegate { return !form.LyricsWindow.Locked; }, 1500);
            } finally { mouse_event(0x0004, 0, 0, 0, UIntPtr.Zero); SetCursorPos(cursor.X, cursor.Y); form.LyricsWindow.Bounds = original; form.TopMost = topmost; }
            await form.Browser.CoreWebView2.ExecuteScriptAsync("document.documentElement.dataset.theme='dark'");
            await Until(delegate { return form.DarkChrome; }, 1500);
            await Task.Delay(160); Capture(form.LyricsWindow, "lyrics-dark.png");
            await form.Media("play");
            form.HideToTray();
            await Until(delegate { return form.LyricsWindow.CurrentLine == "第二句，把日子慢慢唱"; }, 4000);
            Assert(form.LyricsWindow.Visible && !form.Visible, "desktop lyrics continue with main window in tray");
            await form.Media("pause");
            await Until(delegate { return !form.Playing; }, 1500);
            form.RestoreWindow();
            await form.Browser.CoreWebView2.ExecuteScriptAsync("document.documentElement.dataset.theme='light'");
            results["desktopLyrics"] = new { separateNativeWindow = true, focusPreserved = true, syncedSeek = true, pointerDrag = true, realClickThrough = true, trayUnlock = true, hiddenPlayback = true };
        }

        private static async Task PersonalLyrics()
        {
            await ClickWeb("[data-lyric-color='#f4cf7b']");
            await Until(delegate { return form.Preferences.LyricsColor == "#f4cf7b"; }, 1500);
            Assert(form.ChangeDesktopPreference("lyricsMinFont",16) && form.ChangeDesktopPreference("lyricsMaxFont",36), "custom font bounds accepted");
            Assert(!form.ChangeDesktopPreference("lyricsMinFont",40) && !form.ChangeDesktopPreference("lyricsMaxFont",10) && !form.ChangeDesktopPreference("lyricsColor","url(file:///bad)"), "invalid bounds and colors rejected");
            Rectangle original=form.LyricsWindow.Bounds;
            Point cursor;GetCursorPos(out cursor);
            bool pinned=form.TopMost;
            try {
                form.TopMost=true;form.LyricsWindow.Present(true);
                form.LyricsWindow.Location=form.PointToScreen(new Point(30,200));
                Point corner=form.LyricsWindow.PointToScreen(new Point(form.LyricsWindow.Width-4,form.LyricsWindow.Height-4));
                SetCursorPos(corner.X,corner.Y);await Task.Delay(150);
                Assert(GetAncestor(WindowFromPoint(corner),2)==form.LyricsWindow.Handle,"resize pointer belongs to lyric window");
                Size before=form.LyricsWindow.Size;
                mouse_event(0x0002,0,0,0,UIntPtr.Zero);await Task.Delay(150);
                for(int i=1;i<=4;i++){SetCursorPos(corner.X+i*14,corner.Y+i*10);await Task.Delay(40);}
                mouse_event(0x0004,0,0,0,UIntPtr.Zero);await Task.Delay(150);
                results["lyricResize"] = new { before=before.ToString(), after=form.LyricsWindow.Size.ToString() };
                Assert(form.LyricsWindow.Width>=before.Width+28 && form.LyricsWindow.Height>=before.Height+20,"actual corner resize changes lyric bounds");
                form.LyricsWindow.Size=new Size(50,20);
                Assert(form.LyricsWindow.Width>=form.LyricsWindow.MinimumSize.Width && form.LyricsWindow.Height>=form.LyricsWindow.MinimumSize.Height,"minimum readable lyric window enforced");
                form.LyricsWindow.UpdateLyrics(new string('长',200),"这是一行很长的翻译", "synced");
                using(Bitmap bitmap=form.LyricsWindow.RenderBitmap()) {
                    Assert(form.LyricsWindow.RenderedFontSize>=16 && form.LyricsWindow.RenderedFontSize<=36,"autosizing stays inside user font limits");
                    bitmap.Save(Path.Combine(evidenceDirectory,"lyrics-small-alpha.png"),System.Drawing.Imaging.ImageFormat.Png);
                }
                form.LyricsWindow.Bounds=original;
                SetCursorPos(form.Left+20,form.Top+100);await Task.Delay(180);
                using(Bitmap bitmap=form.LyricsWindow.RenderBitmap()) Assert(bitmap.GetPixel(bitmap.Width/2,4).A<=1,"idle backdrop is per-pixel transparent, not whole-window dimming");
                Rectangle previous=form.LyricsWindow.ToolBounds("previoustrack"),next=form.LyricsWindow.ToolBounds("nexttrack");
                Assert(Math.Abs((previous.Left+next.Right)/2-form.LyricsWindow.Width/2)<=2,"transport group centered horizontally");
                Assert(form.ChangeDesktopPreference("lyricsOrientation","vertical"),"manual vertical layout accepted");
                Assert(form.LyricsWindow.Height>form.LyricsWindow.Width,"vertical lyric window is tall and narrow");
                previous=form.LyricsWindow.ToolBounds("previoustrack");next=form.LyricsWindow.ToolBounds("nexttrack");
                Assert(Math.Abs((previous.Top+next.Bottom)/2-form.LyricsWindow.Height/2)<=2,"transport group centered vertically");
                form.LyricsWindow.UpdateLyrics("把日子慢慢唱","在自己的节奏里","synced");
                Assert(form.ChangeDesktopPreference("lyricsSecondaryEnabled",false),"secondary lyric can be disabled");
                using(Bitmap bitmap=form.LyricsWindow.RenderBitmap())Assert(!form.LyricsWindow.RenderedSecondary,"secondary text is not rendered when disabled");
                Assert(form.ChangeDesktopPreference("lyricsSecondaryEnabled",true),"secondary lyric can be restored");
                using(Bitmap bitmap=form.LyricsWindow.RenderBitmap())Assert(form.LyricsWindow.RenderedSecondary,"secondary text returns without reloading the song");
                await Task.Delay(180);Capture(form.LyricsWindow,"lyrics-vertical.png");
                Rectangle area=Screen.FromControl(form.LyricsWindow).WorkingArea;
                form.LyricsWindow.Top=area.Top+4;
                SendMessage(form.LyricsWindow.Handle,0x0231,IntPtr.Zero,IntPtr.Zero);SendMessage(form.LyricsWindow.Handle,0x0232,IntPtr.Zero,IntPtr.Zero);
                Assert(form.LyricsWindow.Orientation=="vertical","top edge no longer changes chosen direction");
                form.ChangeDesktopPreference("lyricsOrientation","horizontal");
                form.LyricsWindow.Left=area.Left+4;
                SendMessage(form.LyricsWindow.Handle,0x0231,IntPtr.Zero,IntPtr.Zero);SendMessage(form.LyricsWindow.Handle,0x0232,IntPtr.Zero,IntPtr.Zero);
                Assert(form.LyricsWindow.Orientation=="horizontal","side edge no longer changes chosen direction");
                form.ChangeDesktopPreference("lyricsOrientation","horizontal");form.LyricsWindow.Bounds=original;
                form.HideToTray();form.ChangeDesktopPreference("lyricsLocked",true);
                ((ToolStripMenuItem)form.TrayMenu.Items[5]).PerformClick();
                await Until(delegate{return !form.LyricsWindow.Locked;},1500);
                Assert(!form.Visible,"unlock from tray does not restore hidden main player");
                form.RestoreWindow();
                form.LyricsWindow.Bounds=original;
                Assert(form.ChangeDesktopPreference("lyricsMaxFont",80) && form.ChangeDesktopPreference("lyricsMinFont",72),"large legal font range accepted");
                Assert(Screen.FromControl(form.LyricsWindow).WorkingArea.Contains(form.LyricsWindow.Bounds),"growing minimum font keeps the expanded window inside the work area");
                form.ChangeDesktopPreference("lyricsMinFont",16);form.ChangeDesktopPreference("lyricsMaxFont",36);form.ChangeDesktopPreference("lyricsSize",30);
                form.ChangeDesktopPreference("lyricsSecondaryEnabled",false);
                results["lyricPersonalization"]=new{perPixelAlpha=true,cornerResize=true,minimumSize=true,ellipsisAndFontBounds=true,vertical=true,edgeRotationDisabled=true,centeredTransport=true,customColor=true,unlockKeepsMainHidden=true};
            } finally {mouse_event(0x0004,0,0,0,UIntPtr.Zero);SetCursorPos(cursor.X,cursor.Y);form.TopMost=pinned;form.LyricsWindow.Bounds=original;}
        }

        private static async Task WindowPreferences()
        {
            AssertDefaultWindow();
            await form.Browser.CoreWebView2.ExecuteScriptAsync("window.qaClickLog=[];document.addEventListener('click',e=>{const b=e.target.closest('button');if(b)qaClickLog.push({text:b.textContent,data:{...b.dataset},trusted:e.isTrusted})},true)");
            double zoom=form.Browser.ZoomFactor;
            results["windowStage"]="click shrink preset";
            await ClickWeb("[data-window-size='less']");
            await Until(delegate{return form.Preferences.WindowScale==.95;},1500);
            results["windowStage"]="preset passed; click interface zoom";
            Assert(form.Bounds==WindowLayout.LaunchBounds(form.Preferences) && form.Browser.ZoomFactor==zoom,"window preset changes physical bounds, not page zoom");
            Size physical=form.Size;
            await ClickWeb("[data-zoom='more']");
            await Until(delegate{return form.Browser.ZoomFactor>zoom;},1500);
            results["windowStage"]="interface passed; reset window";
            Assert(form.Size==physical,"interface scale does not resize native window");
            await ClickWebPointer("[data-window-size='reset']");
            results["windowResetDiagnostic"]=new { request=form.LastPreferenceChange, scale=form.Preferences.WindowScale, zoom=form.Browser.ZoomFactor, clicks=await form.Browser.CoreWebView2.ExecuteScriptAsync("({clicks:qaClickLog,error:document.querySelector('.desktop-settings-error').textContent})") };
            await Until(delegate{return form.Preferences.WindowScale==1;},1500);
            results["windowStage"]="reset passed; save custom dimensions";
            Assert(!form.ChangeDesktopPreference("windowScale",.4),"unsupported window scale rejected");
            form.Bounds=new Rectangle(110,140,1040,760);
            await PaintReady();
            await ClickWebPointer("[data-save-window]");
            results["windowSaveDiagnostic"]=new {request=form.LastPreferenceChange,actual=form.Bounds.ToString(),savedWidth=form.Preferences.SavedWindowWidth,savedHeight=form.Preferences.SavedWindowHeight,clicks=await form.Browser.CoreWebView2.ExecuteScriptAsync("({clicks:qaClickLog,error:document.querySelector('.desktop-settings-error').textContent})")};
            await Until(delegate{return form.Preferences.SavedWindowWidth==1040 && form.Preferences.SavedWindowHeight==760;},1500);
            results["windowStage"]="custom save passed";
            form.Bounds=new Rectangle(210,230,1000,720);
            Assert(form.Preferences.SavedWindowWidth==1040,"later unsaved resizing does not overwrite chosen size");
            results["independentWindowAndInterfaceScale"]=true;results["explicitWindowSave"]=true;
        }

        private static void AssertDefaultWindow()
        {
            Assert(form.WindowState == FormWindowState.Normal, "new launch must ignore the previous maximized state");
            Rectangle area = Screen.PrimaryScreen.WorkingArea;
            int width = Math.Min(area.Width, Math.Min(1920, Math.Max(1120, (int)Math.Round(area.Width * .76))));
            int height = Math.Min(area.Height, Math.Min(1200, Math.Max(780, (int)Math.Round(area.Height * .84))));
            Rectangle expected = new Rectangle(area.X + (area.Width - width) / 2, area.Y + (area.Height - height) / 2, width, height);
            Rectangle actual = form.Bounds;
            results["startupGeometry"] = new { actual = actual.ToString(), expected = expected.ToString() };
            Assert(Math.Abs(actual.X - expected.X) <= 2 && Math.Abs(actual.Y - expected.Y) <= 2 && actual.Width == expected.Width && actual.Height == expected.Height, "new process starts at default size and centered position");
            results["legacyWindowGeometryIgnored"] = true;
        }

        private static async Task Chrome()
        {
            await DragWindow();
            Assert(form.ClientSize.Height == form.Height && form.ClientSize.Width == form.Width, "system caption replaced by full Music surface");
            await UntilScript("document.querySelectorAll('#music-window-controls button').length === 3 && !!document.querySelector('#music-desktop-settings')", 2500);
            results["integratedWindowChrome"] = true;
            Point edge = form.PointToScreen(new Point(2, form.ClientSize.Height / 2));
            IntPtr packed = (IntPtr)((edge.X & 0xffff) | ((edge.Y & 0xffff) << 16));
            Assert(SendMessage(form.Handle, 0x0084, IntPtr.Zero, packed).ToInt32() == 10, "native resize hit zone survives custom frame");
            await ClickWeb("#music-window-controls [data-command='maximize']");
            await Until(delegate { return form.WindowState == FormWindowState.Maximized; }, 2000);
            await UntilScript("document.querySelector('[data-command=maximize]').getAttribute('aria-label') === '还原窗口'", 2000);
            Rectangle client = form.RectangleToScreen(form.ClientRectangle);
            Rectangle area = Screen.FromControl(form).WorkingArea;
            Assert(client.Left >= area.Left - 1 && client.Top >= area.Top - 1 && client.Right <= area.Right + 1 && client.Bottom <= area.Bottom + 1, "maximized content respects work area");
            results["maximizedWorkArea"] = true;
            await ClickWeb("#music-window-controls [data-command='maximize']");
            await Until(delegate { return form.WindowState == FormWindowState.Normal; }, 2000);
            await ClickWeb("#music-window-controls [data-command='minimize']");
            await Until(delegate { return form.WindowState == FormWindowState.Minimized; }, 2000);
            form.RestoreWindow();
            await Task.Delay(150);
            results["captionButtons"] = true;
            await ClickWeb("[data-setting='topmost']");
            await Until(delegate { return form.TopMost && form.Preferences.Topmost; }, 1500);
            await ClickWeb("[data-setting='topmost']");
            await Until(delegate { return !form.TopMost; }, 1500);
            await ClickWeb("[data-zoom='more']");
            await Until(delegate { return Math.Abs(form.Browser.ZoomFactor - 1.15) < .001; }, 1500);
            await ClickWeb("[data-zoom='reset']");
            await Until(delegate { return Math.Abs(form.Browser.ZoomFactor - 1) < .001; }, 1500);
            await ClickWeb("input[name='music-desktop-close'][value='exit']");
            await Until(delegate { return !form.Preferences.CloseToTray; }, 1500);
            await ClickWeb("input[name='music-desktop-close'][value='tray']");
            await Until(delegate { return form.Preferences.CloseToTray; }, 1500);
            Assert(!form.ChangeDesktopPreference("zoom", 8) && form.Preferences.Zoom == 1, "out-of-range desktop scale rejected");
            results["desktopSettingsUseRealControls"] = true;
            await form.Browser.CoreWebView2.ExecuteScriptAsync("window.fixtureDialog=document.createElement('dialog');fixtureDialog.innerHTML='<button>关闭测试对话框</button>';document.body.append(fixtureDialog);fixtureDialog.showModal()");
            await Task.Delay(120);
            await ClickWeb("#music-window-controls [data-command='maximize']");
            await Until(delegate { return form.WindowState == FormWindowState.Maximized; }, 1500);
            await ClickWeb("#music-window-controls [data-command='maximize']");
            await Until(delegate { return form.WindowState == FormWindowState.Normal; }, 1500);
            await form.Browser.CoreWebView2.ExecuteScriptAsync("fixtureDialog.close();fixtureDialog.remove()");
            results["captionAboveWebDialogs"] = true;
            await PaintReady();
            Capture(form, "window-light.png");
            form.Activate();
            SetForegroundWindow(form.Handle);
            form.TrayMenu.Show(form, new Point(Math.Max(0, form.ClientSize.Width - 360), 100));
            await Task.Delay(180);
            Capture(form.TrayMenu, "tray-light.png");
            Assert(form.TrayMenu.DisplayTitle == "Fixture 1", "tray reflects actual media session title");
            MenuKey(Keys.Down);
            await Task.Delay(100);
            Control focused = Control.FromHandle(GetFocus());
            Assert(focused != null && focused.AccessibleName == "播放", "tray keyboard starts on play control");
            MenuKey(Keys.Enter);
            await Until(delegate { return form.Playing; }, 2000);
            Assert(form.TrayMenu.Visible, "transport keeps tray menu open");
            MenuKey(Keys.Right); MenuKey(Keys.Enter);
            await Until(delegate { return form.SongTitle == "Fixture 2"; }, 2000);
            MenuKey(Keys.Left); MenuKey(Keys.Enter);
            await Until(delegate { return !form.Playing; }, 2000);
            MenuKey(Keys.Down);
            await Task.Delay(100);
            Assert(form.TrayMenu.Items[3].Selected, "tray keyboard moves from transport to commands");
            MenuKey(Keys.Escape);
            await Until(delegate { return !form.TrayMenu.Visible; }, 1500);
            await form.Media("previoustrack");
            await Until(delegate { return form.SongTitle == "Fixture 1"; }, 2000);
            results["trayKeyboardAndTransport"] = true;
            await form.Browser.CoreWebView2.ExecuteScriptAsync("document.documentElement.dataset.theme='dark';document.documentElement.dataset.motion='reduced'");
            await Until(delegate { return form.DarkChrome && form.ReducedMotion; }, 1500);
            await PaintReady();
            Capture(form, "window-dark.png");
            form.TrayMenu.Show(form, new Point(Math.Max(0, form.ClientSize.Width - 360), 100));
            await Task.Delay(180);
            Capture(form.TrayMenu, "tray-dark.png");
            form.TrayMenu.Close();
            await form.Browser.CoreWebView2.ExecuteScriptAsync("document.documentElement.dataset.theme='light';document.documentElement.dataset.motion='standard'");
            results["trayThemeAndVisualEvidence"] = true;
        }

        private static async Task DragWindow()
        {
            Rectangle original = form.Bounds;
            bool topmost = form.TopMost;
            Point cursor;
            GetCursorPos(out cursor);
            IntPtr foreground = GetForegroundWindow();
            try
            {
                form.WindowState = FormWindowState.Normal;
                form.TopMost = true; // pointer QA must target our own isolated window
                Rectangle area = Screen.FromControl(form).WorkingArea;
                form.Bounds = new Rectangle(area.X + 160, area.Y + 120, Math.Min(980, area.Width - 320), Math.Min(720, area.Height - 240));
                form.Activate(); SetForegroundWindow(form.Handle);
                await PaintReady();
                Point start = form.PointToScreen(new Point(form.ClientSize.Width / 2, 18));
                SetCursorPos(start.X, start.Y);
                await Task.Delay(80);
                Assert(GetAncestor(WindowFromPoint(start), 2) == form.Handle, "native drag input targets only the isolated Music window");
                Rectangle before = form.Bounds;
                mouse_event(0x0002, 0, 0, 0, UIntPtr.Zero);
                await Task.Delay(80);
                for (int step = 1; step <= 6; step++)
                {
                    SetCursorPos(start.X + step * 12, start.Y + step * 7);
                    await Task.Delay(35);
                }
                mouse_event(0x0004, 0, 0, 0, UIntPtr.Zero);
                await Task.Delay(150);
                int dx = form.Left - before.Left, dy = form.Top - before.Top;
                results["dragMovement"] = new { x = dx, y = dy, before = before.ToString(), after = form.Bounds.ToString(), windowState = form.WindowState.ToString() };
                Assert(dx >= 40 && dy >= 20, "holding and moving the top bar must move the native window");
                results["nativePointerDrag"] = true;
                Dictionary<string, object> resize = new Dictionary<string, object>();
                Rectangle anchor = before;
                foreach (double zoom in new double[] { .85, 1.25 })
                {
                    Assert(form.ChangeDesktopPreference("zoom", zoom), "resize check uses supported UI scale");
                    foreach (string edge in new string[] { "left", "right", "top", "bottom", "top-left", "top-right", "bottom-left", "bottom-right" })
                    {
                        form.Bounds = anchor;
                        await PaintReady();
                        form.Activate(); SetForegroundWindow(form.Handle);
                        bool left = edge.Contains("left"), right = edge.Contains("right"), top = edge.Contains("top"), bottom = edge.Contains("bottom");
                        Point from = form.PointToScreen(new Point(left ? 5 : right ? form.ClientSize.Width - 5 : form.ClientSize.Width / 2, top ? 5 : bottom ? form.ClientSize.Height - 5 : form.ClientSize.Height / 2));
                        SetCursorPos(from.X, from.Y);
                        await Task.Delay(120);
                        Assert(GetAncestor(WindowFromPoint(from), 2) == form.Handle, "resize input targets only isolated Music");
                        mouse_event(0x0002, 0, 0, 0, UIntPtr.Zero);
                        await Task.Delay(160);
                        for (int step = 1; step <= 4; step++)
                        {
                            SetCursorPos(from.X + (left ? -12 : right ? 12 : 0) * step, from.Y + (top ? -9 : bottom ? 9 : 0) * step);
                            await Task.Delay(30);
                        }
                        mouse_event(0x0004, 0, 0, 0, UIntPtr.Zero);
                        await Task.Delay(100);
                        int width = form.Width - anchor.Width, height = form.Height - anchor.Height;
                        resize[zoom.ToString(System.Globalization.CultureInfo.InvariantCulture) + ":" + edge] = new { width = width, height = height };
                        results["nativeResizeMovement"] = resize;
                        Assert((!(left || right) || width >= 24) && (!(top || bottom) || height >= 18), "actual edge/corner resize at " + edge + " zoom " + zoom);
                    }
                }
                Assert(form.ChangeDesktopPreference("zoom", 1), "restore UI scale after native gestures");
                results["nativePointerResizeAllEdges"] = true;
            }
            finally
            {
                mouse_event(0x0004, 0, 0, 0, UIntPtr.Zero);
                form.Bounds = original;
                form.TopMost = topmost;
                SetCursorPos(cursor.X, cursor.Y);
                if (foreground != IntPtr.Zero && foreground != form.Handle) SetForegroundWindow(foreground);
            }
        }

        private static void MenuKey(Keys key)
        {
            IntPtr focus = GetFocus();
            Control control = Control.FromHandle(focus);
            if (control == null || !form.TrayMenu.Contains(control)) focus = form.TrayMenu.Handle;
            PostMessage(focus, 0x0100, (IntPtr)(int)key, (IntPtr)1);
            PostMessage(focus, 0x0101, (IntPtr)(int)key, (IntPtr)0xC0000001);
        }

        private static async Task PaintReady()
        {
            await form.Browser.CoreWebView2.CallDevToolsProtocolMethodAsync("Runtime.evaluate", "{\"expression\":\"new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))\",\"awaitPromise\":true}");
            await Task.Delay(120);
        }

        private static async Task ClickWeb(string selector)
        {
            string expression = "(() => {const e=document.querySelector(" + new JavaScriptSerializer().Serialize(selector) + ");if(!e)throw new Error('missing control');e.scrollIntoView({block:'nearest'});const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()";
            string raw = await form.Browser.CoreWebView2.ExecuteScriptAsync(expression);
            Dictionary<string, object> point = new JavaScriptSerializer().Deserialize<Dictionary<string, object>>(raw);
            double x = Convert.ToDouble(point["x"]), y = Convert.ToDouble(point["y"]);
            foreach (string kind in new string[] { "mousePressed", "mouseReleased" })
                await form.Browser.CoreWebView2.CallDevToolsProtocolMethodAsync("Input.dispatchMouseEvent", new JavaScriptSerializer().Serialize(new { type = kind, x = x, y = y, button = "left", clickCount = 1 }));
            await Task.Delay(75);
        }

        private static async Task ClickWebPointer(string selector)
        {
            Point cursor;GetCursorPos(out cursor);bool pinned=form.TopMost;
            try {
                form.TopMost=true;form.Activate();SetForegroundWindow(form.Handle);
                await Task.Delay(160);
                await form.Browser.CoreWebView2.ExecuteScriptAsync("document.querySelector("+new JavaScriptSerializer().Serialize(selector)+").scrollIntoView({block:'center',behavior:'instant'})");
                await PaintReady();
                string expression="(() => {const e=document.querySelector("+new JavaScriptSerializer().Serialize(selector)+");const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2,w:innerWidth}})()";
                Dictionary<string,object> point=new JavaScriptSerializer().Deserialize<Dictionary<string,object>>(await form.Browser.CoreWebView2.ExecuteScriptAsync(expression));
                double ratio=form.Browser.ClientSize.Width/Convert.ToDouble(point["w"]);
                Point target=form.Browser.PointToScreen(new Point((int)(Convert.ToDouble(point["x"])*ratio),(int)(Convert.ToDouble(point["y"])*ratio)));
                SetCursorPos(target.X,target.Y);await Task.Delay(100);
                Assert(GetAncestor(WindowFromPoint(target),2)==form.Handle,"scaled UI pointer targets isolated Husic window");
                mouse_event(0x0002,0,0,0,UIntPtr.Zero);await Task.Delay(80);mouse_event(0x0004,0,0,0,UIntPtr.Zero);await Task.Delay(150);
            } finally {mouse_event(0x0004,0,0,0,UIntPtr.Zero);SetCursorPos(cursor.X,cursor.Y);form.TopMost=pinned;}
        }

        private static void Capture(Control control, string name)
        {
            using (Bitmap bitmap = new Bitmap(control.Width, control.Height))
            using (Graphics graphics = Graphics.FromImage(bitmap))
            {
                // Layered lyric surfaces cannot reliably WM_PRINT their child
                // controls. Capture only our own visible topmost rectangle.
                if (control is DesktopLyrics) {
                    graphics.CopyFromScreen(control.PointToScreen(Point.Empty), Point.Empty, control.Size);
                    bitmap.Save(Path.Combine(evidenceDirectory, name), System.Drawing.Imaging.ImageFormat.Png);
                    return;
                }
                IntPtr dc = graphics.GetHdc();
                try { PrintWindow(control.Handle, dc, 2); }
                finally { graphics.ReleaseHdc(dc); }
                bitmap.Save(Path.Combine(evidenceDirectory, name), System.Drawing.Imaging.ImageFormat.Png);
            }
        }

        private static async Task Navigate(Action navigate)
        {
            TaskCompletionSource<bool> done = new TaskCompletionSource<bool>();
            EventHandler<CoreWebView2NavigationCompletedEventArgs> handler = delegate { done.TrySetResult(true); };
            form.Browser.CoreWebView2.NavigationCompleted += handler;
            try { navigate(); await Within(done.Task, 10000); await Task.Delay(100); }
            finally { form.Browser.CoreWebView2.NavigationCompleted -= handler; }
        }
        private static async Task Until(Func<bool> predicate, int milliseconds)
        {
            DateTime until = DateTime.UtcNow.AddMilliseconds(milliseconds);
            while (DateTime.UtcNow < until) { if (predicate()) return; await Task.Delay(40); }
            throw new TimeoutException("Native state did not reach the expected value");
        }
        private static async Task UntilScript(string expression, int milliseconds)
        {
            DateTime until = DateTime.UtcNow.AddMilliseconds(milliseconds);
            while (DateTime.UtcNow < until)
            {
                if (await form.Browser.CoreWebView2.ExecuteScriptAsync(expression) == "true") return;
                await Task.Delay(50);
            }
            throw new TimeoutException("Renderer state did not reach the expected value");
        }
        private static async Task Within(Task task, int milliseconds)
        {
            if (await Task.WhenAny(task, Task.Delay(milliseconds)) != task) throw new TimeoutException("Native initialization timed out");
            await task;
        }
        private static async Task<double> Number(string expression)
        {
            return Double.Parse(await form.Browser.CoreWebView2.ExecuteScriptAsync(expression), System.Globalization.CultureInfo.InvariantCulture);
        }
        private static void Assert(bool value, string message) { if (!value) throw new InvalidOperationException(message); }

        private static string Fixture()
        {
            return @"<!doctype html><html><head><meta charset='utf-8'><title>Music isolated native checks</title><link rel='stylesheet' href='/styles.css'><link rel='stylesheet' href='/settings.css'></head><body>
<div class='frame'><aside class='sidebar'><div class='brand'><img class='brand-mark' src='/brand.png'><strong>Music</strong></div><nav class='side-nav'><a>首页</a><a>发现</a><a>排行榜</a><a>音乐库</a></nav><nav class='side-nav side-settings'><a href='/settings'>设置</a></nav></aside><main class='content'><div class='page settings-page'><header class='page-header'><h1>设置</h1></header><div class='settings-intro'><img src='/brand.png' width='52' height='52'><div><strong>让音乐照你的习惯来。</strong><p>隔离的桌面交互检查，不使用真实账号或播放历史。</p></div></div><p class='settings-signature'>Music · hardware</p></div></main></div><script>
const bytes=new Uint8Array(44+8000*2*15),v=new DataView(bytes.buffer);
function word(at,s){for(let i=0;i<s.length;i++)bytes[at+i]=s.charCodeAt(i)}
word(0,'RIFF');v.setUint32(4,bytes.length-8,true);word(8,'WAVE');word(12,'fmt ');v.setUint32(16,16,true);v.setUint16(20,1,true);v.setUint16(22,1,true);v.setUint32(24,8000,true);v.setUint32(28,16000,true);v.setUint16(32,2,true);v.setUint16(34,16,true);word(36,'data');v.setUint32(40,bytes.length-44,true);
window.fixtureAudioCount=1;window.fixtureAudio=new Audio(URL.createObjectURL(new Blob([bytes],{type:'audio/wav'})));fixtureAudio.loop=true;
let index=1;function metadata(){navigator.mediaSession.metadata=new MediaMetadata({title:'Fixture '+index,artist:'Isolated check'});}
async function play(){await fixtureAudio.play();navigator.mediaSession.playbackState='playing'}
function pause(){fixtureAudio.pause();navigator.mediaSession.playbackState='paused'}
navigator.mediaSession.setActionHandler('play',play);navigator.mediaSession.setActionHandler('pause',pause);
navigator.mediaSession.setActionHandler('nexttrack',()=>{index++;metadata()});navigator.mediaSession.setActionHandler('previoustrack',()=>{index--;metadata()});metadata();navigator.mediaSession.playbackState='paused';window.fixtureReady=true;
window.fixtureLyrics=()=>chrome.webview.postMessage({type:'music-lyrics',line:fixtureAudio.currentTime>=2?'第二句，把日子慢慢唱':'第一句，风从身边经过',secondary:'下一句 / 翻译',state:'synced'});fixtureAudio.addEventListener('timeupdate',fixtureLyrics);
</script></body></html>";
        }
    }
}
#endif
