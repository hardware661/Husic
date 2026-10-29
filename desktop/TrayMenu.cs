using System;
using System.ComponentModel;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.IO;
using System.Runtime.InteropServices;
using System.Windows.Forms;

namespace Husic.Desktop
{
    // A single native popup: no extra browser, player, network request or timer.
    // ContextMenuStrip retains outside-click dismissal and the OS menu lifetime.
    internal sealed class TrayMenu : ContextMenuStrip
    {
        private readonly Action<string> onCommand;
        private readonly TrackHeader header;
        private readonly PlaybackRow playback;
        private readonly ContentHost headerHost;
        private readonly ContentHost playbackHost;
        private readonly CommandItem[] commands;
        private readonly ToolStripSeparator firstDivider;
        private readonly ToolStripSeparator lastDivider;
        private Font bodyFont;
        private Font titleFont;
        private Font detailFont;
        private Palette colors = new Palette(false);
        private float scale = 1;
        private bool mediaReady;
        private bool playing;
        private bool dark;
        private bool reducedMotion;
        private bool topmost;
        private bool keyboardNavigation;
        private int navigationIndex = -1;

        [DllImport("user32.dll")] private static extern IntPtr MonitorFromPoint(Point point, uint flags);
        [DllImport("shcore.dll")] private static extern int GetDpiForMonitor(IntPtr monitor, int type, out uint x, out uint y);
        [DllImport("dwmapi.dll")] private static extern int DwmSetWindowAttribute(IntPtr hwnd, int attribute, ref int value, int size);

        internal TrayMenu(Action<string> onCommand)
        {
            if (onCommand == null) throw new ArgumentNullException("onCommand");
            this.onCommand = onCommand;
            AccessibleName = "Husic 播放菜单";
            AccessibleRole = AccessibleRole.MenuPopup;
            ShowImageMargin = false;
            ShowCheckMargin = false;
            ShowItemToolTips = false;
            AutoClose = true;
            DropShadowEnabled = true;
            DoubleBuffered = true;
            Renderer = new MenuRenderer(this);

            header = new TrackHeader(this);
            playback = new PlaybackRow(this);
            headerHost = new ContentHost(header, "当前歌曲");
            playbackHost = new ContentHost(playback, "播放控制");
            firstDivider = new ToolStripSeparator();
            lastDivider = new ToolStripSeparator();
            commands = new[] {
                CreateCommand("打开 Husic", "open", 3),
                CreateCommand("桌面歌词", "lyrics", 4),
                CreateCommand("锁定歌词", "lyrics-lock", 5),
                CreateCommand("窗口置顶", "pin", 6),
                CreateCommand("设置", "settings", 7),
                CreateCommand("退出 Husic", "quit", 8)
            };
            Items.AddRange(new ToolStripItem[] {
                headerHost, playbackHost, firstDivider,
                commands[0], commands[1], commands[2], commands[3], commands[4], lastDivider, commands[5]
            });
            ApplyDpi(96);
            UpdateState("", "", false, false, false, false, false);
        }

        internal string DisplayTitle { get { return header.Title; } }

        internal void UpdateState(string title, string artist, bool ready, bool playing, bool dark, bool reduced, bool topmost)
        {
            bool themeChanged = this.dark != dark;
            mediaReady = ready;
            this.playing = playing;
            this.dark = dark;
            reducedMotion = reduced;
            this.topmost = topmost;
            colors = new Palette(dark);
            BackColor = colors.Background;
            ForeColor = colors.Ink;
            header.BackColor = playback.BackColor = colors.Background;
            header.UpdateTrack(CleanText(title, 180), CleanText(artist, 180), ready, playing);
            playback.UpdatePlayback(ready, playing);
            commands[3].Checked = topmost;
            commands[3].AccessibleDescription = topmost ? "已开启，点击取消置顶" : "未开启，点击保持窗口在最前面";
            if (!ready && keyboardNavigation && navigationIndex < 3 && Visible) FocusIndex(3);
            if (themeChanged && IsHandleCreated) ApplyWindowShape();
            Invalidate(true);
        }

