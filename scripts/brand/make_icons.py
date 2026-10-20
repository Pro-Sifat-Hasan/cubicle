"""Render Cubicle's app icon (PNG sizes + multi-size .ico) from code.

    uv run --with pillow python scripts/brand/make_icons.py

The mark: an amber tile holding a friendly ink-coloured "staffer" screen with
two eyes and a smile — your always-on assistant.
"""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

AMBER = (255, 176, 32, 255)
INK = (23, 25, 43, 255)
WHITE = (255, 255, 255, 255)
MINT = (43, 196, 138, 255)

ROOT = Path(__file__).resolve().parents[2]
OUT_DIRS = [ROOT / "assets" / "brand", ROOT / "desktop" / "src" / "Cubicle.Desktop" / "Assets"]


def render(size: int) -> Image.Image:
    s = 4  # supersample for smooth edges
    n = size * s
    img = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle((0, 0, n - 1, n - 1), radius=int(n * 0.22), fill=AMBER)

    # screen / face
    m = int(n * 0.18)
    top = int(n * 0.20)
    bottom = int(n * 0.72)
    d.rounded_rectangle((m, top, n - m, bottom), radius=int(n * 0.10), fill=INK)
    # eyes
    eye_r = int(n * 0.055)
    ey = int(top + (bottom - top) * 0.42)
    for ex in (int(n * 0.38), int(n * 0.62)):
        d.ellipse((ex - eye_r, ey - eye_r, ex + eye_r, ey + eye_r), fill=WHITE)
    # smile
    sw = int(n * 0.16)
    sy = int(top + (bottom - top) * 0.62)
    d.arc((n // 2 - sw, sy - sw // 2, n // 2 + sw, sy + sw // 2), start=20, end=160, fill=WHITE, width=max(2, int(n * 0.035)))
    # desk
    d.rounded_rectangle((int(n * 0.26), int(n * 0.78), int(n * 0.74), int(n * 0.84)), radius=int(n * 0.03), fill=INK)
    # online dot
    r = int(n * 0.075)
    cx, cy = int(n * 0.80), int(n * 0.20)
    d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=MINT, outline=AMBER, width=max(2, int(n * 0.02)))

    return img.resize((size, size), Image.LANCZOS)


def main() -> None:
    sizes = [16, 24, 32, 48, 64, 128, 256]
    images = {sz: render(sz) for sz in sizes}
    for out in OUT_DIRS:
        out.mkdir(parents=True, exist_ok=True)
        images[256].save(out / "cubicle.ico", sizes=[(sz, sz) for sz in sizes])
    brand = OUT_DIRS[0]
    for sz in (64, 256):
        images[sz].save(brand / f"cubicle-{sz}.png")
    render(1024).save(brand / "cubicle-1024.png")
    print("icons written to", ", ".join(str(p) for p in OUT_DIRS))


if __name__ == "__main__":
    main()
