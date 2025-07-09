#!/bin/bash

set -e  # Exit on any error

CONFIG_FILE="./ecthelion/config.json"
BACKUP_FILE="./ecthelion/config.backup.json"

# Backup config file if it exists
if [ -f "$CONFIG_FILE" ]; then
  echo "Backing up existing config file..."
  cp "$CONFIG_FILE" "$BACKUP_FILE"
fi

# Write new config contents
cat > "$CONFIG_FILE" <<EOL
{
  "ip": "http://localhost:42069/api",
  "enableCookieAuth": true
}
EOL

# Build Ecthelion
cd ./ecthelion
corepack yarn
corepack yarn export
cd ..

# Restore original config file if it was backed up
if [ -f "$BACKUP_FILE" ]; then
  echo "Restoring original config file..."
  mv "$BACKUP_FILE" "$CONFIG_FILE"
else
  rm "$CONFIG_FILE"
fi

# Build Octyne
go build "$@"
