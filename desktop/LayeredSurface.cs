using System;
using System.ComponentModel;
using System.Drawing;
using System.Runtime.InteropServices;

namespace Hardware.Music
{
    // Per-pixel alpha: the lyrics remain opaque while the empty desktop is
    // genuinely transparent. No color-key fringes or second browser surface.
    internal static class LayeredSurface
    {
        [StructLayout(LayoutKind.Sequential)] private struct NativeSize { internal int Width, Height; }
        [StructLayout(LayoutKind.Sequential, Pack = 1)] private struct Blend { internal byte Op, Flags, Alpha, Format; }
        [DllImport("user32.dll")] private static extern IntPtr GetDC(IntPtr window);
        [DllImport("user32.dll")] private static extern int ReleaseDC(IntPtr window, IntPtr dc);
        [DllImport("gdi32.dll")] private static extern IntPtr CreateCompatibleDC(IntPtr dc);
        [DllImport("gdi32.dll")] private static extern bool DeleteDC(IntPtr dc);
        [DllImport("gdi32.dll")] private static extern IntPtr SelectObject(IntPtr dc, IntPtr value);
        [DllImport("gdi32.dll")] private static extern bool DeleteObject(IntPtr value);
        [DllImport("user32.dll", SetLastError = true)] private static extern bool UpdateLayeredWindow(IntPtr window, IntPtr destination, ref Point position, ref NativeSize size, IntPtr source, ref Point sourcePoint, int key, ref Blend blend, int flags);
        internal static void Present(IntPtr window, Bitmap bitmap, Point position)
        {
            IntPtr screen = GetDC(IntPtr.Zero), memory = IntPtr.Zero, image = IntPtr.Zero, previous = IntPtr.Zero;
            try {
                memory = CreateCompatibleDC(screen);
                image = bitmap.GetHbitmap(Color.FromArgb(0));
                previous = SelectObject(memory, image);
                NativeSize size = new NativeSize { Width = bitmap.Width, Height = bitmap.Height };
                Point origin = Point.Empty;
                Blend blend = new Blend { Op = 0, Flags = 0, Alpha = 255, Format = 1 };
                if (!UpdateLayeredWindow(window, screen, ref position, ref size, memory, ref origin, 0, ref blend, 2)) throw new Win32Exception(Marshal.GetLastWin32Error());
            }
            finally {
                if (previous != IntPtr.Zero) SelectObject(memory, previous);
                if (image != IntPtr.Zero) DeleteObject(image);
                if (memory != IntPtr.Zero) DeleteDC(memory);
                if (screen != IntPtr.Zero) ReleaseDC(IntPtr.Zero, screen);
            }
        }
    }
}
