# Real-time Audio Capture and RTP Streamer

This Go program captures audio from a specific application (Firefox) on a Linux desktop using PulseAudio and streams it in real-time to a destination using the Real-time Transport Protocol (RTP).

It works by creating a dedicated, virtual audio "sink" in PulseAudio, launching Firefox with its audio output redirected to this sink, and then capturing the audio from the sink's "monitor" source.

## Features

- **Automatic Audio Isolation:** Creates a virtual PulseAudio sink to capture audio specifically from Firefox, without capturing all system audio.
- **Launches Firefox:** Opens Firefox to a specified URL to act as the audio source.
- **High-Quality Audio:** Encodes the audio to **16-bit, 48kHz, Stereo Linear PCM (L16)** format.
- **RTP Streaming:** Packetizes the audio into RTP packets using the Pion library.
- **Graceful Shutdown:** Cleans up the Firefox process and PulseAudio sink on `Ctrl+C`.

## Requirements

- **Go:** Version 1.18 or later.
- **Linux Operating System:** With PulseAudio installed (standard on most modern desktops like Ubuntu, Fedora, etc.).
- **`pactl`:** The PulseAudio controller command-line tool (usually installed by default with PulseAudio).
- **Firefox:** The browser to be used as the audio source.

## Setup

1.  **Clone the repository or download the files.**

2.  **Install Go dependencies:**
    Open your terminal in the project directory and run:
    ```sh
    go mod tidy
    ```

3.  **Build the executable:**
    This will create an `audio-capture` binary in your project directory.
    ```sh
    go build
    ```

## Usage

Run the program from your terminal using the following format:

```sh
./audio-capture <URL> <DESTINATION_IP:PORT>
```

**Example:**

```sh
./audio-capture 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' 127.0.0.1:5004
```

- The program will launch Firefox to the YouTube URL.
- It will start capturing any audio produced by that Firefox instance.
- It will stream this audio via RTP to port `5004` on your local machine.
- Press `Ctrl+C` in the terminal to shut down the program cleanly.

## How to Listen to the Stream

You can use a media player like VLC or `ffplay` to listen to the RTP stream. To do this, you need a simple **SDP (Session Description Protocol)** file to describe the stream to the player.

1.  Create a file named `stream.sdp` with content like this:

    ```sdp
    v=0
    o=- 0 0 IN IP4 127.0.0.1
    s=L16 Audio Stream from Go
    c=IN IP4 127.0.0.1
    t=0 0
    m=audio 5004 RTP/AVP 96
    a=rtpmap:96 L16/48000/2
    ```

2.  **Important:** Make sure the `c=IN IP4` address and the `m=audio` port in the SDP file match the destination IP and port you are sending the stream to.

3.  Open the stream with your player:

    **Using VLC:**
    ```sh
    vlc stream.sdp
    ```

    **Using `ffplay`:**
    ```sh
    ffplay -protocol_whitelist file,rtp,udp -i stream.sdp
    ```

## How to Run the Included Tests

The `test/` directory contains scripts and SDP files to demonstrate the program. The test streams are configured to be sent to the IP address `172.21.100.46`.

**Instructions:**

1.  **Build the program** if you haven't already:
    ```sh
    go build
    ```

2.  **Choose a test to run (e.g., `test1`).**

3.  **Start the listener:** On the machine where you want to hear the audio (e.g., the one with IP `172.21.100.46`), open the corresponding SDP file in VLC. This tells VLC to wait for the stream.
    ```sh
    vlc test/stream1.sdp
    ```

4.  **Run the streaming script:** In the project's root directory on your Linux machine, run the test script. This will start the `audio-capture` program, which will launch Firefox and begin sending the audio.
    ```sh
    ./test/test1.sh
    ```

You should now hear the audio from the radio station in VLC.

To try the other test, simply use `stream2.sdp` and `test2.sh`.