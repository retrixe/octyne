package system

import (
	"bytes"
	"context"
	"log"
	"os"
	"time"
)

// ReadAndWatchFile reads a file and returns its contents.
// It also starts a goroutine that watches for changes to the file and sends updates on a channel.
func ReadAndWatchFile(filePath string) (<-chan []byte, context.CancelFunc, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	channel := make(chan []byte, 1)
	channel <- file // Send the initial file content to the channel
	go (func() {
		for {
			select {
			case <-ctx.Done():
				close(channel)
				return
			case <-time.After(1 * time.Second):
			}
			newFile, err := os.ReadFile(filePath)
			if err != nil {
				log.Println("An error occurred while reading " + filePath + "! " + err.Error())
				continue
			}
			if !bytes.Equal(newFile, file) {
				file = newFile
				channel <- newFile
			}
		}
	})()
	return channel, cancel, nil
}
