#!/bin/bash

set -e  # Exit on any error

CONFIG_FILE="./ecthelion/config.json"
BACKUP_FILE="./ecthelion/config.backup.json"

# Backup config file if it exists
if [ -f "$CONFIG_FILE" ]; then
  echo "Backing up existing config file..."
  cp "$CONFIG_FILE" "$BACKUP_FILE"
else
  echo "No existing config file to back up."
fi

# Write new config contents
echo "Writing new config file..."
cat > "$CONFIG_FILE" <<EOL
{
  "ip": "http://localhost:42069",
  "enableCookieAuth": true
}
EOL

# Build Ecthelion
echo "Building Ecthelion..."
cd ./ecthelion
corepack enable
yarn
yarn build
yarn export
cd ..

# Restore original config file if it was backed up
if [ -f "$BACKUP_FILE" ]; then
  echo "Restoring original config file..."
  cp "$BACKUP_FILE" "$CONFIG_FILE"
  rm "$BACKUP_FILE"
else
  echo "No backup config file to restore."
fi

# Build Octyne
echo "Building Octyne..."
go build .

echo "Build complete."
