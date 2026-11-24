<img src="web/icon.svg" width="72" alt="">

# Boox Debloat

A small app for the **Onyx Boox C69 M Star** (Star Education edition). It removes the extra system apps, lets you bring any of them back with one switch, and keeps the sleep screen working.

It also installs apps from APK files, suggests e-ink friendly apps, and lets you browse, copy and delete files on the tablet.

It runs on your computer, opens in your browser, and talks to the tablet over USB or Wi-Fi.

## What it changes

- **Removes 20 apps** the tablet doesn't need: emergency alerts (no SIM), factory test tools, Onyx Mail, print services and Qualcomm mobile-network extras. The full list is in [`apps.json`](apps.json).
- **Turns off NFC** without removing it.
- **De-Googles the tablet if you want**, with a 10-second confirm and a way back. See [De-Google](#de-google).
- **Never touches** the apps the sleep screen, updates and the system rely on. That includes **Onyx sync (ksync)**: older debloat guides removed it, which makes setting a sleep picture hang on "Processing…" or fail.

Apps are removed with `pm uninstall --user 0`, which only hides them from your user. Nothing is deleted from the tablet, and every app can be switched back on.

## What you need

- A **Mac**, a **Windows PC** or a **Linux PC**.
- A **USB cable that carries data**. Some charging-only cables don't.
- **Homebrew** on Mac or **Scoop** on Windows, to install the app. If you don't have them, see [Install without Homebrew or Scoop](#install-without-homebrew-or-scoop). Linux needs nothing extra.

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

### Linux

1. Download `linux_amd64.tar.gz` from the [latest release](https://github.com/RodrickRK/boox-debloat/releases/latest) (`linux_arm64.tar.gz` on ARM, like a Raspberry Pi).
2. Open a terminal in the folder you downloaded it to and run:

   ```bash
   tar -xzf boox-debloat_*_linux_*.tar.gz
   mkdir -p ~/.local/bin && mv boox-debloat ~/.local/bin/
   ```

3. Install adb and the USB permissions it needs:

   | Distro | Command |
   | --- | --- |
   | Debian, Ubuntu, Mint, Pop!_OS | `sudo apt install adb android-sdk-platform-tools-common` then `sudo usermod -aG plugdev $USER` |
   | Fedora | `sudo dnf install android-tools` |
   | Arch, Manjaro | `sudo pacman -S android-tools android-udev` |

4. Log out and back in, so the USB permission takes effect.

On 64-bit Intel or AMD PCs you can skip installing adb and press **Download Android platform tools** in the app instead, but you still need the USB permission package from step 3.

The Mac and Windows installs both include **adb** (Android platform tools), which the app uses to talk to the tablet.

If adb is ever missing, the app shows a **Download Android platform tools** button. It downloads Google's official platform tools (about 10 MB) into the app's own settings folder, so it needs no Homebrew, Scoop or admin rights. You can also [download it from Google](https://developer.android.com/tools/releases/platform-tools) yourself.

## 2. Turn on USB debugging

The app can only reach the tablet with USB debugging on.

1. On the tablet, open **Apps**, tap the menu at the top right, then **App management**.
2. Turn on **USB Debug Mode**.
3. Connect the tablet to your computer with a USB cable that carries data.
4. When the tablet asks **"Allow USB debugging?"**, tap **Allow** and tick **Always allow from this computer**.

### Use Wi-Fi instead of the cable (optional)

The tablet and your computer must be on the same Wi-Fi network.

- **Start with USB (works on any firmware).** Connect with the cable as above, press **Switch to Wi-Fi** in the left panel, then unplug. This lasts until the tablet restarts; after a restart, plug in and switch again.
- **No cable: wireless debugging (Android 11 and newer).** Only works if your firmware shows **Developer options**. Turn on **Wireless debugging** there and tap **Pair device with pairing code**. In the app, type the pairing address and 6-digit code under **Or connect over Wi-Fi** and press **Pair**. The tablet usually connects by itself; if not, type the **IP address & Port** from the Wireless debugging screen and press **Connect**. The app also lists tablets it finds on your network, so you can tap one instead of typing.


**How to turn on wireless debugging:**

1. Join the same Wi-Fi network on the tablet and on your computer.
2. Turn on Developer options: open **Settings**, then **About tablet**, and tap **Build number** seven times until it says you're a developer. On some Boox firmware it's the version number instead.
3. Open **Developer options**. It's usually under **Settings › System**, or search Settings for "developer".
4. Turn on **Wireless debugging** and tap **Allow** for this network.
5. Tap the words **Wireless debugging** (not the switch) to open its screen. It shows the **IP address & Port** and a **Pair device with pairing code** button.

Wireless debugging turns itself off when the tablet restarts or changes network, and gets a new port each time. You only need to pair once per computer. If your firmware has no Developer options, use the USB way.

Over Wi-Fi, installing big apps takes longer, and restarting the tablet drops the connection.

**Why does wireless debugging keep turning off?** That's Android being careful: it switches wireless debugging off whenever the tablet restarts or Wi-Fi disconnects, even briefly, and picks a new port when you turn it back on. Boox tablets also turn Wi-Fi off when the screen sleeps to save battery, which takes wireless debugging down with it. To make it last longer:

- Turn off the Boox power setting that switches Wi-Fi off in standby (it costs some battery), or set a longer auto-sleep time while you use the app.
- Use **Switch to Wi-Fi** from USB instead. It doesn't depend on the wireless debugging switch and lasts until the tablet restarts. Reserving the tablet's IP address in your router keeps the address the same.
- You never need to pair again: turn wireless debugging back on and press **Connect**. The app finds the new port for you.

## 3. Open the app

In Terminal (Mac), PowerShell (Windows) or a terminal (Linux), run:

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

### Install apps

1. Choose **Apps** in the left panel.
2. Under **Recommended for e-ink**, open an app's official page (F-Droid or GitHub releases) and download its `.apk`.
3. Drop it onto **Install an app**. It installs, or updates the app if it's already there.

**Your apps** lists the apps you installed, each with **Uninstall**. The recommended list includes KOReader, Librera, Mihon, EinkBro, inkOS, Key Mapper, AnkiDroid, Kiwix, Material Files, LocalSend, Syncthing-Fork, F-Droid, Aurora Store and Obtainium. It links only to official sources, never to APK mirror sites. The one exception is the Play Store, which Google doesn't offer as a download; the app checks Google's signature on it before installing (see [Google Play](#google-play)).

### Files

Choose **Files** to browse the tablet's storage. Open folders, **Upload files** (or drop them on the list), **Download** to your computer, and **Delete** files or empty folders. The Android folder, where apps keep their data, can't be changed.

### Sleep picture

1. Drop a picture onto the drawing of the tablet, or use **Choose picture**.
2. Keep **Fit to screen** on. The app resizes it to 758 × 1024, the tablet's screen size.
3. Choose **Send to tablet**.
4. On the tablet: open **Storage**, then **Pictures**, long-press the picture and set it as the screensaver.

Set it from the **file browser**. The hidden Wallpaper Settings page crashes on this model.

### Google Play

The left panel shows the state of Google Play:

- **Play Store missing.** The C69 M Star's Star Education firmware comes with Google Play services but no Play Store. Download Google's Play Store APK (the **nodpi** version), for example from [APKMirror](https://www.apkmirror.com/apk/google-inc/google-play-store/), and choose it with **Install Play Store**. The app reads the file's manifest and signing certificate and only installs it if it's `com.android.vending` and signed by Google: either with Google's Play Store key (SHA-1 `38:91:8A:45:3D:07:19:93:54:F8:B1:9A:F0:5E:C6:56:2C:ED:57:88`) or with the same key as the Google apps that came on your tablet. Anything else is refused. Choose a plain `.apk`, not an XAPK bundle.

  The tablet's own Google apps are signed with a different Google key from the Play Store, so if Play Store won't sign in after installing, uninstall it from **Apps** and use **Aurora Store** instead.
- **Off.** Press **Turn on Google Play**: it turns on Google Play Store, Google Play services and Google Services Framework, skipping any that are already on, and saves a backup first. Restart the tablet when it asks.
- **On.** Nothing to do.

The Star Education firmware isn't certified by Google, so Play Store may say **"This device isn't Play Protect certified"** or refuse to sign in. Debloating never removes Google Play, Play Services or Google Services Framework (only **De-Google** does, when you ask), so this isn't caused by debloating. To fix it, register the tablet with Google:

1. Sign in to a Google account on the tablet (Settings, then Accounts), or at least open Play Store once.
2. With the tablet connected and USB debugging on, get its Google Services Framework (GSF) ID:

   ```bash
   adb shell 'content query --uri content://com.google.android.gsf.gservices --where "name=\"android_id\""'
   ```

   The number after `value=` is the GSF ID, a long number like `3812345678901234567`.
3. On your computer, open [google.com/android/uncertified](https://www.google.com/android/uncertified), sign in with the same Google account, paste the GSF ID and choose **Register**.
4. Wait 10 to 30 minutes. Google can take a few hours.
5. On the tablet, open **Settings**, then **Apps**, and for **Google Play Store** and **Google Play services**:
   - tap **Force stop**,
   - open **Storage** and tap **Clear data** (or **Clear storage**).
6. Restart the tablet, open Play Store and sign in again.

Or do step 5 from your computer:

```bash
adb shell am force-stop com.android.vending
adb shell am force-stop com.google.android.gms
adb shell pm clear com.android.vending
adb shell pm clear com.google.android.gms
adb reboot
```

Clearing Play services' data signs you out of Google on the tablet; sign in again after the restart.

A factory reset or firmware update gives the tablet a new GSF ID. Register the new one the same way.

#### Turn off Play Protect scanning

Once Play Store works, Play Protect may warn about or block apps you install yourself, like Aurora Store, KOReader or Boox Debloat's recommended apps, and it can be slow on this tablet. To turn its scanning off:

1. Open **Play Store** and tap your profile picture at the top right.
2. Tap **Play Protect**, then the settings (gear) icon at the top right.
3. Turn off **Scan apps with Play Protect**, and **Improve harmful app detection** if it's there.

You can turn it back on the same way. Google may not show these switches on every Play Store version.

If Play Store installs an app but the app closes straight away with a licence error, the app uses Google's licence check, which can fail on uncertified firmware. Install it from [Aurora Store](https://f-droid.org/packages/com.aurora.store/) or the developer's own site instead.

### De-Google

The **Google** group in the Debloat list holds Google Play Store, Google Play services, Google Services Framework, and the small Google apps the Play Store installs by itself (SafetyCore, Key Verifier, Google verifier).

**De-Google…** removes them for your user. The three main ones always go together; switch on **Keep** next to any of the small ones you want to keep. Before anything happens, a dialog lists what will be removed, suggests alternatives, and keeps the **De-Google** button greyed out for 10 seconds. A backup of your app list is saved first.

- **Apps that rely on Google services may not work** after this: Play Store apps that need Google to run, push notifications, Sign in with Google, and some banking, streaming and messaging apps.
- **Your Google account is signed out.** Android removes it from the tablet once Play services is gone. Sign in again after bringing Google back.
- **Alternatives** that work without Google: [Aurora Store](https://f-droid.org/packages/com.aurora.store/) (free apps from Google Play, no account needed), [F-Droid](https://f-droid.org/) and [Obtainium](https://github.com/ImranR98/Obtainium/releases/latest).
- **Play services and the framework keep their data** (`pm uninstall -k --user 0`), so the GSF ID you registered with Google survives. The Play Store and the small apps don't, so any Play Store APK can go back on.
- **Undo it** with **Bring Google back** in the Google group, or **Turn on Google Play** in the left panel. Play services and the framework come straight back. The Play Store on the C69 was installed after the firmware, so removing it deletes its files: install it again with **Install Play Store**. Restart the tablet and sign in to Google again. The small Google apps come back when the Play Store reinstalls them.
- **Restore all apps** doesn't bring Google back; it has its own button.
- The app never removes anything the tablet needs to work, like WebView, the permission controller, the package installer or the keyboard, whatever you choose.

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

**Linux:** download the new release and repeat steps 1 and 2 of the Linux install.

## Uninstall

Uninstalling the app doesn't change your tablet. Restore any apps you want back first.

If the app downloaded platform tools for you, they stay in `~/Library/Application Support/boox-debloat/platform-tools` (Mac), `%AppData%\boox-debloat\platform-tools` (Windows) or `~/.config/boox-debloat/platform-tools` (Linux). Delete that folder to remove them.

**Mac:**

```bash
brew uninstall --cask boox-debloat
```

**Windows:**

```powershell
scoop uninstall boox-debloat
```

**Linux:**

```bash
rm ~/.local/bin/boox-debloat
```

## Install without Homebrew or Scoop

1. Download the file for your computer from the [latest release](https://github.com/RodrickRK/boox-debloat/releases/latest):
   - Mac with Apple chip (M1 or newer): `darwin_arm64.tar.gz`
   - Mac with Intel chip: `darwin_amd64.tar.gz`
   - Windows: `windows_amd64.zip`
2. Unzip it.
3. Run `boox-debloat` from the unzipped folder in a terminal.
4. If the app says adb is missing, press **Download Android platform tools**.

On a Mac, the first run may say the app is from an unidentified developer. Right-click it in Finder, choose **Open**, then **Open** again.

## Troubleshooting

**Help and FAQ** in the app's left panel opens these instructions and answers in a new tab.

**Show activity** in the left panel opens a side panel with every command the app sends to the tablet and what the tablet answers, updated as it happens.

To report a problem, choose **Copy logs** in the left panel, then paste them into [a new issue](https://github.com/RodrickRK/boox-debloat/issues/new) with what you tried. The logs only cover this run of the app. They show just the last four characters of the tablet's serial number and hide your user folder.

- **"Plug in your tablet" doesn't go away.** Check that USB Debug Mode is on and the cable carries data, then press **Refresh**. Run `adb devices` to see whether your computer can see the tablet.
- **Wireless debugging keeps turning off.** Android switches it off when the tablet restarts or Wi-Fi drops, and Boox turns Wi-Fi off when the screen sleeps. See [Use Wi-Fi instead of the cable](#use-wi-fi-instead-of-the-cable-optional) for how to make it last.
- **Wi-Fi won't connect.** Check that the tablet and computer are on the same network, and that the port matches what the tablet shows now (wireless debugging picks a new port each time it's turned on). Some guest and office networks block devices from reaching each other.
- **"More than one device connected".** Another Android device or emulator is connected. Disconnect it, or start the app with `boox-debloat -serial <serial>`. The same tablet over USB and Wi-Fi at once is fine.
- **"Install Android platform tools".** adb is missing. Press **Download Android platform tools** on that screen, or [download it from Google](https://developer.android.com/tools/releases/platform-tools) and press **Refresh**.
- **"Waiting for USB permission".** Unlock the tablet and tap **Allow** on the prompt.
- **Linux: "Linux needs permission to use the tablet".** adb sees the tablet but can't open it. Install the USB permission package for your distro (Linux install, step 3), log out and back in, run `adb kill-server`, then replug the tablet.
- **Linux: `boox-debloat: command not found`.** `~/.local/bin` isn't on your PATH yet. Log out and back in, or run it as `~/.local/bin/boox-debloat`.
- **Windows doesn't see the tablet.** Install the [Google USB driver](https://developer.android.com/studio/run/win-usb), then unplug and replug the tablet.
- **"This page has expired".** Close the page and run `boox-debloat` again. Each run opens a new address.
- **"This device isn't Play Protect certified".** Register the tablet's GSF ID with Google, then force stop and clear data for Play Store and Play services. See [Google Play](#google-play).
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
