# Real-time Audio Capture and RTP Streamer

This Go program captures audio from a specific application (like Firefox) on a Linux desktop using PulseAudio and streams it in real-time to a destination server using the Real-time Transport Protocol (RTP).

## Features

- Opens Firefox to a specified URL to act as the audio source.
- Captures audio from a specific PulseAudio monitor source.
- Encodes the audio to **16-bit mono Linear PCM (L16)** format.
- Packetizes the audio into RTP packets using the Pion library with a dynamic payload type.
- Streams the RTP packets over UDP to a network destination.
- Handles graceful shutdown on `Ctrl+C`.

## Requirements

- **Go:** Version 1.18 or later.
- **Linux Operating System:** With PulseAudio installed (standard on most modern desktops like Ubuntu, Fedora, etc.).
- **`parec`:** The PulseAudio recorder command-line tool. This is usually installed by default with PulseAudio.
- **Firefox:** The browser to be used as the audio source.

## Setup

1.  **Clone the repository or download the files.**

2.  **Install Go dependencies:**
    Open your terminal in the project directory and run:
    ```sh
    go mod tidy
    ```

3.  **Find Your PulseAudio Monitor Source:**
    This is the most important step. You need to tell the program which audio output to "listen" to. On PulseAudio, every output device (like your speakers or headphones) has a corresponding "monitor" source.

    Run the following command to list all your audio sources:
    ```sh
    pactl list sources short
    ```

    Look for a line that ends in `.monitor`. The name will depend on your hardware. It will look something like this:

    ```
    5    alsa_output.pci-0000_00_1f.3.analog-stereo.monitor    module-alsa-card.c    s16le 2ch 44100Hz    SUSPENDED
    ```

    The name you need is the second field: `alsa_output.pci-0000_00_1f.3.analog-stereo.monitor`. Copy this string.

## Usage

Run the program from your terminal using the following format:

```sh
go run main.go <URL> <DESTINATION_IP:PORT> <YOUR_PULSEAUDIO_MONITOR_SOURCE>
```

**Example:**

```sh
go run main.go 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' 127.0.0.1:5004 alsa_output.pci-0000_00_1f.3.analog-stereo.monitor
```

- The program will launch Firefox to the YouTube URL.
- It will start capturing any audio produced by your system's default output.
- It will stream this audio via RTP to port `5004` on your local machine.
- Press `Ctrl+C` in the terminal to shut down the program, which will also close Firefox.

## How to Listen to the Stream

You can use a media player like `ffplay` (part of the FFmpeg suite) or VLC to listen to the RTP stream.

To do this, you need a simple **SDP (Session Description Protocol)** file to describe the stream to the player.

1.  Create a file named `stream.sdp` with the following content. The provided `stream.sdp` in this repository is already configured for this.

    ```sdp
    v=0
    o=- 0 0 IN IP4 127.0.0.1
    s=L16 Audio Stream from Go
    c=IN IP4 127.0.0.1
    t=0 0
    m=audio 5004 RTP/AVP 96
    a=rtpmap:96 L16/8000/1
    ```

2.  Make sure the `c=` line in the SDP file matches the destination IP address you used when running the program (e.g., `127.0.0.1` for local testing).
3.  Open the stream with your player:

    **Using `ffplay`:**
    ```sh
    ffplay -protocol_whitelist file,rtp,udp -i stream.sdp
    ```

    **Using VLC:**
    ```sh
    vlc stream.sdp
    ```

You should hear the audio from the Firefox tab playing in `ffplay` or VLC after a short delay.
