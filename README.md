# QUIC File Transfer (Go)

A file transfer system built on the QUIC protocol using [quic-go](https://github.com/quic-go/quic-go). Includes both a client and server for uploading, downloading, and listing files over QUIC streams.

## Features

- **File Upload** — Transfer files from client to server over a QUIC stream
- **File Download** — Download files from the server with size verification
- **File Listing** — Query the server for available files with sizes
- **Self-signed TLS** — Server generates certificates at startup (no manual cert setup)
- **Interactive CLI** — Menu-driven client interface

## Project Structure

```
quic_go_client/
├── client/
│   └── main.go          # QUIC client with upload/download/list
├── server/
│   └── main.go          # QUIC server with file handling
├── go.mod               # Go module definition
├── .gitignore
└── README.md
```

## Requirements

- Go 1.21+
- `github.com/quic-go/quic-go`

## Getting Started

### Install dependencies

```bash
go mod tidy
```

### Run the server

```bash
go run ./server
```

The server starts on `localhost:4242` and stores files in `./server_files/`.

### Run the client

In a separate terminal:

```bash
go run ./client
```

The client connects to `localhost:4242` and provides an interactive menu:

```
QUIC Client Menu:
1. Upload file to server
2. Download file from server
3. List files on server
4. Exit
```

## Protocol

Communication uses QUIC bidirectional streams with fixed-size fields:

| Field    | Size      | Description                      |
|----------|-----------|----------------------------------|
| Command  | 10 bytes  | `UP`, `DOWN`, or `LIST` (null-padded) |
| Filename | 256 bytes | Null-padded filename (UP/DOWN only) |

### Upload (`UP`)
1. Client sends: `[command 10B] [filename 256B] [file content...]`
2. Client closes stream after transfer

### Download (`DOWN`)
1. Client sends: `[command 10B] [filename 256B]`
2. Server responds: `[file size 20B]`
3. Client sends: `READY`
4. Server sends: `[file content...]`

### List (`LIST`)
1. Client sends: `[command 10B]`
2. Server responds: file list as text (one file per line)

## License

MIT License
