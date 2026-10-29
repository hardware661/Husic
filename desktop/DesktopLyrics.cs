using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Drawing.Imaging;
using System.Globalization;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Windows.Forms;

namespace Husic.Desktop
{
    internal sealed class DesktopLyrics : Form
    {
        private readonly Action<string> command;
        private readonly Timer pulse = new Timer { Interval = 60 };
        private readonly ToolTip tips = new ToolTip { InitialDelay = 500, ReshowDelay = 80 };
        private readonly string[] actions = { "previoustrack", "toggle", "nexttrack", "lyrics-smaller", "lyrics-larger", "lyrics-lock", "open", "lyrics" };
        private readonly string[] labels = { "上一首", "播放 / 暂停", "下一首", "缩小歌词", "放大歌词", "锁定并穿透鼠标（托盘可解锁）", "回到 Husic", "收起桌面歌词" };
        private readonly Rectangle[] toolBounds = new Rectangle[8];
        private bool locked, reduced, playing, tools, positioned, painting;
        private int lyricSize = 28, minFont = 18, maxFont = 42, backdrop, pressed = -1, hover = -1;
        private string orientation = "horizontal", line = "选一首喜欢的歌", secondary = "", state = "idle";
        private Color lyricColor = Color.White, secondaryColor = Color.FromArgb(216, 216, 226);
        private float scale = 1;

        private DateTime changed = DateTime.UtcNow;
        internal string CurrentLine { get { return line; } }
        internal string SecondaryLine { get { return secondary; } }
        internal string LyricState { get { return state; } }
        internal bool Locked { get { return locked; } }
        internal string Orientation { get { return orientation; } }
        internal float RenderedFontSize { get; private set; }
        internal bool RenderedSecondary { get; private set; }
        private bool secondaryEnabled = true;
        internal int BackdropOpacity { get { return backdrop; } }
        internal Rectangle ToolBounds(string action) { int i = Array.IndexOf(actions, action); return i < 0 ? Rectangle.Empty : toolBounds[i]; }

