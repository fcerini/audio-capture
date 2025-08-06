package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/pion/rtp"
)

const (
	// PulseAudio settings for L16 audio
	// We capture at 48kHz stereo to match the audio source.
	sampleRate = 48000 // Audio sample rate
	channels   = 2    // Number of audio channels (2 for stereo)
	bitDepth   = 16   // Bit depth (16 for s16be)

	// RTP settings for L16 (Linear PCM)
	payloadTypeL16 = 96   // Dynamic payload type for L16
	rtpClockRate   = 48000 // Clock rate for L16 must match sample rate
	mtu            = 1500 // Maximum Transmission Unit for RTP packets
)

func main() {
	// 1. Validate command-line arguments
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <URL> <destination_host:port> <pulseaudio_monitor_source>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample: %s 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' 127.0.0.1:5004 alsa_output.pci-0000_00_1f.3.analog-stereo.monitor\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nTo find your monitor source, run: pactl list sources short | grep .monitor\n")
		os.Exit(1)
	}
	url := os.Args[1]
	destination := os.Args[2]
	pulseDevice := os.Args[3]

	// Seed random number generator
	rand.Seed(time.Now().UnixNano())

	// 2. Set up graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// 3. Launch Firefox
	log.Printf("🚀 Launching Firefox with URL: %s", url)
	firefoxCmd := exec.Command("firefox", "--new-window", url)
	if err := firefoxCmd.Start(); err != nil {
		log.Fatalf("❌ Failed to start Firefox: %v", err)
	}

	// 4. Start audio capture and streaming
	log.Printf("🎤 Starting audio capture from PulseAudio source: %s", pulseDevice)
	log.Printf("📡 Streaming L16 PCM audio to: %s", destination)
	parecCmd, err := startStreaming(destination, pulseDevice)
	if err != nil {
		log.Fatalf("❌ Failed to start streaming: %v", err)
	}

	// 5. Wait for shutdown signal
	<-	sigs
	log.Println("\n🛑 Received shutdown signal. Cleaning up...")

	// 6. Clean up processes
	if firefoxCmd.Process != nil {
		log.Println("🔥 Terminating Firefox...")
		if err := firefoxCmd.Process.Kill(); err != nil {
			log.Printf("⚠️  Failed to kill Firefox process: %v", err)
		}
	}
	if parecCmd.Process != nil {
		log.Println("🔥 Terminating PulseAudio recorder (parec)...")
		if err := parecCmd.Process.Kill(); err != nil {
			log.Printf("⚠️  Failed to kill parec process: %v", err)
		}
	}
	log.Println("✅ Cleanup complete. Exiting.")
}

// startStreaming sets up the RTP connection and starts the `parec` process to capture and stream audio.
func startStreaming(destination, pulseDevice string) (*exec.Cmd, error) {
	// Set up UDP connection for RTP
	udpAddr, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve UDP address: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial UDP: %w", err)
	}

	// Create RTP packetizer for L16 audio
	packetizer := rtp.NewPacketizer(
		uint16(mtu),
		payloadTypeL16,
		rand.Uint32(),
		&pcmPayloader{},
		rtp.NewRandomSequencer(),
		rtpClockRate,
	)

	// Start PulseAudio recorder `parec`
	// Format: s16be = Signed 16-bit Big-Endian PCM (Network Byte Order)
	parecCmd := exec.Command("parec", "--format=s16be", fmt.Sprintf("--rate=%d", sampleRate), fmt.Sprintf("--channels=%d", channels), fmt.Sprintf("--device=%s", pulseDevice))

	stdout, err := parecCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe from parec: %w", err)
	}

	if err := parecCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start parec: %w", err)
	}

	// Start a goroutine to read audio data, packetize, and send
	go func() {
		defer conn.Close()
		// Buffer size for 20ms of audio data: 48000 samples/sec * 2 channels * 16 bits/sample / 8 bits/byte * 0.020 sec = 3840 bytes
		bufferSize := (sampleRate / 50) * channels * (bitDepth / 8)
		reader := bufio.NewReaderSize(stdout, bufferSize)

		for {
			// Read a chunk of raw PCM audio data
			pcmData := make([]byte, bufferSize)
			n, err := io.ReadFull(reader, pcmData)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				log.Println("👂 Audio stream ended.")
				return
			}
			if err != nil {
				log.Printf("❌ Error reading from parec stdout: %v", err)
				return
			}
			if n == 0 {
				continue
			}

			// The number of "samples" for RTP is based on the RTP clock rate.
			// For 20ms of data: 48000 samples/sec * 0.020 sec = 960 samples
			samples := uint32(rtpClockRate / 50)
			packets := packetizer.Packetize(pcmData, samples)

			// Send the RTP packets over the UDP connection
			for _, p := range packets {
				data, err := p.Marshal()
					if err != nil {
						log.Printf("❌ Failed to marshal RTP packet: %v", err)
						continue
					}
					if _, err := conn.Write(data); err != nil {
						log.Printf("❌ Failed to send RTP packet: %v", err)
					}
				}
			}
		}()

		return parecCmd, nil
}

// pcmPayloader implements the rtp.Payloader interface for raw PCM data.
// It simply chunks the payload based on the MTU.
type pcmPayloader struct{}

func (p *pcmPayloader) Payload(mtu uint16, payload []byte) [][]byte {
	var out [][]byte

	// Split into MTU-sized chunks if necessary
	for len(payload) > 0 {
		chunkSize := len(payload)
		if chunkSize > int(mtu) {
			chunkSize = int(mtu)
		}
		out = append(out, payload[:chunkSize])
		payload = payload[chunkSize:]
	}

	return out
}