# Audio Capture Client

This directory contains the client-side code for capturing and streaming audio.

## Capturing Audio with `parec`

The `parec` command is a utility from the PulseAudio sound system (default on Ubuntu) used to record audio from the command line.

Its role is to capture raw audio data from a source (like a microphone) and print it to standard output. We can then pipe this raw data to another tool (like `ffmpeg`) to encode it and send it over the network.

### Example Streaming Command

Here is a complete example of how to capture audio with `parec` and stream it as an RTP stream to the server:

```bash
parec --format=s16le | ffmpeg -f s16le -ar 8000 -ac 1 -i - -c:a pcm_mulaw -f rtp rtp://<SERVER_IP>:4000?payload_type=0
```

**Breakdown of the `parec` part:**
*   `parec`: The command to start recording.
*   `--format=s16le`: Specifies the output format as Signed 16-bit Little-Endian PCM, a common uncompressed format.
*   `|`: The pipe, which sends the raw audio data from `parec` to `ffmpeg`.

### How to Select a Microphone

To see a list of all available microphones and other audio sources, run:
```bash
pactl list sources
```
Look for the `Name:` field in the output (e.g., `alsa_input.pci-0000_00_1f.3.analog-stereo`).

To use a specific microphone, use the `-d` (device) flag:
```bash
parec -d <DEVICE_NAME> --format=s16le | ffmpeg ...
```

If you don't specify a device, the system's default input will be used.


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