        [DllImport("user32.dll")] private static extern int GetWindowLong(IntPtr hwnd, int index);
        [DllImport("user32.dll")] private static extern int SetWindowLong(IntPtr hwnd, int index, int value);
        [DllImport("user32.dll")] private static extern bool ReleaseCapture();
        [DllImport("user32.dll")] private static extern IntPtr SendMessage(IntPtr hwnd, int message, IntPtr wParam, IntPtr lParam);
        [DllImport("user32.dll")] private static extern uint GetDpiForWindow(IntPtr hwnd);
        [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
        [DllImport("user32.dll")] private static extern bool SetForegroundWindow(IntPtr hwnd);
        [DllImport("user32.dll")] private static extern bool SetWindowPos(IntPtr hwnd, IntPtr after, int x, int y, int width, int height, uint flags);

        internal DesktopLyrics(Action<string> command)
        {
            this.command = command;
            FormBorderStyle = FormBorderStyle.None; StartPosition = FormStartPosition.Manual;
            ShowInTaskbar = false; TopMost = true; AutoScaleMode = AutoScaleMode.None;
            Text = "Husic 桌面歌词"; AccessibleName = Text;
            SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint, true);
            pulse.Tick += delegate {
                bool next = !locked && Bounds.Contains(Cursor.Position);
                if (tools != next) { tools = next; if (!tools) { hover = -1; pressed = -1; } RenderSurface(); }
                else if (!reduced && (DateTime.UtcNow - changed).TotalMilliseconds < 180) RenderSurface();
            };
            VisibleChanged += delegate { pulse.Enabled = Visible; if (Visible) RenderSurface(); };
        }
        protected override bool ShowWithoutActivation { get { return true; } }
        protected override CreateParams CreateParams {
            get { CreateParams p = base.CreateParams; p.ExStyle |= 0x08000000 | 0x00000080 | 0x00080000; if (locked) p.ExStyle |= 0x20; p.Style |= 0x00040000; return p; }
        }
        protected override void OnHandleCreated(EventArgs e) { base.OnHandleCreated(e); UpdateDpi(); Constrain(); }
        private void UpdateDpi() {
            try { scale = Math.Max(1, GetDpiForWindow(Handle) / 96f); }
            catch (EntryPointNotFoundException) { using (Graphics g = CreateGraphics()) scale = g.DpiX / 96f; }
        }
        private int S(float v) { return (int)Math.Round(v * scale); }
        private void Constrain() {
            Rectangle area = Screen.FromControl(this).WorkingArea;
            MinimumSize = orientation == "vertical" ? new Size(S(Math.Max(100,minFont*2.4f+20)), S(340)) : new Size(S(380), S(Math.Max(96,minFont*1.85f+52)));
            MaximumSize = area.Size;
        }
        protected override void WndProc(ref Message m)
        {
            if (m.Msg == 0x0083) { m.Result = IntPtr.Zero; return; }
            if (m.Msg == 0x0021) { m.Result = (IntPtr)3; return; }
            if (m.Msg == 0x0084 && !locked) {
                long packed = m.LParam.ToInt64(); Point p = PointToClient(new Point((short)(packed & 0xffff), (short)((packed >> 16) & 0xffff)));
                int edge = S(7); bool left = p.X < edge, right = p.X >= Width - edge, top = p.Y < edge, bottom = p.Y >= Height - edge;
                m.Result = (IntPtr)(top && left ? 13 : top && right ? 14 : bottom && left ? 16 : bottom && right ? 17 : left ? 10 : right ? 11 : top ? 12 : bottom ? 15 : 1); return;
            }
            base.WndProc(ref m);
            if (m.Msg == 0x0232) {
                RenderSurface();
            }
            if (m.Msg == 0x02E0) { UpdateDpi(); Constrain(); RenderSurface(); }
        }
        protected override void OnResize(EventArgs e) { base.OnResize(e); Arrange(); RenderSurface(); }
        protected override void OnPaint(PaintEventArgs e) { RenderSurface(); }
        protected override void OnFormClosing(FormClosingEventArgs e) { if (e.CloseReason == CloseReason.UserClosing) { e.Cancel = true; command("lyrics"); } base.OnFormClosing(e); }
        protected override void OnMouseMove(MouseEventArgs e) {
            base.OnMouseMove(e); int next = HitTool(e.Location);
            Cursor = next >= 0 ? Cursors.Hand : Cursors.SizeAll;
            if (next != hover) { hover = next; tips.SetToolTip(this, next < 0 ? "拖动移动，边角调整大小" : labels[next]); RenderSurface(); }
        }
        protected override void OnMouseDown(MouseEventArgs e) {
            base.OnMouseDown(e); if (e.Button != MouseButtons.Left || locked) return;
            pressed = HitTool(e.Location);
            if (pressed >= 0) { Capture = true; RenderSurface(); return; }
            Point p = Cursor.Position; ReleaseCapture();
            SendMessage(Handle, 0x00A1, (IntPtr)2, (IntPtr)((p.X & 0xffff) | ((p.Y & 0xffff) << 16)));
        }
        protected override void OnMouseUp(MouseEventArgs e) {
            base.OnMouseUp(e); int was = pressed; pressed = -1; Capture = false;
            if (e.Button == MouseButtons.Left && was >= 0 && was == HitTool(e.Location)) command(actions[was]);
            RenderSurface();
        }
        private int HitTool(Point p) { if (!tools || locked) return -1; for (int i=0;i<toolBounds.Length;i++) if (toolBounds[i].Contains(p)) return i; return -1; }
        internal void Apply(DesktopPreferences p, bool reducedMotion, bool isPlaying) {
            IntPtr foreground = GetForegroundWindow();
            locked = p.LyricsLocked; reduced = reducedMotion; playing = isPlaying;
            secondaryEnabled = p.LyricsSecondaryEnabled;
            lyricSize = p.LyricsSize; minFont = p.LyricsMinFont; maxFont = p.LyricsMaxFont; backdrop = p.LyricsBackdrop;
            lyricColor = ColorTranslator.FromHtml(p.LyricsColor); secondaryColor = ColorTranslator.FromHtml(p.LyricsSecondaryColor);
            if (p.LyricsOrientation != orientation) SetOrientation(p.LyricsOrientation);
            if (IsHandleCreated) {
                Size before=Size;Constrain();
                if(positioned && Size!=before)ClampToScreen();
                int style = GetWindowLong(Handle,-20); SetWindowLong(Handle,-20,locked?style|0x20:style&~0x20);
            }
            tools = !locked && Bounds.Contains(Cursor.Position); RenderSurface();
            if (IsHandleCreated && foreground != IntPtr.Zero && foreground != Handle && GetForegroundWindow() == Handle) SetForegroundWindow(foreground);
        }
        internal void Present(bool enabled) {
            if (!enabled) { Hide(); return; }
            IntPtr foreground = GetForegroundWindow(), window = Handle;
            if (!positioned) {
                Rectangle area = Screen.PrimaryScreen.WorkingArea;
                Size = orientation == "vertical" ? new Size(S(142),Math.Min(S(640),area.Height-S(48))) : new Size(Math.Min(S(820),area.Width-S(48)),S(142));
                Location = new Point(area.Left+(area.Width-Width)/2,area.Bottom-Height-S(36)); positioned=true;
            }
            if (!Visible) Show();
            SetWindowPos(Handle,(IntPtr)(-1),0,0,0,0,0x0010|0x0001|0x0002);
            RenderSurface();
            if (foreground!=IntPtr.Zero && foreground!=Handle && GetForegroundWindow()==Handle) SetForegroundWindow(foreground);
        }
        internal void UpdateLyrics(string current,string next,string kind) {
            if (line==current && secondary==next && state==kind)return;
            line=current;secondary=next;state=kind;changed=DateTime.UtcNow;AccessibleDescription=current+"，"+next;RenderSurface();
        }
        internal void Reset() { UpdateLyrics("Husic","","idle"); }
        private void SetOrientation(string next) {
            if (next==orientation)return;
            Rectangle old=Bounds;orientation=next;Constrain();
            if (positioned) {
                Size=new Size(Math.Max(MinimumSize.Width,old.Height),Math.Max(MinimumSize.Height,old.Width));
                Location=new Point(old.Left+(old.Width-Width)/2,old.Top+(old.Height-Height)/2);ClampToScreen();
            }
            Arrange();RenderSurface();
        }
        private void ClampToScreen() { Rectangle area=Screen.FromRectangle(Bounds).WorkingArea;Location=new Point(Math.Max(area.Left,Math.Min(Left,area.Right-Width)),Math.Max(area.Top,Math.Min(Top,area.Bottom-Height))); }
        private void Arrange() {
            if (toolBounds==null)return;int s=S(28),gap=S(4),stride=s+gap;
            if(orientation=="horizontal") {
                int center=(Width-(3*s+2*gap))/2;
                for(int i=0;i<3;i++)toolBounds[i]=new Rectangle(center+i*stride,S(7),s,s);
                for(int i=3;i<5;i++)toolBounds[i]=new Rectangle(S(10)+(i-3)*stride,S(7),s,s);
                for(int i=5;i<8;i++)toolBounds[i]=new Rectangle(Width-S(10)-(8-i)*stride+gap,S(7),s,s);
            } else {
                int center=(Height-(3*s+2*gap))/2;
                for(int i=0;i<3;i++)toolBounds[i]=new Rectangle((Width-s)/2,center+i*stride,s,s);
                for(int i=3;i<5;i++)toolBounds[i]=new Rectangle((Width-s)/2,S(8)+(i-3)*stride,s,s);
                for(int i=5;i<8;i++)toolBounds[i]=new Rectangle((Width-s)/2,Height-S(8)-(8-i)*stride+gap,s,s);
            }
        }
        internal Bitmap RenderBitmap() {
            Bitmap bitmap=new Bitmap(Math.Max(1,Width),Math.Max(1,Height),PixelFormat.Format32bppPArgb);
            using(Graphics g=Graphics.FromImage(bitmap)) {
                g.SmoothingMode=SmoothingMode.AntiAlias;g.Clear(Color.Transparent);
                if(!locked) using(Brush hit=new SolidBrush(Color.FromArgb(1,24,24,28))) g.FillRectangle(hit,0,0,Width,Height);
                // Alpha 1 is visually transparent, but keeps the unlocked blank
                // drag/resize area hittable. Locked mode is true click-through.
                int alpha=tools?Math.Max(160,backdrop*255/100):backdrop*255/100;
                using(Brush b=new SolidBrush(Color.FromArgb(alpha,24,24,28)))
                using(GraphicsPath p=Round(new RectangleF(0,0,Width,Height),S(16)))g.FillPath(b,p);
                bool vertical=orientation=="vertical";
                RectangleF text=vertical?new RectangleF(S(10),S(16),Width-S(20),Height-S(32)):new RectangleF(S(18),S(42),Width-S(36),Height-S(52));
                RenderedSecondary=secondaryEnabled&&!string.IsNullOrWhiteSpace(secondary);
                float available=vertical?text.Width*(RenderedSecondary?.54f:1):text.Height*(RenderedSecondary?.58f:1);
                float preferred=S(lyricSize)*(vertical?Width/(float)S(142):Height/(float)S(142));
                float font=Math.Max(S(minFont),Math.Min(S(maxFont),Math.Min(preferred,available)));
                RenderedFontSize=font/scale;
                if(!RenderedSecondary)DrawText(g,line,text,font,lyricColor,vertical);
                else if(vertical) {
                    DrawText(g,line,new RectangleF(text.X+text.Width*.47f,text.Y,text.Width*.53f,text.Height),font,lyricColor,true);
                    DrawText(g,secondary,new RectangleF(text.X,text.Y,text.Width*.43f,text.Height),Math.Max(S(10),font*.57f),secondaryColor,true);
                } else {
                    DrawText(g,line,new RectangleF(text.X,text.Y,text.Width,text.Height*.62f),font,lyricColor,false);
                    DrawText(g,secondary,new RectangleF(text.X,text.Y+text.Height*.64f,text.Width,text.Height*.36f),Math.Max(S(10),font*.57f),secondaryColor,false);
                }
                if(tools && !locked) for(int i=0;i<actions.Length;i++) {
                    Rectangle r=toolBounds[i];
                    if(i==hover)using(Brush b=new SolidBrush(Color.FromArgb(pressed==i?80:45,255,255,255)))using(GraphicsPath p=Round(r,S(7)))g.FillPath(b,p);
                    DrawGlyph(g,actions[i],new RectangleF(r.X+S(5),r.Y+S(5),S(18),S(18)),Color.FromArgb(240,240,244));
                }
            }
            return bitmap;
        }
        private void DrawText(Graphics g,string text,RectangleF box,float size,Color color,bool vertical) {
            if(String.IsNullOrEmpty(text)||box.Width<=0||box.Height<=0)return;
            if(vertical) {
                List<string> glyphs=new List<string>();TextElementEnumerator elements=StringInfo.GetTextElementEnumerator(text);
                while(elements.MoveNext())glyphs.Add((string)elements.Current);
                float step=size*1.2f;int count=Math.Max(1,(int)(box.Height/step));bool truncated=glyphs.Count>count;
                int shown=Math.Min(count,glyphs.Count);float top=box.Y+(box.Height-shown*step)/2;
                for(int i=0;i<shown;i++)DrawText(g,truncated&&i==shown-1?"︙":glyphs[i],new RectangleF(box.X,top+i*step,box.Width,step),size,color,false);
                return;
            }
            using(Font font=new Font("Microsoft YaHei UI",size,FontStyle.Bold,GraphicsUnit.Pixel))
            using(StringFormat format=new StringFormat {Alignment=StringAlignment.Center,LineAlignment=StringAlignment.Center,Trimming=StringTrimming.EllipsisCharacter,FormatFlags=StringFormatFlags.NoWrap})
            using(GraphicsPath path=new GraphicsPath()) {
                path.AddString(text,font.FontFamily,(int)font.Style,font.Size,box,format);
                using(Pen shadow=new Pen(Color.FromArgb(210,12,12,16),Math.Max(2,S(2)))) {shadow.LineJoin=LineJoin.Round;g.DrawPath(shadow,path);}
                using(Brush brush=new SolidBrush(color))g.FillPath(brush,path);
            }
        }
        private void RenderSurface() {
            if(!IsHandleCreated||!Visible||painting||Width<2||Height<2)return;
            painting=true;
            try {Arrange();using(Bitmap b=RenderBitmap())LayeredSurface.Present(Handle,b,Location);}
            finally {painting=false;}
        }
        private static GraphicsPath Round(RectangleF r,float radius) {
            GraphicsPath p=new GraphicsPath();float d=Math.Min(radius*2,Math.Min(r.Width,r.Height));
            p.AddArc(r.X,r.Y,d,d,180,90);p.AddArc(r.Right-d,r.Y,d,d,270,90);p.AddArc(r.Right-d,r.Bottom-d,d,d,0,90);p.AddArc(r.X,r.Bottom-d,d,d,90,90);p.CloseFigure();return p;
        }
        private void DrawGlyph(Graphics g,string action,RectangleF r,Color color) {
            GraphicsState saved=g.Save();g.TranslateTransform(r.X,r.Y);g.ScaleTransform(r.Width/18,r.Height/18);
            using(Pen p=new Pen(color,1.4f))using(Brush b=new SolidBrush(color)) {
                p.StartCap=p.EndCap=LineCap.Round;p.LineJoin=LineJoin.Round;
                if(action=="toggle") {if(playing){g.FillRectangle(b,5,3,3,12);g.FillRectangle(b,11,3,3,12);}else g.FillPolygon(b,new[]{new PointF(6,3),new PointF(15,9),new PointF(6,15)});}
                else if(action=="previoustrack"||action=="nexttrack") {bool n=action=="nexttrack";g.DrawLine(p,n?14:4,4,n?14:4,14);g.FillPolygon(b,n?new[]{new PointF(4,3),new PointF(13,9),new PointF(4,15)}:new[]{new PointF(14,3),new PointF(5,9),new PointF(14,15)});}
                else if(action=="lyrics-larger"||action=="lyrics-smaller") {g.DrawLine(p,4,9,14,9);if(action=="lyrics-larger")g.DrawLine(p,9,4,9,14);}
                else if(action=="lyrics-lock") {g.DrawRectangle(p,4,8,10,8);g.DrawArc(p,6,2,6,10,180,180);g.DrawLine(p,9,11,9,13);}
                else if(action=="open") {g.DrawRectangle(p,3,4,12,10);g.DrawLine(p,3,7,15,7);}
                else {g.DrawLine(p,5,5,13,13);g.DrawLine(p,13,5,5,13);}
            }
            g.Restore(saved);
        }
        protected override void Dispose(bool disposing) {if(disposing){pulse.Dispose();tips.Dispose();}base.Dispose(disposing);}
    }
}
