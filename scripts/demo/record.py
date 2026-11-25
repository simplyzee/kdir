#!/usr/bin/env python3
"""Record docs/assets/demo.gif: a real bash session driving a real kdir binary.

    go build -o /tmp/kdir ./cmd/kdir
    pip install pyte pillow     # plus ffmpeg and the JetBrains Mono font
    python3 scripts/demo/record.py /tmp/kdir docs/assets/demo.gif

The session runs in a throwaway HOME with a fake kubeconfig (no credentials,
no cluster). Keystrokes are typed into a pseudo-terminal, the output is fed
through a terminal emulator (pyte), and each screen state is drawn as a frame.
"""

import os
import pty
import select
import shutil
import subprocess
import sys
import tempfile
import time

import pyte
from PIL import Image, ImageDraw, ImageFont

COLS, ROWS = 108, 22
FONT_SIZE = 15
PAD = 18
TITLE_H = 34
FONT_DIR = os.environ.get("FONT_DIR", "/usr/share/fonts/truetype/jetbrains-mono")

KUBECONFIG = """\
apiVersion: v1
kind: Config
current-context: prod-eu
clusters:
- name: prod-eu
  cluster: {server: "https://PROD5678.gr7.eu-west-1.eks.amazonaws.com"}
- name: staging-eu
  cluster: {server: "https://ABCD1234.gr7.eu-west-1.eks.amazonaws.com"}
users:
- name: me
  user: {}
contexts:
- name: prod-eu
  context: {cluster: prod-eu, user: me, namespace: default}
- name: staging-eu
  context: {cluster: staging-eu, user: me, namespace: default}
"""

PAYMENTS = """\
server: https://ABCD1234.gr7.eu-west-1.eks.amazonaws.com
namespace: payments
"""

LEGACY = """\
context: staging-us
"""

# The prompt shows what kubectl would use, like kube-ps1 or starship do:
# kdir's own context:namespace when a .kubectx is active, "kubectl disabled"
# when it fails closed, and the kubeconfig's current-context otherwise.
RCFILE = r"""
eval "$(kdir hook bash)"
__kube() {
  local s; s=$(kdir status --short)
  if [ -n "$s" ]; then printf '\[\e[1;32m\]%s\[\e[0m\]' "$s"; return; fi
  kdir status >/dev/null 2>&1
  if [ $? -eq 2 ]; then printf '\[\e[1;31m\]kubectl disabled\[\e[0m\]'; return; fi
  printf '\[\e[1;33m\]%s\[\e[0m\]' "$(sed -n 's/^current-context: //p' ~/.kube/config)"
}
PROMPT_COMMAND="${PROMPT_COMMAND};"'PS1="\[\e[1;34m\]\w\[\e[0m\] ($(__kube)) \$ "'
cd ~/src
"""

# (keys to type, seconds to wait afterwards). "\r" presses Enter.
SCRIPT = [
    ("", 1.2),
    ("cd payments\r", 1.6),
    ("cat .kubectx\r", 2.0),
    ("kdir allow\r", 2.4),
    ("kdir status\r", 3.4),
    ("cd ~/src\r", 2.0),
    ("cd legacy\r", 3.6),
    ("cd ~\r", 4.0),
]

THEME = {  # pyte color name -> RGB
    "default": (220, 223, 228), "black": (40, 44, 52), "red": (240, 113, 120),
    "green": (152, 195, 121), "brown": (229, 192, 123), "yellow": (229, 192, 123),
    "blue": (97, 175, 239), "magenta": (198, 120, 221), "cyan": (86, 182, 194),
    "white": (220, 223, 228),
}
BG = (30, 33, 40)
WINDOW_BG = (22, 24, 29)


def setup(kdir_bin):
    root = "/tmp/kdir-demo"  # fixed, so the overlay path in the GIF is tidy
    shutil.rmtree(root, ignore_errors=True)
    home = os.path.join(root, "home")
    for d in (".kube", "src/payments", "src/legacy", ".config", "bin"):
        os.makedirs(os.path.join(home, d))
    files = {
        ".kube/config": KUBECONFIG,
        "src/payments/.kubectx": PAYMENTS,
        "src/legacy/.kubectx": LEGACY,
        ".bashrc": RCFILE,
    }
    for rel, text in files.items():
        with open(os.path.join(home, rel), "w") as f:
            f.write(text)
    shutil.copy(kdir_bin, os.path.join(home, "bin/kdir"))
    # legacy is a teammate's repo that you already trust, so kdir acts on it.
    env = env_for(home, root)
    subprocess.run(["kdir", "allow", os.path.join(home, "src/legacy")],
                   env=env, check=True, capture_output=True)
    return root, home


def env_for(home, root):
    run = os.path.join(root, "run")
    os.makedirs(run, exist_ok=True)
    os.chmod(run, 0o700)
    return {
        "HOME": home,
        "PATH": os.path.join(home, "bin") + ":/usr/bin:/bin",
        "TERM": "xterm-256color",
        "LANG": "C.UTF-8",
        "XDG_RUNTIME_DIR": run,
        "XDG_CONFIG_HOME": os.path.join(home, ".config"),
    }