        // The caller retains ownership. The menu owns and releases its copy.
        internal void SetArtwork(Image image) { header.SetArtwork(image); }

        internal void UpdateLyricsState(bool enabled, bool locked)
        {
            commands[1].Checked = enabled;
            commands[1].Text = enabled ? "收起桌面歌词" : "显示桌面歌词";
            commands[1].AccessibleName = commands[1].Text;
            commands[2].Text = locked ? "解锁歌词" : "锁定歌词";
            commands[2].AccessibleName = commands[2].Text;
            // Unlock is available even if lyrics were hidden while locked.
            commands[2].Enabled = enabled || locked;
            Invalidate(true);
        }

        private CommandItem CreateCommand(string text, string command, int index)
        {
            CommandItem item = new CommandItem(text, command);
            item.Click += delegate { Close(ToolStripDropDownCloseReason.ItemClicked); onCommand(command); };
            item.MouseEnter += delegate { navigationIndex = index; keyboardNavigation = false; playback.RefreshButtons(); };
            return item;
        }

        private static string CleanText(string text, int maximum)
        {
            text = (text ?? "").Replace('\r', ' ').Replace('\n', ' ').Replace('\t', ' ').Trim();
            return text.Length > maximum ? text.Substring(0, maximum) : text;
        }

        private int S(float value) { return (int)Math.Round(value * scale); }

        protected override void OnOpening(CancelEventArgs e)
        {
            ApplyDpi(PopupDpi());
            navigationIndex = -1;
            keyboardNavigation = false;
            base.OnOpening(e);
            ApplyWindowShape();
        }

        protected override void OnClosed(ToolStripDropDownClosedEventArgs e)
        {
            keyboardNavigation = false;
            navigationIndex = -1;
            playback.RefreshButtons();
            base.OnClosed(e);
        }

        protected override void OnHandleCreated(EventArgs e)
        {
            base.OnHandleCreated(e);
            ApplyWindowShape();
        }

        protected override void OnSizeChanged(EventArgs e)
        {
            base.OnSizeChanged(e);
            if (IsHandleCreated) ApplyWindowShape();
        }

        protected override void WndProc(ref Message m)
        {
            base.WndProc(ref m);
            if (m.Msg == 0x02E0 && header != null) ApplyDpi((int)(m.WParam.ToInt64() & 0xffff));
        }

        private static int PopupDpi()
        {
            try
            {
                uint x, y;
                if (GetDpiForMonitor(MonitorFromPoint(Cursor.Position, 2), 0, out x, out y) == 0 && x > 0) return (int)x;
            }
            catch (DllNotFoundException) { }
            catch (EntryPointNotFoundException) { }
            using (Graphics graphics = Graphics.FromHwnd(IntPtr.Zero)) return (int)graphics.DpiX;
        }

        private void ApplyDpi(int dpi)
        {
            float next = Math.Max(72, Math.Min(384, dpi)) / 96f;
            if (bodyFont != null && Math.Abs(next - scale) < 0.001f) return;
            scale = next;
            Font oldBody = bodyFont, oldTitle = titleFont, oldDetail = detailFont;
            // Pixel fonts make per-monitor scaling explicit in the Framework host.
            bodyFont = new Font("Microsoft YaHei UI", 13f * scale, FontStyle.Regular, GraphicsUnit.Pixel);
            titleFont = new Font("Microsoft YaHei UI", 14.667f * scale, FontStyle.Bold, GraphicsUnit.Pixel);
            detailFont = new Font("Microsoft YaHei UI", 12.667f * scale, FontStyle.Regular, GraphicsUnit.Pixel);
            SuspendLayout();
            Font = bodyFont;
            Padding = new Padding(S(8), S(6), S(8), S(6));
            MinimumSize = new Size(S(304), 0);
            MaximumSize = new Size(S(304), 0);
            headerHost.Size = header.Size = new Size(S(288), S(68));
            playbackHost.Size = playback.Size = new Size(S(288), S(56));
            playback.Arrange();
            foreach (CommandItem item in commands) item.Size = new Size(S(288), S(44));
            foreach (ToolStripSeparator divider in new[] { firstDivider, lastDivider })
            {
                divider.AutoSize = false;
                divider.Margin = Padding.Empty;
                divider.Size = new Size(S(288), S(9));
            }
            ResumeLayout(true);
            if (oldBody != null) oldBody.Dispose();
            if (oldTitle != null) oldTitle.Dispose();
            if (oldDetail != null) oldDetail.Dispose();
            ApplyWindowShape();
        }

