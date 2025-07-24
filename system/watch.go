package system

import (
	"bytes"
	"log"
	"os"
	"time"
)

// ReadAndWatchFile reads a file and returns its contents.
// It also starts a goroutine that watches for changes to the file and sends updates on a channel.
func ReadAndWatchFile(filePath string) ([]byte, chan []byte, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	channel := make(chan []byte, 1)
	go (func() {
		for {
			select {
			case _, more := <-channel:
				if !more {
					return
				}
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
	return file, channel, nil
}
