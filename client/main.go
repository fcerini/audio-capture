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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pion/rtp"
)

const (
	// PulseAudio settings for L16 audio
	sampleRate = 48000 // Audio sample rate
	channels   = 1     // Number of audio channels (1 for mono)
	bitDepth   = 16    // Bit depth (16 for s16be)

	// RTP settings for L16 (Linear PCM)
	payloadTypeL16 = 96    // Dynamic payload type for L16
	rtpClockRate   = 48000 // Clock rate for L16 must match sample rate
	mtu            = 1500  // Maximum Transmission Unit for RTP packets
)

func main() {
	// 1. Validate command-line arguments
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <URL> <destination_host:port>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample: %s 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' 127.0.0.1:5004\n", os.Args[0])
		os.Exit(1)
	}
	url := os.Args[1]
	destination := os.Args[2]

	// Seed random number generator
	rand.Seed(time.Now().UnixNano())

	// 2. Create a unique virtual PulseAudio sink for this instance
	sinkName := fmt.Sprintf("rtp-stream-%d", rand.Intn(100000))
	log.Printf("🎧 Creating PulseAudio sink: %s", sinkName)
	moduleIndex, err := exec.Command("pactl", "load-module", "module-null-sink", fmt.Sprintf("sink_name=%s", sinkName)).Output()
	if err != nil {
		log.Fatalf("❌ Failed to create PulseAudio sink: %v. Make sure PulseAudio is running.", err)
	}
	moduleIndexStr := strings.TrimSpace(string(moduleIndex))

	// TODO: Mmmm Si creo un profile nuevo, Firefox no arranca youtube...

	// 3. Create a temporary Firefox profile in the user's home directory to avoid Snap confinement issues.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("❌ Failed to get user home directory: %v", err)
	}
	profileDir, err := os.MkdirTemp(homeDir, "firefox-profile-*")
	if err != nil {
		log.Fatalf("❌ Failed to create temporary profile directory in home: %v", err)
	}
	log.Printf("🦊 Created temporary Firefox profile in: %s", profileDir)
	// Defer cleanup of the profile directory for when the program exits
	defer func() {
		log.Printf("🦊 Removing temporary Firefox profile: %s", profileDir)
		if err := os.RemoveAll(profileDir); err != nil {
			log.Printf("⚠️  Failed to remove profile directory %s: %v", profileDir, err)
		}
	}()

	// Add a delay to allow the sink to initialize fully before use.
	log.Println("⏳ Waiting for PulseAudio sink to initialize...")
	time.Sleep(2 * time.Second)

	// 4. Set up graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// 5. Launch Firefox in a new, isolated instance, directing its audio to our sink
	log.Printf("🚀 Launching isolated Firefox instance with URL: %s", url)
	//	firefoxCmd := exec.Command("firefox", "--new-instance", "--profile", profileDir, "--new-window", url)
	firefoxCmd := exec.Command("firefox", "--new-instance", "--new-window", url)
	firefoxCmd.Env = append(os.Environ(), fmt.Sprintf("PULSE_SINK=%s", sinkName))
	if err := firefoxCmd.Start(); err != nil {
		log.Fatalf("❌ Failed to start Firefox: %v", err)
	}

	// 6. Start audio capture and streaming from the new sink's monitor
	pulseDevice := fmt.Sprintf("%s.monitor", sinkName)
	log.Printf("🎤 Starting audio capture from PulseAudio source: %s", pulseDevice)
	log.Printf("📡 Streaming L16 PCM audio to: %s", destination)
	parecCmd, err := startStreaming(destination, pulseDevice)
	if err != nil {
		log.Fatalf("❌ Failed to start streaming: %v", err)
	}

	// 7. Wait for shutdown signal and clean up
	<-sigs
	log.Println("\n🛑 Received shutdown signal. Cleaning up...")

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

	log.Printf("🎧 Unloading PulseAudio module: %s", moduleIndexStr)
	if _, err := strconv.Atoi(moduleIndexStr); err == nil {
		if err := exec.Command("pactl", "unload-module", moduleIndexStr).Run(); err != nil {
			log.Printf("⚠️ Failed to unload PulseAudio module %s: %v", moduleIndexStr, err)
		}
	}
	// The deferred function for profile cleanup will run automatically now.

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
	parecCmd := exec.Command("parec", "--format=s16be", fmt.Sprintf("--rate=%d", sampleRate), fmt.Sprintf("--channels=%d", channels), fmt.Sprintf("--device=%s", pulseDevice))

	stdout, err := parecCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe from parec: %w", err)
	}

	stderr, err := parecCmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe from parec: %w", err)
	}

	if err := parecCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start parec: %w", err)
	}

	// Goroutine to log any errors from parec
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("parec stderr: %s", scanner.Text())
		}
	}()

	// Start a goroutine to read audio data, packetize, and send
	go func() {
		defer conn.Close()
		bufferSize := (sampleRate / 50) * channels * (bitDepth / 8)
		reader := bufio.NewReaderSize(stdout, bufferSize)

		for {
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

			samples := uint32(rtpClockRate / 50)
			packets := packetizer.Packetize(pcmData, samples)

			for _, p := range packets {
				data, err := p.Marshal()
				if err != nil {
					log.Printf("❌ Failed to marshal RTP packet: %v", err)
					continue
				}
				_, err = conn.Write(data)
				if err != nil {
					fmt.Printf("!")
				}
			}
		}
	}()

	return parecCmd, nil
}

type pcmPayloader struct{}

func (p *pcmPayloader) Payload(mtu uint16, payload []byte) [][]byte {
	var out [][]byte
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
