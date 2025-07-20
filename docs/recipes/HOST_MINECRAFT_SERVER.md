# Recipe: Host a Minecraft server with Octyne

Note: This recipe is written mostly for Linux users, but can be followed for other operating systems. On Linux systems, `~` refers to your home directory. You can replace it with any directory you prefer.

## Step 1: Setup a Minecraft server

You can use any Minecraft server software, but for this recipe, we will use [Paper](https://papermc.io/), a popular fork of Spigot. Download the latest Paper release from the [Paper downloads page](https://papermc.io/downloads), and save it to the `~/mcserver` folder with the `paper.jar` file name.

Additionally, install Java if you haven't already, as it is required to run Minecraft servers. You can install OpenJDK on Linux with:

```bash
# For Debian/Ubuntu-based systems
sudo apt install openjdk-17-jre-headless

# For Fedora/RHEL-based systems
sudo dnf install java-17-openjdk-headless
```

## Step 2: Download Octyne to a folder

Create a folder for Octyne, for example `~/octyne/`, and download the latest Octyne release from the [Octyne releases page](https://github.com/retrixe/octyne/releases/latest) into that folder.

You can use `wget` on Linux to download it directly to the folder:

```bash
# Create the folder
mkdir ~/octyne

# Download the latest Octyne release using `wget`
# If not using 64-bit Linux, replace `linux-x86_64` with the appropriate platform,
# e.g. `macos-arm64`, `linux-arm64`, `linux-armv6`, etc.
wget -O ~/octyne/octyne https://github.com/retrixe/octyne/releases/latest/download/octyne-linux-x86_64
```

Make the Octyne binary executable by running:

```bash
chmod +x ~/octyne/octyne
```

## Step 3: Create a config file

Create a `config.json` file in the Octyne folder (`~/octyne/config.json`) with the following content:

```jsonc
{
  "servers": {
    "mcserver": {
      "enabled": true,
      "directory": "/home/user/mcserver",
      "command": "java -Xmx2G -jar paper.jar nogui"
    }
  }
}
```

## Step 4: Run Octyne

To run Octyne, open a terminal, navigate to the Octyne folder, and run `./octyne`:

```bash
# Navigate to the Octyne folder
cd ~/octyne

# Run Octyne
./octyne
```

You will receive the message that an `admin` user has been generated. You can now access the Octyne web dashboard at `http://<your server's IP>:42069`! (If running locally, use `http://localhost:42069`.)

You should see the Octyne dashboard, where you can manage your Minecraft server and other applications.

## Step 5: Run Octyne with systemd (recommended)

Octyne will stop once you close the terminal. To run it in the background, you can use `systemd` on Linux to manage Octyne as a service.

Create a `systemd` service file with the following command:

```bash
sudo nano /etc/systemd/system/octyne.service
```

Paste the following content into the file, replacing `abcxyz` with your Linux account username and adjusting the paths as necessary:

```ini
[Unit]
Description=Octyne
After=network.target
StartLimitIntervalSec=0

[Service]
Type=simple
Restart=on-failure
RestartSec=1
# Replace `abcxyz` with your Linux account username.
User=abcxyz
WorkingDirectory=/home/abcxyz/octyne/
ExecStart=/home/abcxyz/octyne/octyne

[Install]
WantedBy=multi-user.target
```

You can save and exit the file (in nano, press `CTRL + X`, then `Y`, and `Enter`).

Then enable and start the Octyne service with:

```bash
sudo systemctl enable --now octyne.service
```

## Step 6: Install octynectl (recommended)

You can install [octynectl](https://github.com/retrixe/octynectl) to manage Octyne from the terminal. This tool allows you to control Octyne servers directly from the command line using the `octynectl` command.

## Step 7: Setup HTTPS (recommended)

[The README covers how to set up HTTPS for Octyne.](/README.md#https-setup) It is highly recommended to set up HTTPS for secure access to the Octyne dashboard.

## Future: Update Octyne

To update Octyne, you can stop Octyne, download the latest version, then restart it. Here’s how you can do it:

```bash
# Stop Octyne
sudo systemctl stop octyne.service

# Download the latest Octyne release using `wget`
# If not using 64-bit Linux, replace `linux-x86_64` with the appropriate platform,
# e.g. `macos-arm64`, `linux-arm64`, `linux-armv6`, etc.
wget -O ~/octyne/octyne https://github.com/retrixe/octyne/releases/latest/download/octyne-linux-x86_64

# Make the Octyne binary executable
chmod +x ~/octyne/octyne

# Start Octyne
sudo systemctl start octyne.service
```