class Recorder:
    def __init__(self, env, home):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.frames = []  # (image, duration seconds)
        self.last = None
        self.last_t = time.monotonic()
        pid, self.fd = pty.fork()
        if pid == 0:
            os.chdir(home)
            os.execve("/bin/bash", ["bash", "--noprofile", "--rcfile",
                                    os.path.join(home, ".bashrc"), "-i"], env)
        import fcntl, struct, termios
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))

    def pump(self, seconds):
        end = time.monotonic() + seconds
        while True:
            left = end - time.monotonic()
            if left <= 0:
                break
            r, _, _ = select.select([self.fd], [], [], min(left, 0.03))
            if r:
                try:
                    self.stream.feed(os.read(self.fd, 65536))
                except OSError:
                    break
            self.snap()

    def snap(self):
        state = (tuple(self.screen.display), self.screen.cursor.x, self.screen.cursor.y)
        now = time.monotonic()
        if state == self.last:
            return
        if self.frames:
            img, _ = self.frames[-1]
            self.frames[-1] = (img, now - self.last_t)
        self.frames.append((render(self.screen), 0.1))
        self.last, self.last_t = state, now

    def type(self, keys, wait):
        for ch in keys:
            os.write(self.fd, ch.encode())
            self.pump(0.35 if ch == "\r" else 0.055)
        self.pump(wait)

    def finish(self):
        now = time.monotonic()
        img, _ = self.frames[-1]
        self.frames[-1] = (img, now - self.last_t)


_fonts = {}


def font(bold):
    if bold not in _fonts:
        name = "JetBrainsMono-Bold.ttf" if bold else "JetBrainsMono-Regular.ttf"
        _fonts[bold] = ImageFont.truetype(os.path.join(FONT_DIR, name), FONT_SIZE)
    return _fonts[bold]


CELL_W = None
CELL_H = int(FONT_SIZE * 1.45)


def color(name, default):
    if name == "default":
        return default
    if name in THEME:
        return THEME[name]
    if len(name) == 6:
        return tuple(int(name[i:i + 2], 16) for i in (0, 2, 4))
    return default


def render(screen):
    global CELL_W
    if CELL_W is None:
        CELL_W = font(False).getlength("M")
    w = int(PAD * 2 + CELL_W * COLS)
    h = int(TITLE_H + PAD * 2 + CELL_H * ROWS)
    img = Image.new("RGB", (w, h), WINDOW_BG)
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, w - 1, h - 1], radius=10, fill=BG)
    for i, c in enumerate([(255, 95, 86), (255, 189, 46), (39, 201, 63)]):
        cx, cy = PAD + 6 + i * 20, TITLE_H // 2 + 2
        d.ellipse([cx - 6, cy - 6, cx + 6, cy + 6], fill=c)
    title = "kdir demo"
    tf = font(False)
    d.text(((w - tf.getlength(title)) / 2, 9), title, font=tf, fill=(130, 135, 145))
    top = TITLE_H + PAD - 6
    for y in range(ROWS):
        line = screen.buffer[y]
        for x in range(COLS):
            ch = line[x]
            fg = color(ch.fg, THEME["default"])
            bg = color(ch.bg, None)
            if ch.reverse:
                fg, bg = (bg or BG), fg
            px, py = PAD + x * CELL_W, top + y * CELL_H
            if bg:
                d.rectangle([px, py, px + CELL_W, py + CELL_H], fill=bg)
            if ch.data.strip():
                d.text((px, py + 2), ch.data, font=font(ch.bold), fill=fg)
    cx, cy = screen.cursor.x, screen.cursor.y
    if not screen.cursor.hidden:
        px, py = PAD + cx * CELL_W, top + cy * CELL_H
        d.rectangle([px, py + 2, px + CELL_W - 1, py + CELL_H - 2], fill=(180, 185, 195))
    return img


def write_gif(frames, out):
    with tempfile.TemporaryDirectory() as tmp:
        concat = []
        for i, (img, dur) in enumerate(frames):
            p = os.path.join(tmp, f"{i:05d}.png")
            img.save(p)
            concat.append(f"file '{p}'\nduration {max(dur, 0.04):.3f}\n")
        concat.append(f"file '{p}'\n")  # ffmpeg ignores the last duration
        lst = os.path.join(tmp, "list.txt")
        with open(lst, "w") as f:
            f.writelines(concat)
        subprocess.run([
            "ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", lst,
            "-vf", "fps=20,split[a][b];[a]palettegen=max_colors=128:stats_mode=full[p];"
                   "[b][p]paletteuse=dither=none:diff_mode=rectangle",
            "-loop", "0", out,
        ], check=True)


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: record.py <kdir binary> <out.gif>")
    kdir_bin, out = os.path.abspath(sys.argv[1]), sys.argv[2]
    root, home = setup(kdir_bin)
    try:
        rec = Recorder(env_for(home, root), home)
        for keys, wait in SCRIPT:
            rec.type(keys, wait)
        rec.finish()
        write_gif(rec.frames, out)
    finally:
        shutil.rmtree(root, ignore_errors=True)
    print(f"wrote {out} ({len(rec.frames)} frames)")


if __name__ == "__main__":
    main()