        private void ApplyWindowShape()
        {
            if (!IsHandleCreated || Width < 2 || Height < 2) return;
            bool nativeRound = false;
            try
            {
                int corner = 2, useDark = dark ? 1 : 0;
                int border = colors.Border.R | (colors.Border.G << 8) | (colors.Border.B << 16);
                nativeRound = DwmSetWindowAttribute(Handle, 33, ref corner, sizeof(int)) == 0;
                DwmSetWindowAttribute(Handle, 20, ref useDark, sizeof(int));
                DwmSetWindowAttribute(Handle, 34, ref border, sizeof(int));
            }
            catch (DllNotFoundException) { }
            catch (EntryPointNotFoundException) { }
            Region previous = Region;
            if (nativeRound) Region = null;
            else using (GraphicsPath path = Drawing.Round(new RectangleF(0, 0, Width, Height), S(8))) Region = new Region(path);
            if (previous != null && !Object.ReferenceEquals(previous, Region)) previous.Dispose();
        }

        private void SendMedia(int index)
        {
            if (!mediaReady) return;
            onCommand(index == 0 ? "previoustrack" : index == 2 ? "nexttrack" : "toggle");
        }

        // Hosted buttons and native menu rows share one keyboard order. In
        // particular Up/Down never strand focus inside the three-button row.
        private bool MenuKey(Keys keyData)
        {
            if ((keyData & (Keys.Control | Keys.Alt)) != 0) return false;
            Keys key = keyData & Keys.KeyCode;
            int current = navigationIndex;
            if (key == Keys.Escape) { Close(ToolStripDropDownCloseReason.Keyboard); return true; }
            int last = commands.Length + 2;
            if (key == Keys.Down) FocusIndex(current < 0 ? (mediaReady ? 1 : 3) : current < 3 ? 3 : current == last ? (mediaReady ? 1 : 3) : current + 1);
            else if (key == Keys.Up) FocusIndex(current < 0 || current < 3 ? last : current == 3 ? (mediaReady ? 1 : last) : current - 1, -1);
            else if (key == Keys.Left || key == Keys.Right)
            {
                if (mediaReady && current < 3) FocusIndex(current < 0 ? 1 : (current + (key == Keys.Left ? 2 : 1)) % 3);
            }
            else if (key == Keys.Tab)
            {
                int first = mediaReady ? 0 : 3;
                int next = current < first ? first : current + ((keyData & Keys.Shift) == 0 ? 1 : -1);
                FocusIndex(next > last ? first : next < first ? last : next, (keyData & Keys.Shift) == 0 ? 1 : -1);
            }
            else if (key == Keys.Home) FocusIndex(mediaReady ? 1 : 3);
            else if (key == Keys.End) FocusIndex(last);
            else if (key == Keys.Enter || key == Keys.Space)
            {
                keyboardNavigation = true;
                if (current < 0) FocusIndex(mediaReady ? 1 : 3);
                playback.RefreshButtons();
                if (navigationIndex < 3) SendMedia(navigationIndex);
                else commands[navigationIndex - 3].PerformClick();
            }
            else return false;
            return true;
        }

        private void FocusIndex(int index, int direction = 1)
        {
            if (!mediaReady && index < 3) index = 3;
            while (index >= 3 && !commands[index - 3].Enabled) {
                index += direction;
                if (index > commands.Length + 2) index = mediaReady ? 1 : 3;
                if (index < 3 && !mediaReady) index = commands.Length + 2;
            }
            navigationIndex = index;
            keyboardNavigation = true;
            if (index < 3) { playbackHost.Select(); playback.FocusButton(index); }
            else { Focus(); commands[index - 3].Select(); }
            playback.RefreshButtons();
        }

