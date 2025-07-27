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

Create a `config.json` file in the Octyne folder with the following content, replacing `<user>` with your Linux account username and adjusting the RAM allocation as needed:

```jsonc
{
  "servers": {
    "mcserver": {
      "enabled": true,
      // Replace <user> with your Linux account username, e.g. /home/abcxyz/mcserver
      "directory": "/home/<user>/mcserver",
      // Replace 2G with the amount of RAM you want to allocate to the Minecraft server.
      "command": "java -Xmx2G -jar paper.jar nogui"
    }
  }
}
```

You can use the `nano` text editor to create and edit the file:

```bash
nano ~/octyne/config.json
```

You can save and exit the file in `nano` by pressing `CTRL + X`, then `Y`, and `Enter`.

## Step 4: Run Octyne

To run Octyne, open a terminal, navigate to the Octyne folder, and run `./octyne`:

```bash
# Navigate to the Octyne folder
cd ~/octyne

# Run Octyne
./octyne
```

You will receive the message that an `admin` user has been generated, along with its password. Make sure to note down this password, you can change it once you log into the Octyne web dashboard.

Open the Octyne web dashboard at `http://<your server's IP>:7877` in your browser, log in, and you should see the Minecraft server at `mcserver`! (If running locally, use `http://localhost:7877`.)

You may notice that the Minecraft server is not running yet, due to the Minecraft EULA not being accepted. To accept the EULA, after clicking on the `mcserver` server, edit `eula.txt` in the `Files` tab and change the line `eula=false` to `eula=true`. Now, you can start the server by clicking the `Start` button in the `Console` tab.

## Step 5: Run Octyne with systemd (recommended)

Octyne will stop once you close the terminal. To run it in the background, you can use `systemd` on Linux to manage Octyne as a service.

First, you must move the Octyne binary to a more appropriate location, such as `/usr/local/bin/`, to avoid issues with security mechanisms like SELinux. You can do this with the following command:

```bash
sudo mv ~/octyne/octyne /usr/local/bin/octyne

# if using SELinux (openSUSE, Fedora, RHEL, CentOS, Oracle/Rocky/Alma/Amazon Linux, etc.), run this:
sudo restorecon /usr/local/bin/octyne
```

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
ExecStart=/usr/local/bin/octyne

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
# NOTE: if you did not move Octyne to `/usr/local/bin` in Step 5, use the old path `~/octyne/octyne`
wget -O /usr/local/bin/octyne https://github.com/retrixe/octyne/releases/latest/download/octyne-linux-x86_64

# Make the Octyne binary executable
chmod +x /usr/local/bin/octyne

# Start Octyne
sudo systemctl start octyne.service
```
