package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quic-go/quic-go"
)

const (
	serverAddr   = "localhost:4242"
	storageDir   = "./server_files"
	cmdSize      = 10
	filenameSize = 256
	sizeField    = 20
)

func main() {
	if err := ensureDir(storageDir); err != nil {
		log.Fatalf("Failed to create storage directory: %v", err)
	}

	createExampleFiles()

	tlsConfig := generateTLSConfig()

	listener, err := quic.ListenAddr(serverAddr, tlsConfig, nil)
	if err != nil {
		log.Fatalf("Failed to start QUIC listener: %v", err)
	}
	defer listener.Close()

	fmt.Printf("QUIC File Transfer Server running on %s\n", serverAddr)
	fmt.Printf("Storage directory: %s\n", storageDir)

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			log.Printf("Connection accept error: %v", err)
			continue
		}
		fmt.Printf("Client connected: %s\n", conn.RemoteAddr())
		go handleConnection(conn)
	}
}

func handleConnection(conn quic.Connection) {
	defer func() {
		fmt.Printf("Client disconnected: %s\n", conn.RemoteAddr())
	}()

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		go handleStream(stream)
	}
}

func handleStream(stream quic.Stream) {
	defer stream.Close()

	// Read command (10 bytes)
	cmdBuf := make([]byte, cmdSize)
	_, err := io.ReadFull(stream, cmdBuf)
	if err != nil {
		log.Printf("Command read error: %v", err)
		return
	}

	cmd := strings.TrimRight(string(cmdBuf), "\x00")
	cmd = strings.TrimSpace(cmd)

	switch cmd {
	case "UP":
		handleUpload(stream)
	case "DOWN":
		handleDownload(stream)
	case "LIST":
		handleList(stream)
	default:
		log.Printf("Unknown command: '%s'", cmd)
	}
}

func handleUpload(stream quic.Stream) {
	// Read filename (256 bytes)
	fnBuf := make([]byte, filenameSize)
	_, err := io.ReadFull(stream, fnBuf)
	if err != nil {
		log.Printf("Filename read error: %v", err)
		return
	}

	fileName := strings.TrimRight(string(fnBuf), "\x00")
	fileName = filepath.Base(fileName) // prevent path traversal

	filePath := filepath.Join(storageDir, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("File create error: %v", err)
		return
	}
	defer file.Close()

	n, err := io.Copy(file, stream)
	if err != nil && err != io.EOF {
		log.Printf("File receive error: %v", err)
		return
	}

	fmt.Printf("Received '%s': %d bytes\n", fileName, n)
}

func handleDownload(stream quic.Stream) {
	// Read filename (256 bytes)
	fnBuf := make([]byte, filenameSize)
	_, err := io.ReadFull(stream, fnBuf)
	if err != nil {
		log.Printf("Filename read error: %v", err)
		return
	}

	fileName := strings.TrimRight(string(fnBuf), "\x00")
	fileName = filepath.Base(fileName)

	filePath := filepath.Join(storageDir, fileName)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		// Send error
		errMsg := make([]byte, sizeField)
		copy(errMsg, []byte("ERROR"))
		stream.Write(errMsg)
		return
	}

	// Send file size (20 bytes, padded)
	sizeStr := strconv.FormatInt(fileInfo.Size(), 10)
	sizeMsg := make([]byte, sizeField)
	copy(sizeMsg, []byte(sizeStr))
	stream.Write(sizeMsg)

	// Wait for READY signal
	readyBuf := make([]byte, 5)
	_, err = io.ReadFull(stream, readyBuf)
	if err != nil {
		log.Printf("Ready signal read error: %v", err)
		return
	}

	// Send file content
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("File open error: %v", err)
		return
	}
	defer file.Close()

	n, err := io.Copy(stream, file)
	if err != nil {
		log.Printf("File send error: %v", err)
		return
	}

	fmt.Printf("Sent '%s': %d bytes\n", fileName, n)
}

func handleList(stream quic.Stream) {
	files, err := os.ReadDir(storageDir)
	if err != nil {
		log.Printf("Directory read error: %v", err)
		return
	}

	var lines []string
	for _, file := range files {
		if !file.IsDir() {
			info, err := file.Info()
			if err != nil {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s (%d bytes)", file.Name(), info.Size()))
		}
	}

	result := strings.Join(lines, "\n")
	stream.Write([]byte(result))

	fmt.Printf("Sent file list (%d files)\n", len(lines))
}

func generateTLSConfig() *tls.Config {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("Key generation failed: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("Certificate generation failed: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatalf("Key marshal failed: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		log.Fatalf("TLS key pair failed: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-file-transfer"},
	}
}

func ensureDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

func createExampleFiles() {
	files := map[string]string{
		"server_file1.txt": "This is a test file available on the server for download.",
		"server_file2.txt": "Another server-side file.\nContains multiple lines.\nReady for download testing.",
	}

	for name, content := range files {
		filePath := filepath.Join(storageDir, name)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				log.Printf("Example file creation failed %s: %v", name, err)
			}
		}
	}
}