        protected override bool ProcessCmdKey(ref Message m, Keys keyData)
        {
            return MenuKey(keyData) || base.ProcessCmdKey(ref m, keyData);
        }

        protected override bool ProcessDialogKey(Keys keyData)
        {
            return MenuKey(keyData) || base.ProcessDialogKey(keyData);
        }

        protected override void Dispose(bool disposing)
        {
            base.Dispose(disposing);
            if (!disposing) return;
            if (bodyFont != null) { bodyFont.Dispose(); bodyFont = null; }
            if (titleFont != null) { titleFont.Dispose(); titleFont = null; }
            if (detailFont != null) { detailFont.Dispose(); detailFont = null; }
        }

        private sealed class Palette
        {
            internal readonly Color Background, Ink, Secondary, Border, Hover, Pressed, Disabled, SwitchTrack;
            internal Palette(bool dark)
            {
                Background = dark ? Color.FromArgb(36, 36, 36) : Color.FromArgb(250, 250, 250);
                Ink = dark ? Color.FromArgb(245, 245, 247) : Color.FromArgb(29, 29, 31);
                Secondary = dark ? Color.FromArgb(161, 161, 166) : Color.FromArgb(110, 110, 115);
                Border = dark ? Color.FromArgb(58, 58, 60) : Color.FromArgb(222, 222, 224);
                Hover = dark ? Color.FromArgb(54, 54, 56) : Color.FromArgb(237, 237, 238);
                Pressed = dark ? Color.FromArgb(65, 65, 67) : Color.FromArgb(226, 226, 228);
                Disabled = dark ? Color.FromArgb(105, 105, 110) : Color.FromArgb(166, 166, 171);
                SwitchTrack = dark ? Color.FromArgb(76, 76, 79) : Color.FromArgb(211, 211, 214);
            }
        }

        private sealed class ContentHost : ToolStripControlHost
        {
            internal ContentHost(Control content, string accessibleName) : base(content)
            {
                AutoSize = false;
                Margin = Padding.Empty;
                Padding = Padding.Empty;
                AccessibleName = accessibleName;
                AccessibleRole = AccessibleRole.Grouping;
            }
            // Header/empty playback-area clicks do not dismiss the popup.
            protected override void OnClick(EventArgs e) { }
        }

        private sealed class CommandItem : ToolStripMenuItem
        {
            internal readonly string Command;
            internal CommandItem(string text, string command) : base(text)
            {
                Command = command;
                AutoSize = false;
                Margin = Padding.Empty;
                Padding = Padding.Empty;
                AccessibleName = text;
                AccessibleRole = AccessibleRole.MenuItem;
            }
        }

