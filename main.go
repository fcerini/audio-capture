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
	// PulseAudio settings
	// These must match the `parec` command arguments.
	sampleRate   = 44100 // Audio sample rate
	channels     = 2     // Number of audio channels (2 for stereo)
	bitDepth     = 16    // Bit depth (16 for s16le)
	audioBitrate = sampleRate * channels * bitDepth

	// RTP settings
	payloadType  = 0    // RTP payload type for PCMU (G.711 μ-law)
	rtpClockRate = 8000 // Clock rate for PCMU
	mtu          = 1500 // Maximum Transmission Unit for RTP packets
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
	log.Printf("📡 Streaming RTP to: %s", destination)
	parecCmd, err := startStreaming(destination, pulseDevice)
	if err != nil {
		log.Fatalf("❌ Failed to start streaming: %v", err)
	}

	// 5. Wait for shutdown signal
	<-sigs
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

	// Create RTP packetizer
	packetizer := rtp.NewPacketizer(
		uint16(mtu),
		payloadType,
		rand.Uint32(),
		&pcmToMuLawPayloader{},
		rtp.NewRandomSequencer(),
		rtpClockRate,
	)

	// Start PulseAudio recorder `parec`
	// Format: s16le = Signed 16-bit Little-Endian PCM
	parecCmd := exec.Command("parec", "--format=s16le", fmt.Sprintf("--rate=%d", sampleRate), fmt.Sprintf("--channels=%d", channels), fmt.Sprintf("--device=%s", pulseDevice))

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
		// Buffer size for 20ms of audio data
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

			// Packetize the audio data using our custom payloader
			// The number of "samples" for RTP is based on the RTP clock rate, not the raw audio sample rate.
			// For 20ms of data: 8000 samples/sec * 0.020 sec = 160 samples
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

// pcmToMuLawPayloader implements the rtp.Payloader interface.
// It converts raw 16-bit PCM audio into G.711 μ-law format.

type pcmToMuLawPayloader struct{}

func (p *pcmToMuLawPayloader) Payload(mtu uint16, payload []byte) [][]byte {
	var out [][]byte

	// Downsample and encode PCM to μ-law
	// This is a simple implementation. A more advanced version would use proper resampling.
	// Here we just pick one channel and decimate samples to fit the 8kHz clock rate.
	step := (sampleRate / rtpClockRate) * channels
	muLawPayload := make([]byte, 0, len(payload)/(2*step)+1)

	for i := 0; i+1 < len(payload); i += 2 * step {
		sample := int16(payload[i]) | int16(payload[i+1])<<8 // Little-endian 16-bit sample
		muLawPayload = append(muLawPayload, linearToMuLaw(sample))
	}

	// Split into MTU-sized chunks if necessary
	for len(muLawPayload) > 0 {
		chunkSize := len(muLawPayload)
		if chunkSize > int(mtu) {
			chunkSize = int(mtu)
		}
		out = append(out, muLawPayload[:chunkSize])
		muLawPayload = muLawPayload[chunkSize:]
	}

	return out
}

// linearToMuLaw converts a 16-bit linear PCM sample to an 8-bit μ-law sample.
// This is a standard G.711 μ-law encoding algorithm.
func linearToMuLaw(sample int16) byte {
	const (
		bias      = 0x84
		quantMask = 0x0F
		segMask   = 0x70
		mu        = 255
	)

	sign := sample >> 15 & 0x80
	if sign != 0 {
		sample = -sample
	}
	if sample > 32635 {
		sample = 32635
	}

	sample += bias
	exponent := 7
	for i := int16(0x4000); (sample & i) == 0; i >>= 1 {
		exponent--
	}

	position := (sample >> (exponent + 3)) & quantMask
	segment := byte(exponent << 4)

	return ^(byte(sign) | segment | byte(position))
}
