package main

import (
	"context"
	"fmt"
	"io"
	"os"
	pb "synthesize/filesync"
	"time"

	"google.golang.org/grpc"
)

func Connect() {
	conn, err := grpc.NewClient(PrivateIP) // Replace with actual private IP
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := pb.NewFileSyncServiceClient(conn)
	filename := "example.txt"
	f, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	const chunkSize = 1024
	buffer := make([]byte, chunkSize)
	chunkNumber := int32(0)

	for {
		n, err := f.Read(buffer)
		if err == io.EOF {
			break
		}

		isLast := false
		if n < chunkSize {
			isLast = true
		}

		chunk := &pb.FileChunk{
			Filename:    filename,
			Data:        buffer[:n],
			ChunkNumber: chunkNumber,
			IsLast:      isLast,
		}
		chunkNumber++

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		ack, err := client.SendFile(ctx, chunk)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Sent chunk %d: %s\n", chunk.ChunkNumber, ack.Message)
	}
}