        private sealed class TrackHeader : Control
        {
            private readonly TrayMenu menu;
            private readonly Image brand;
            private Image artwork;
            private string detail = "";
            internal string Title { get; private set; }
            internal TrackHeader(TrayMenu menu)
            {
                this.menu = menu;
                SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer, true);
                SetStyle(ControlStyles.Selectable, false);
                TabStop = false;
                AccessibleRole = AccessibleRole.StaticText;
                using (MemoryStream stream = new MemoryStream(Resources.Bytes("Husic.Brand.png")))
                using (Image source = Image.FromStream(stream)) brand = new Bitmap(source);
            }
            internal void UpdateTrack(string title, string artist, bool ready, bool playing)
            {
                Title = String.IsNullOrEmpty(title) ? "Husic" : title;
                detail = !String.IsNullOrEmpty(artist) ? artist : String.IsNullOrEmpty(title) ? "选一首喜欢的歌" : !ready ? "准备播放" : playing ? "正在播放" : "已暂停";
                AccessibleName = Title + "，" + detail;
                Invalidate();
            }
            internal void SetArtwork(Image image)
            {
                Image next = image == null ? null : new Bitmap(image);
                Image previous = artwork;
                artwork = next;
                if (previous != null) previous.Dispose();
                Invalidate();
            }
            protected override void OnPaint(PaintEventArgs e)
            {
                Graphics graphics = e.Graphics;
                graphics.SmoothingMode = SmoothingMode.AntiAlias;
                Rectangle art = new Rectangle(menu.S(10), menu.S(10), menu.S(48), menu.S(48));
                Image source = artwork ?? brand;
                GraphicsState saved = graphics.Save();
                using (GraphicsPath clip = Drawing.Round(art, menu.S(9))) graphics.SetClip(clip, CombineMode.Intersect);
                graphics.InterpolationMode = InterpolationMode.HighQualityBicubic;
                float edge = Math.Min(source.Width, source.Height);
                graphics.DrawImage(source, art, (source.Width - edge) / 2, (source.Height - edge) / 2, edge, edge, GraphicsUnit.Pixel);
                graphics.Restore(saved);
                TextFormatFlags flags = TextFormatFlags.NoPrefix | TextFormatFlags.NoPadding | TextFormatFlags.SingleLine | TextFormatFlags.EndEllipsis | TextFormatFlags.VerticalCenter | TextFormatFlags.PreserveGraphicsClipping;
                TextRenderer.DrawText(graphics, Title, menu.titleFont, new Rectangle(menu.S(72), menu.S(12), Width - menu.S(84), menu.S(24)), menu.colors.Ink, flags);
                TextRenderer.DrawText(graphics, detail, menu.detailFont, new Rectangle(menu.S(72), menu.S(37), Width - menu.S(84), menu.S(20)), menu.colors.Secondary, flags);
            }
            protected override void Dispose(bool disposing)
            {
                if (disposing) { brand.Dispose(); if (artwork != null) { artwork.Dispose(); artwork = null; } }
                base.Dispose(disposing);
            }
        }

        private sealed class PlaybackRow : Panel
        {
            private readonly TrayMenu menu;
            private readonly MediaButton[] buttons;
            private readonly ToolTip tips = new ToolTip { InitialDelay = 450, ReshowDelay = 80, AutoPopDelay = 3000 };
            internal PlaybackRow(TrayMenu menu)
            {
                this.menu = menu;
                AccessibleName = "播放控制";
                AccessibleRole = AccessibleRole.Grouping;
                SetStyle(ControlStyles.Selectable, true);
                TabStop = true;
                buttons = new[] { new MediaButton(menu, 0), new MediaButton(menu, 1), new MediaButton(menu, 2) };
                Controls.AddRange(buttons);
                tips.SetToolTip(buttons[0], "上一首");
                tips.SetToolTip(buttons[2], "下一首");
            }
            internal void Arrange()
            {
                for (int i = 0; i < buttons.Length; i++) buttons[i].Bounds = new Rectangle(menu.S(60 + i * 60), menu.S(4), menu.S(48), menu.S(48));
            }
            internal void UpdatePlayback(bool ready, bool playing)
            {
                foreach (MediaButton button in buttons) button.Enabled = ready;
                buttons[1].AccessibleName = playing ? "暂停" : "播放";
                tips.SetToolTip(buttons[1], playing ? "暂停" : "播放");
                RefreshButtons();
            }
            internal void FocusButton(int index) { buttons[index].Focus(); }
            internal void RefreshButtons() { foreach (MediaButton button in buttons) button.Invalidate(); }
            protected override bool ProcessDialogKey(Keys keyData) { return menu.MenuKey(keyData) || base.ProcessDialogKey(keyData); }
            protected override void Dispose(bool disposing) { if (disposing) tips.Dispose(); base.Dispose(disposing); }
        }

