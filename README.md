<img src="web/icon.svg" width="72" alt="">

# Boox Debloat

A small app for the **Onyx Boox C69 M Star** (Star Education edition). It removes the extra system apps, lets you bring any of them back with one switch, and keeps the sleep screen working.

It runs on your computer, opens in your browser, and talks to the tablet over USB.

## What it changes

- **Removes 20 apps** the tablet doesn't need: emergency alerts (no SIM), factory test tools, Onyx Mail, print services and Qualcomm mobile-network extras. The full list is in [`apps.json`](apps.json).
- **Turns off NFC** without removing it.
- **Never touches** the apps the sleep screen, updates and Google Play rely on. That includes **Onyx sync (ksync)**: older debloat guides removed it, which makes setting a sleep picture hang on "Processing…" or fail.

Apps are removed with `pm uninstall --user 0`, which only hides them from your user. Nothing is deleted from the tablet, and every app can be switched back on.

## What you need

- A **Mac** or a **Windows PC**.
- A **USB cable that carries data**. Some charging-only cables don't.
- **Homebrew** on Mac or **Scoop** on Windows, to install the app. If you don't have them, see [Install without Homebrew or Scoop](#install-without-homebrew-or-scoop).

## 1. Install

### Mac

Open **Terminal** (press Cmd+Space, type "Terminal", press Enter), then run:

```bash
brew install --cask RodrickRK/tap/boox-debloat
```

No Homebrew yet? Install it from [brew.sh](https://brew.sh) first.

### Windows

Open **PowerShell** (Start menu, type "PowerShell", press Enter), then run:

```powershell
scoop bucket add rodrickrk https://github.com/RodrickRK/scoop-bucket
scoop install boox-debloat
```

No Scoop yet? Install it from [scoop.sh](https://scoop.sh) first.

Both install **adb** (Android platform tools), which the app uses to talk to the tablet.

If adb is ever missing, the app shows an **Install Android platform tools** button that installs it with Homebrew or Scoop, plus a link to [download it from Google](https://developer.android.com/tools/releases/platform-tools).

## 2. Turn on USB debugging

The app can only reach the tablet with USB debugging on.

1. On the tablet, open **Apps**, tap the menu at the top right, then **App management**.
2. Turn on **USB Debug Mode**.
3. Connect the tablet to your computer with a USB cable that carries data.
4. When the tablet asks **"Allow USB debugging?"**, tap **Allow** and tick **Always allow from this computer**.

## 3. Open the app

In Terminal (Mac) or PowerShell (Windows), run:

```bash
boox-debloat
```

Your browser opens the app. If it doesn't, the terminal shows an address like `http://127.0.0.1:…/#…`; copy it into your browser.

When the tablet is ready, the left panel shows its name and a filled dot next to **Connected**. The app finds the tablet by itself; press **Refresh** in the left panel to check straight away.

## 4. Use it

1. **Choose apps.** Switch off the apps you want removed, or use **Remove all**. Apps under **Always kept** can't be removed.
2. **Review changes**, check the list, then **Apply to tablet**. A backup of your app list is saved first.
3. **Restart the tablet** when it asks, or any time with **Restart tablet** in the left panel.

To bring an app back, switch it on again and apply, or use **Restore all apps**. **Refresh** in the left panel reloads the app list from the tablet.

Backups are saved here:

- Mac: `~/Library/Application Support/boox-debloat/backups`
- Windows: `%AppData%\boox-debloat\backups`

### Sleep picture

1. Drop a picture onto the drawing of the tablet, or use **Choose picture**.
2. Keep **Fit to screen** on. The app resizes it to 758 × 1024, the tablet's screen size.
3. Choose **Send to tablet**.
4. On the tablet: open **Storage**, then **Pictures**, long-press the picture and set it as the screensaver.

Set it from the **file browser**. The hidden Wallpaper Settings page crashes on this model.

### Try it without a tablet

```bash
boox-debloat -demo
```

Demo mode shows the whole app with a pretend tablet. Nothing is sent to a real device.

## 5. Stop the app

Press **Ctrl+C** in the terminal, or just close the page; the app stops by itself a few minutes later.

## Update

**Mac:**

```bash
brew upgrade --cask boox-debloat
```

**Windows:**

```powershell
scoop update boox-debloat
```

## Uninstall

Uninstalling the app doesn't change your tablet. Restore any apps you want back first.

**Mac:**

```bash
brew uninstall --cask boox-debloat
```

**Windows:**

```powershell
scoop uninstall boox-debloat
```

## Install without Homebrew or Scoop

1. Download the file for your computer from the [latest release](https://github.com/RodrickRK/boox-debloat/releases/latest):
   - Mac with Apple chip (M1 or newer): `darwin_arm64.tar.gz`
   - Mac with Intel chip: `darwin_amd64.tar.gz`
   - Windows: `windows_amd64.zip`
2. Unzip it.
3. Install adb from Google's [Android SDK Platform-Tools](https://developer.android.com/tools/releases/platform-tools).
4. Run `boox-debloat` from the unzipped folder in a terminal.

On a Mac, the first run may say the app is from an unidentified developer. Right-click it in Finder, choose **Open**, then **Open** again.

## Troubleshooting

- **"Plug in your tablet" doesn't go away.** Check that USB Debug Mode is on and the cable carries data, then press **Refresh**. Run `adb devices` to see whether your computer can see the tablet.
- **"Install Android platform tools".** adb is missing. Use the button on that screen, or [download it from Google](https://developer.android.com/tools/releases/platform-tools) and press **Refresh**.
- **"Waiting for USB permission".** Unlock the tablet and tap **Allow** on the prompt.
- **Windows doesn't see the tablet.** Install the [Google USB driver](https://developer.android.com/studio/run/win-usb), then unplug and replug the tablet.
- **"This page has expired".** Close the page and run `boox-debloat` again. Each run opens a new address.
- **Setting a sleep picture hangs on "Processing…".** Bring back **Onyx sync (ksync)** under **Always kept**, restart the tablet, and use a 758 × 1024 picture.

## Build from source

```bash
go build -o boox-debloat .
./boox-debloat -demo
```

Releases are built with [GoReleaser](https://goreleaser.com) when a `v*` tag is pushed. See `.goreleaser.yaml`.

## Licence

MIT. Not affiliated with Onyx. Use at your own risk.

Created by [tekki](https://www.tekki.studio). If it saved you some time, you can [buy me a coffee](https://buymeacoffee.com/tekki.studio).
