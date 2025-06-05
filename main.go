package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print(`
	Would you like to:
	1. Create a new bucket
	2. Add a folder to a bucket
	3. Connect to a new user
	> `)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	switch input {
	case "1":
		fmt.Print(`
		What would you like the bucket name to be?`)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		CreateBucket("my.db", input)
	case "2":
		fmt.Print(`
		What folder would you like to add?`)
		folder, _ := reader.ReadString('\n')
		folder = strings.TrimSpace(folder)

		fmt.Print(`
		To which bucket?`)
		bucket, _ := reader.ReadString('\n')
		bucket = strings.TrimSpace(bucket)
		AddFolderToBucket("my.db", folder, bucket)
	default:
		fmt.Println("Invalid selection. Exiting.")
		return
	}
}

/***

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

	// Block main from exiting
	<-make(chan struct{})
**/
