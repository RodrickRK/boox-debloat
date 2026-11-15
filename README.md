<img src="web/icon.svg" width="72" alt="">

# Boox Debloat

A small app for the **Onyx Boox C69 M Star** (Star Education edition). It removes the extra system apps, lets you bring any of them back with one switch, and keeps the sleep screen working.

It runs on your computer, opens in your browser, and talks to the tablet over USB with adb.

## What it changes

- **Removes 20 apps** the tablet doesn't need: emergency alerts (no SIM), factory test tools, Onyx Mail, print services and Qualcomm mobile-network extras. The full list is in [`apps.json`](apps.json).
- **Turns off NFC** without removing it.
- **Never touches** the apps the sleep screen, updates and Google Play rely on. That includes **Onyx sync (ksync)**: older debloat guides removed it, which makes setting a sleep picture hang on "Processing…" or fail.

Apps are removed with `pm uninstall --user 0`, which only hides them from your user. Nothing is deleted from the tablet, and every app can be switched back on.

## Install

**Mac** (Homebrew):

```bash
brew install --cask RodrickRK/tap/boox-debloat
```

**Windows** (Scoop):

```powershell
scoop bucket add rodrickrk https://github.com/RodrickRK/scoop-bucket
scoop install boox-debloat
```

Both install adb for you.

## Use

1. On the tablet, turn on **USB Debug Mode**: Apps, then the menu, then **App management**.
2. Connect the tablet with a USB cable that carries data.
3. Run `boox-debloat`. Your browser opens the app.
4. Tap **Allow** on the tablet's USB debugging prompt the first time.
5. Switch off the apps you want removed, then **Review changes** and **Apply to tablet**.
6. Restart the tablet when it asks, or any time with **Restart tablet** in the left panel.

To undo, switch an app back on, or use **Restore all apps**. A backup of your app list is saved before every change:

- Mac: `~/Library/Application Support/boox-debloat/backups`
- Windows: `%AppData%\boox-debloat\backups`

### Sleep picture

Drop a picture onto the drawing of the tablet, keep **Fit to screen** on, and choose **Send to tablet**. The app resizes it to 758 × 1024, copies it to the Pictures folder and indexes it. Then, on the tablet: open **Storage**, then **Pictures**, long-press the picture and set it as the screensaver.

Set it from the **file browser**. The hidden Wallpaper Settings page crashes on this model.

### Try it without a tablet

```bash
boox-debloat -demo
```

Demo mode shows the whole app with a pretend tablet. Nothing is sent to a real device.

## Troubleshooting

- **"Plug in your tablet" doesn't go away.** Check that USB Debug Mode is on and the cable carries data. Run `adb devices` to see whether your computer can see the tablet.
- **Windows doesn't see the tablet.** Install the [Google USB driver](https://developer.android.com/studio/run/win-usb), then unplug and replug the tablet.
- **The browser didn't open.** The terminal shows an address; open it yourself. The app stops a few minutes after you close the page.

## Build from source

```bash
go build -o boox-debloat .
./boox-debloat -demo
```

Releases are built with [GoReleaser](https://goreleaser.com) when a `v*` tag is pushed. See `.goreleaser.yaml`.

## Licence

MIT. Not affiliated with Onyx. Use at your own risk.

Created by [Tekki](https://www.tekki.studio). If it saved you some time, you can [buy me a coffee](https://buymeacoffee.com/tekki.studio).
