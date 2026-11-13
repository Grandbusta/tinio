# Brand

**The mark** is one of the characters, as a pixel bust: a solid head with two eyes cut out,
a wide neck and broad shoulders, on an 8 by 8 grid, with the outer corners eased so it reads
solid and warm rather than retro. It's always one colour: Ink `#0f4b92` on light, white on
dark or on Ink. The eyes are holes, never filled in.

| File | Use |
|---|---|
| [mark.svg](mark.svg), [mark-white.svg](mark-white.svg) | The mark at any size, in Ink, and in white for dark backgrounds. |
| [favicon.svg](favicon.svg) | The mark drawn on whole pixels of a 16 by 16 grid, so it's sharp in a browser tab; Ink, or `#a9cdff` in a dark theme. The console's and the profile page's. |
| [icon.svg](icon.svg), [icon-512.png](icon-512.png), [icon-192.png](icon-192.png) | The app icon: the white bust on Ink, shoulders at the bottom edge. |
| [mark-512.png](mark-512.png), [mark-192.png](mark-192.png), [mark-64.png](mark-64.png), [mark-32.png](mark-32.png) | The mark as PNG. |
| [banner-dark.png](banner-dark.png), [banner-light.png](banner-light.png) | The README banner, 1280 by 358. |
| [social.png](social.png) | GitHub's social preview, 1280 by 640 (Settings, then General, then Social preview). |
| [x-avatar.png](x-avatar.png) | The X (Twitter) profile picture, 800 by 800; X shows it as a circle. |
| [x-header.png](x-header.png) | The X header, 1500 by 500, with the words clear of the profile picture. |
| [banner.html](banner.html) | The source of the banners and the X images: open it, or render it with `?light`, `?social`, `?header` or `?avatar`. |
| [concepts.html](concepts.html) | The concepts for the first mark, Mini, kept for the record. |

The wordmark is "tinio" in Manrope ExtraBold, lowercase, set tight. Mark and word are one
colour together: Ink on light, white on dark. The line under them is in Ink, or `#a9cdff` on
dark.

## Palette

One deep blue for trust, slate greys for nearly everything, a butter highlight for warmth,
and the characters' own colours behind their faces. No gradients. About 90% of a screen is
white and slate, 8% text and greys, 2% Ink.

| Ink | 50 | 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 | 950 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| | `#f2f7fe` | `#e5f0fe` | `#cce2ff` | `#a9cdff` | `#7cadef` | `#548dda` | `#2d6bbb` | `#0f4b92` | `#0c3c75` | `#092b54` | `#06162c` |

| Slate | 50 | 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 | 950 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| | `#f8fafc` | `#f1f4f7` | `#e3e7ec` | `#cfd5dc` | `#8a919b` | `#6e757f` | `#565e69` | `#3e4650` | `#282e37` | `#161b21` | `#0b0f13` |

| Role | Light | Dark |
|---|---|---|
| Page, surface | white, Slate 100 | `#0e1114`, `#1b2025` |
| Text, muted text | Slate 950, Slate 600 | `#ebeff4`, `#abb2bb` |
| Lines; an input's border | Slate 200; Slate 400 (3:1) | `#292e35`; `#6b727b` |
| Buttons, links, focus | Ink 700, white text | Ink 300, Ink 950 text |
| The deep band | Ink 950 | Ink 950 |
| Highlight | Butter `#fff4c2` | |
| Approved, waiting, failed | `#1f7a4d`, `#824c11`, `#c9302d`, each with a word or icon | |

Every pairing passes WCAG 2.2 AA: 4.5:1 for text, 3:1 for borders and focus rings. Ink
was chosen clear of Telegram's blue (the agents talk there) and of Tailwind's default blue
and indigo, which read as AI-made.

To render the banners again (Chrome headless):

```bash
cd docs/brand && "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --hide-scrollbars --virtual-time-budget=8000 --window-size=1280,358 --screenshot="$PWD/banner-dark.png" "file://$PWD/banner.html"
```

The others are the same page: `?social` at 1280 by 640, `?header` at 1500 by 500, and
`?avatar` at 400 by 400 with `--force-device-scale-factor=2`. For example:

```bash
cd docs/brand && "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --hide-scrollbars --virtual-time-budget=8000 --window-size=1280,640 --screenshot="$PWD/social.png" "file://$PWD/banner.html?social"
```
