package main

import (
	"fmt"
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
)

func main() {
	updateDB()
	addFileToDB()

	cwd, _ := os.Getwd()
	fmt.Println("Current working dir:", cwd)

	// Create watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Start goroutine to listen for events
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				fmt.Println("EVENT:", event)
				if event.Op&fsnotify.Create == fsnotify.Create {
					fmt.Println("File created:", event.Name)
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					info, err := os.Stat(event.Name)
					if err != nil {
						fmt.Println("Error:", err)
						return
					}

					fmt.Println("File Name:", info.Name())
					fmt.Println("Size (bytes):", info.Size())
					fmt.Println("Last Modified:", info.ModTime())

				}
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					fmt.Println("File deleted:", event.Name)
				}
				if event.Op&fsnotify.Rename == fsnotify.Rename {
					fmt.Println("File renamed:", event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("ERROR:", err)
			}
		}
	}()

	// Add the folder to watcher
	fmt.Println("Watching folder: ./tmp")
	err = watcher.Add("./tmp")

	if err != nil {
		log.Fatal("Failed to add watcher:", err)
	}
	fmt.Println("Successfully watching ./tmp")

	// Block main from exiting
	<-make(chan struct{})
}
