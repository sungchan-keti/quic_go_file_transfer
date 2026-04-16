package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quic-go/quic-go"
)

const (
	uploadDir    = "./client_files"
	downloadDir  = "./client_downloads"
	cmdSize      = 10
	filenameSize = 256
	sizeField    = 20
)

func main() {
	if err := ensureDir(uploadDir); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}
	if err := ensureDir(downloadDir); err != nil {
		log.Fatalf("Failed to create download directory: %v", err)
	}

	createExampleFiles()

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-file-transfer"},
	}

	for {
		fmt.Println("\nQUIC Client Menu:")
		fmt.Println("1. Upload file to server")
		fmt.Println("2. Download file from server")
		fmt.Println("3. List files on server")
		fmt.Println("4. Exit")
		fmt.Print("Select: ")

		var choice int
		fmt.Scanln(&choice)

		switch choice {
		case 1:
			uploadFile(tlsConfig)
		case 2:
			downloadFile(tlsConfig)
		case 3:
			listFiles(tlsConfig)
		case 4:
			fmt.Println("Exiting.")
			return
		default:
			fmt.Println("Invalid selection.")
		}
	}
}

func uploadFile(tlsConfig *tls.Config) {
	fmt.Println("\nFiles available for upload:")
	files, err := os.ReadDir(uploadDir)
	if err != nil {
		log.Printf("Directory read error: %v", err)
		return
	}

	fileList := []os.DirEntry{}
	for _, file := range files {
		if !file.IsDir() {
			fileList = append(fileList, file)
		}
	}

	if len(fileList) == 0 {
		fmt.Println("No files to upload.")
		return
	}

	for i, file := range fileList {
		fileInfo, _ := file.Info()
		fmt.Printf("%d. %s (%d bytes)\n", i+1, file.Name(), fileInfo.Size())
	}

	fmt.Print("Select file number: ")
	var fileIndex int
	fmt.Scanln(&fileIndex)
	if fileIndex < 1 || fileIndex > len(fileList) {
		fmt.Println("Invalid file number.")
		return
	}

	fileName := fileList[fileIndex-1].Name()
	filePath := filepath.Join(uploadDir, fileName)

	conn, err := connectToServer(tlsConfig)
	if err != nil {
		log.Printf("Server connection failed: %v", err)
		return
	}
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("Stream open failed: %v", err)
		return
	}
	defer stream.Close()

	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("File open error: %v", err)
		return
	}
	defer file.Close()

	// Send command (10 bytes, padded)
	cmd := make([]byte, cmdSize)
	copy(cmd, []byte("UP"))
	stream.Write(cmd)

	// Send filename (256 bytes, padded)
	fnField := make([]byte, filenameSize)
	copy(fnField, []byte(fileName))
	stream.Write(fnField)

	// Send file content
	n, err := io.Copy(stream, file)
	if err != nil {
		log.Printf("File transfer error: %v", err)
		return
	}

	stream.Close()
	fmt.Printf("Uploaded '%s': %d bytes sent\n", fileName, n)
}

func downloadFile(tlsConfig *tls.Config) {
	conn, err := connectToServer(tlsConfig)
	if err != nil {
		log.Printf("Server connection failed: %v", err)
		return
	}
	defer conn.CloseWithError(0, "done")

	fileListStr := getFileList(conn)
	if fileListStr == "" {
		fmt.Println("Failed to retrieve file list.")
		return
	}

	files := strings.Split(strings.TrimSpace(fileListStr), "\n")
	if len(files) == 0 || (len(files) == 1 && files[0] == "") {
		fmt.Println("No files available on server.")
		return
	}

	fmt.Println("\nFiles available for download:")
	for i, file := range files {
		fmt.Printf("%d. %s\n", i+1, file)
	}

	fmt.Print("Select file number: ")
	var fileIndex int
	fmt.Scanln(&fileIndex)
	if fileIndex < 1 || fileIndex > len(files) {
		fmt.Println("Invalid file number.")
		return
	}

	selectedFile := files[fileIndex-1]
	fileName := strings.Split(selectedFile, " (")[0]

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("Stream open failed: %v", err)
		return
	}
	defer stream.Close()

	// Send command (10 bytes, padded)
	cmd := make([]byte, cmdSize)
	copy(cmd, []byte("DOWN"))
	stream.Write(cmd)

	// Send filename (256 bytes, padded)
	fnField := make([]byte, filenameSize)
	copy(fnField, []byte(fileName))
	stream.Write(fnField)

	// Read file size (20 bytes)
	sizeBytes := make([]byte, sizeField)
	n, err := stream.Read(sizeBytes)
	if err != nil {
		log.Printf("File size read error: %v", err)
		return
	}

	sizeStr := strings.TrimSpace(string(sizeBytes[:n]))
	if strings.Contains(sizeStr, "ERROR") {
		fmt.Printf("File '%s' not found on server.\n", fileName)
		return
	}

	fileSize, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		log.Printf("File size parse error: %v, received: '%s'", err, sizeStr)
		return
	}

	// Send ready signal
	stream.Write([]byte("READY"))

	// Create local file
	filePath := filepath.Join(downloadDir, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("File create error: %v", err)
		return
	}
	defer file.Close()

	received, err := io.Copy(file, stream)
	if err != nil && err != io.EOF {
		log.Printf("File receive error: %v", err)
		return
	}

	if received == fileSize {
		fmt.Printf("Downloaded '%s': %d bytes\n", fileName, received)
	} else {
		fmt.Printf("Downloaded '%s': %d/%d bytes\n", fileName, received, fileSize)
	}
}

func listFiles(tlsConfig *tls.Config) {
	conn, err := connectToServer(tlsConfig)
	if err != nil {
		log.Printf("Server connection failed: %v", err)
		return
	}
	defer conn.CloseWithError(0, "done")

	fileListStr := getFileList(conn)
	if fileListStr == "" {
		fmt.Println("Failed to retrieve file list.")
		return
	}

	fmt.Println("\nServer file list:")
	if strings.TrimSpace(fileListStr) == "" {
		fmt.Println("(empty)")
	} else {
		fmt.Println(fileListStr)
	}
}

func getFileList(conn quic.Connection) string {
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("Stream open failed: %v", err)
		return ""
	}
	defer stream.Close()

	cmd := make([]byte, cmdSize)
	copy(cmd, []byte("LIST"))
	stream.Write(cmd)

	listBytes, err := io.ReadAll(stream)
	if err != nil && err != io.EOF {
		log.Printf("List read error: %v", err)
		return ""
	}

	return string(listBytes)
}

func connectToServer(tlsConfig *tls.Config) (quic.Connection, error) {
	conn, err := quic.DialAddr(context.Background(), "localhost:4242", tlsConfig, nil)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %v", err)
	}
	fmt.Println("Connected to server.")
	return conn, nil
}

func ensureDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

func createExampleFiles() {
	files := map[string]string{
		"upload1.txt": "Test file 1 for upload from client to server.",
		"upload2.txt": "Test file 2 for upload.\nMultiple lines.\nUsed for file transfer testing.",
		"upload3.txt": "Test file 3 for upload from client to server.",
	}

	for name, content := range files {
		filePath := filepath.Join(uploadDir, name)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				log.Printf("Example file creation failed %s: %v", name, err)
			}
		}
	}
}
