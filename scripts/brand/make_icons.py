"""Render Cubicle's app icon (PNG sizes + multi-size .ico) from code.

    uv run --no-project --with pillow python scripts/brand/make_icons.py

The mark: an amber tile holding a friendly ink-coloured screen with two eyes
and a smile. Small sizes (<= 32 px) use a simplified, pixel-aligned variant so
the tray and title-bar icons stay crisp instead of blurring fine details.
"""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

AMBER = (255, 176, 32, 255)
INK = (23, 25, 43, 255)
WHITE = (255, 255, 255, 255)
MINT = (43, 196, 138, 255)

ROOT = Path(__file__).resolve().parents[2]
BRAND = ROOT / "assets" / "brand"
APP_ASSETS = ROOT / "desktop" / "src" / "Cubicle.Desktop" / "Assets"


def render_detailed(size: int) -> Image.Image:
    s = 4
    n = size * s
    img = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle((0, 0, n - 1, n - 1), radius=int(n * 0.22), fill=AMBER)
    m, top, bottom = int(n * 0.17), int(n * 0.19), int(n * 0.71)
    d.rounded_rectangle((m, top, n - m, bottom), radius=int(n * 0.11), fill=INK)
    eye_r = int(n * 0.058)
    ey = int(top + (bottom - top) * 0.40)
    for ex in (int(n * 0.38), int(n * 0.62)):
        d.ellipse((ex - eye_r, ey - eye_r, ex + eye_r, ey + eye_r), fill=WHITE)
    sw, sy = int(n * 0.15), int(top + (bottom - top) * 0.60)
    d.arc((n // 2 - sw, sy - sw // 2, n // 2 + sw, sy + sw // 2), start=20, end=160, fill=WHITE,
          width=max(2, int(n * 0.04)))
    d.rounded_rectangle((int(n * 0.27), int(n * 0.78), int(n * 0.73), int(n * 0.845)), radius=int(n * 0.03), fill=INK)
    r, cx, cy = int(n * 0.08), int(n * 0.81), int(n * 0.19)
    d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=MINT, outline=AMBER, width=max(2, int(n * 0.025)))
    return img.resize((size, size), Image.LANCZOS)


def render_small(size: int) -> Image.Image:
    """Bold, simplified mark for 16-32 px: tile, screen and two eyes, snapped to whole pixels."""
    s = 8
    n = size * s
    img = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    unit = n / 16  # design on a 16-unit grid

    def box(x0: float, y0: float, x1: float, y1: float) -> tuple[int, int, int, int]:
        return (round(x0 * unit), round(y0 * unit), round(x1 * unit) - 1, round(y1 * unit) - 1)

    d.rounded_rectangle(box(0, 0, 16, 16), radius=round(3.5 * unit), fill=AMBER)
    d.rounded_rectangle(box(2, 3, 14, 12), radius=round(2 * unit), fill=INK)
    for x in (5, 9):
        d.rounded_rectangle(box(x, 5.5, x + 2, 8.5), radius=round(0.9 * unit), fill=WHITE)
    d.rounded_rectangle(box(4, 13, 12, 14.5), radius=round(0.7 * unit), fill=INK)
    return img.resize((size, size), Image.LANCZOS)


def render(size: int) -> Image.Image:
    return render_small(size) if size <= 32 else render_detailed(size)


def main() -> None:
    sizes = [16, 20, 24, 32, 40, 48, 64, 128, 256]
    images = {sz: render(sz) for sz in sizes}
    ico_frames = [images[sz] for sz in sizes]
    for out in (BRAND, APP_ASSETS):
        out.mkdir(parents=True, exist_ok=True)
        ico_frames[-1].save(out / "cubicle.ico", sizes=[(sz, sz) for sz in sizes], append_images=ico_frames[:-1])
    for sz in (64, 256):
        images[sz].save(BRAND / f"cubicle-{sz}.png")
    render_detailed(1024).save(BRAND / "cubicle-1024.png")
    render_detailed(256).save(APP_ASSETS / "cubicle-256.png")  # crisp in-app logo
    print("icons written to", BRAND, "and", APP_ASSETS)


if __name__ == "__main__":
    main()