        private sealed class MediaButton : Button
        {
            private readonly TrayMenu menu;
            private readonly int index;
            private bool hover;
            private bool pressed;
            internal MediaButton(TrayMenu menu, int index)
            {
                this.menu = menu;
                this.index = index;
                FlatStyle = FlatStyle.Flat;
                FlatAppearance.BorderSize = 0;
                UseVisualStyleBackColor = false;
                AccessibleRole = AccessibleRole.PushButton;
                AccessibleName = index == 0 ? "上一首" : index == 2 ? "下一首" : "播放";
                TabIndex = index;
                TabStop = true;
                SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer, true);
                Click += delegate { menu.SendMedia(index); };
            }
            protected override void OnMouseEnter(EventArgs e)
            {
                hover = true;
                menu.navigationIndex = index;
                menu.keyboardNavigation = false;
                menu.playback.RefreshButtons();
                base.OnMouseEnter(e);
            }
            protected override void OnMouseLeave(EventArgs e) { hover = false; Invalidate(); base.OnMouseLeave(e); }
            protected override void OnMouseDown(MouseEventArgs e) { if (e.Button == MouseButtons.Left) pressed = true; Invalidate(); base.OnMouseDown(e); }
            protected override void OnMouseUp(MouseEventArgs e) { pressed = false; Invalidate(); base.OnMouseUp(e); }
            protected override void OnMouseCaptureChanged(EventArgs e) { if (!Capture) pressed = false; Invalidate(); base.OnMouseCaptureChanged(e); }
            protected override void OnGotFocus(EventArgs e) { Invalidate(); base.OnGotFocus(e); }
            protected override void OnLostFocus(EventArgs e) { pressed = false; Invalidate(); base.OnLostFocus(e); }
            protected override bool ProcessDialogKey(Keys keyData) { return menu.MenuKey(keyData) || base.ProcessDialogKey(keyData); }
            protected override void OnKeyDown(KeyEventArgs e)
            {
                if (menu.MenuKey(e.KeyData)) { e.Handled = true; e.SuppressKeyPress = true; return; }
                base.OnKeyDown(e);
            }
            protected override void OnKeyUp(KeyEventArgs e)
            {
                if (e.KeyCode == Keys.Space || e.KeyCode == Keys.Enter) { e.Handled = true; e.SuppressKeyPress = true; return; }
                base.OnKeyUp(e);
            }
            protected override void OnPaint(PaintEventArgs e)
            {
                Graphics graphics = e.Graphics;
                graphics.Clear(menu.colors.Background);
                graphics.SmoothingMode = SmoothingMode.AntiAlias;
                RectangleF body = new RectangleF(menu.S(2), menu.S(2), Width - menu.S(4), Height - menu.S(4));
                bool down = pressed && (hover || menu.keyboardNavigation);
                if (down && !menu.reducedMotion && !menu.keyboardNavigation) body.Inflate(-menu.S(1), -menu.S(1));
                Color ink = Enabled ? menu.colors.Ink : menu.colors.Disabled;
                if (index == 1)
                {
                    Color fill = !Enabled ? menu.colors.SwitchTrack : down ? menu.colors.Secondary : hover ? (menu.dark ? Color.White : Color.FromArgb(48, 48, 50)) : menu.colors.Ink;
                    Drawing.FillRound(graphics, body, body.Height / 2, fill);
                    ink = Enabled ? menu.colors.Background : menu.colors.Secondary;
                }
                else if (Enabled && (hover || down)) Drawing.FillRound(graphics, body, menu.S(12), down ? menu.colors.Pressed : menu.colors.Hover);
                Drawing.Glyph(graphics, index == 0 ? "previoustrack" : index == 2 ? "nexttrack" : menu.playing ? "pause" : "play", new RectangleF((Width - menu.S(24)) / 2f, (Height - menu.S(24)) / 2f, menu.S(24), menu.S(24)), ink);
                if (Focused && menu.keyboardNavigation && Enabled)
                {
                    using (GraphicsPath path = Drawing.Round(new RectangleF(1, 1, Width - 2, Height - 2), index == 1 ? Height / 2f : menu.S(14)))
                    using (Pen pen = new Pen(menu.colors.Ink, Math.Max(1, menu.scale))) graphics.DrawPath(pen, path);
                }
            }
        }

        private sealed class MenuRenderer : ToolStripRenderer
        {
            private readonly TrayMenu menu;
            internal MenuRenderer(TrayMenu menu) { this.menu = menu; }
            protected override void OnRenderToolStripBackground(ToolStripRenderEventArgs e) { e.Graphics.Clear(menu.colors.Background); }
            protected override void OnRenderToolStripBorder(ToolStripRenderEventArgs e)
            {
                e.Graphics.SmoothingMode = SmoothingMode.AntiAlias;
                using (GraphicsPath path = Drawing.Round(new RectangleF(0.5f, 0.5f, e.ToolStrip.Width - 1, e.ToolStrip.Height - 1), menu.S(8)))
                using (Pen pen = new Pen(menu.colors.Border, 1)) e.Graphics.DrawPath(pen, path);
            }
            protected override void OnRenderImageMargin(ToolStripRenderEventArgs e) { }
            protected override void OnRenderItemCheck(ToolStripItemImageRenderEventArgs e) { }
            protected override void OnRenderMenuItemBackground(ToolStripItemRenderEventArgs e)
            {
                CommandItem item = e.Item as CommandItem;
                if (item == null) return;
                e.Graphics.SmoothingMode = SmoothingMode.AntiAlias;
                if (item.Enabled && item.Selected) Drawing.FillRound(e.Graphics, new RectangleF(0, menu.S(2), item.Width, item.Height - menu.S(4)), menu.S(8), item.Pressed ? menu.colors.Pressed : menu.colors.Hover);
                Drawing.Glyph(e.Graphics, item.Command, new RectangleF(menu.S(12), (item.Height - menu.S(20)) / 2f, menu.S(20), menu.S(20)), menu.colors.Secondary);
                if (item.Command == "pin" || item.Command == "lyrics")
                {
                    bool on = item.Checked;
                    RectangleF track = new RectangleF(item.Width - menu.S(40), (item.Height - menu.S(16)) / 2f, menu.S(28), menu.S(16));
                    Drawing.FillRound(e.Graphics, track, track.Height / 2, on ? menu.colors.Ink : menu.colors.SwitchTrack);
                    RectangleF knob = new RectangleF(track.X + menu.S(on ? 14 : 2), track.Y + menu.S(2), menu.S(12), menu.S(12));
                    Drawing.FillRound(e.Graphics, knob, knob.Height / 2, menu.colors.Background);
                }
            }
            protected override void OnRenderItemText(ToolStripItemTextRenderEventArgs e)
            {
                CommandItem item = e.Item as CommandItem;
                if (item == null) { base.OnRenderItemText(e); return; }
                Rectangle bounds = new Rectangle(menu.S(44), 0, item.Width - menu.S(item.Command == "pin" || item.Command == "lyrics" ? 94 : 56), item.Height);
                TextRenderer.DrawText(e.Graphics, item.Text, menu.bodyFont, bounds, item.Enabled ? menu.colors.Ink : menu.colors.Disabled, TextFormatFlags.NoPrefix | TextFormatFlags.NoPadding | TextFormatFlags.SingleLine | TextFormatFlags.VerticalCenter | TextFormatFlags.EndEllipsis);
            }
            protected override void OnRenderSeparator(ToolStripSeparatorRenderEventArgs e)
            {
                using (Pen pen = new Pen(menu.colors.Border, 1)) e.Graphics.DrawLine(pen, menu.S(12), e.Item.Height / 2, e.Item.Width - menu.S(12), e.Item.Height / 2);
            }
        }

        private static class Drawing
        {
            internal static GraphicsPath Round(RectangleF rectangle, float radius)
            {
                GraphicsPath path = new GraphicsPath();
                float diameter = Math.Min(radius * 2, Math.Min(rectangle.Width, rectangle.Height));
                if (diameter <= 0) { path.AddRectangle(rectangle); return path; }
                path.AddArc(rectangle.X, rectangle.Y, diameter, diameter, 180, 90);
                path.AddArc(rectangle.Right - diameter, rectangle.Y, diameter, diameter, 270, 90);
                path.AddArc(rectangle.Right - diameter, rectangle.Bottom - diameter, diameter, diameter, 0, 90);
                path.AddArc(rectangle.X, rectangle.Bottom - diameter, diameter, diameter, 90, 90);
                path.CloseFigure();
                return path;
            }
            internal static void FillRound(Graphics graphics, RectangleF rectangle, float radius, Color color)
            {
                using (GraphicsPath path = Round(rectangle, radius))
                using (Brush brush = new SolidBrush(color)) graphics.FillPath(brush, path);
            }
            internal static void Glyph(Graphics graphics, string name, RectangleF bounds, Color color)
            {
                GraphicsState state = graphics.Save();
                graphics.TranslateTransform(bounds.X, bounds.Y);
                graphics.ScaleTransform(bounds.Width / 24f, bounds.Height / 24f);
                graphics.SmoothingMode = SmoothingMode.AntiAlias;
                using (Pen pen = new Pen(color, 1.65f))
                using (Brush brush = new SolidBrush(color))
                {
                    pen.StartCap = pen.EndCap = LineCap.Round;
                    pen.LineJoin = LineJoin.Round;
                    if (name == "play") graphics.FillPolygon(brush, new[] { new PointF(9, 5), new PointF(19, 12), new PointF(9, 19) });
                    else if (name == "pause") { FillRound(graphics, new RectangleF(7.5f, 5.5f, 3.2f, 13), 1, color); FillRound(graphics, new RectangleF(13.3f, 5.5f, 3.2f, 13), 1, color); }
                    else if (name == "previoustrack") { graphics.DrawLine(pen, 6, 6, 6, 18); graphics.FillPolygon(brush, new[] { new PointF(17, 5.5f), new PointF(7, 12), new PointF(17, 18.5f) }); }
                    else if (name == "nexttrack") { graphics.DrawLine(pen, 18, 6, 18, 18); graphics.FillPolygon(brush, new[] { new PointF(7, 5.5f), new PointF(17, 12), new PointF(7, 18.5f) }); }
                    else if (name == "open")
                    {
                        using (GraphicsPath window = Round(new RectangleF(3, 4, 18, 16), 2.5f)) graphics.DrawPath(pen, window);
                        graphics.DrawLine(pen, 3, 9, 21, 9);
                        graphics.DrawLine(pen, 8, 9, 8, 20);
                    }
                    else if (name == "settings")
                    {
                        PointF[] points = new PointF[32];
                        for (int i = 0; i < points.Length; i++)
                        {
                            double angle = (i - 0.5) * Math.PI / 16;
                            double radius = i % 4 == 0 || i % 4 == 3 ? 9.2 : 7.3;
                            points[i] = new PointF(12 + (float)(Math.Cos(angle) * radius), 12 + (float)(Math.Sin(angle) * radius));
                        }
                        graphics.DrawPolygon(pen, points);
                        graphics.DrawEllipse(pen, 8.7f, 8.7f, 6.6f, 6.6f);
                    }
                    else if (name == "pin")
                    {
                        graphics.DrawLine(pen, 8, 3, 16, 3);
                        graphics.DrawLines(pen, new[] { new PointF(9, 3), new PointF(9, 9), new PointF(5, 14), new PointF(19, 14), new PointF(15, 9), new PointF(15, 3) });
                        graphics.DrawLine(pen, 12, 14, 12, 21);
                    }
                    else if (name == "lyrics")
                    {
                        using (GraphicsPath box = Round(new RectangleF(3, 5, 18, 14), 3)) graphics.DrawPath(pen, box);
                        graphics.DrawLine(pen, 7, 10, 17, 10); graphics.DrawLine(pen, 7, 14, 14, 14);
                    }
                    else if (name == "lyrics-lock")
                    {
                        graphics.DrawArc(pen, 8, 2, 8, 14, 180, 180);
                        using (GraphicsPath box = Round(new RectangleF(5, 10, 14, 11), 2)) graphics.DrawPath(pen, box);
                        graphics.DrawLine(pen, 12, 14, 12, 17);
                    }
                    else if (name == "quit") { graphics.DrawArc(pen, 4.5f, 4.5f, 15, 15, -45, 270); graphics.DrawLine(pen, 12, 2.5f, 12, 11); }
                }
                graphics.Restore(state);
            }
        }
    }
}
