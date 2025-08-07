# Audio Capture Server

This server listens for RTP audio streams on UDP port 4000.

## Running the server

1.  Navigate to the `server` directory:
    ```bash
    cd server
    ```
2.  Tidy the dependencies
    ```bash
    go mod tidy
    ```
3.  Run the server:
    ```bash
    go run main.go
    ```

The server will print a message indicating that it is listening for RTP packets.
