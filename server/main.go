package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/pion/rtp"
)

const (
	listenPort  = 6001
	sampleRate  = 48000 // Must match the client's sample rate
	bitDepth    = 16    // Must match the client's bit depth
	numChannels = 1     // Must match the client's channel count (1 for mono)
)

// Client holds the state for a single connected client, including its WAV file encoder.
type Client struct {
	encoder *wav.Encoder
	file    *os.File
}

func main() {
	// Create a UDP listener
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: listenPort})
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Printf("🎧 Listening for RTP audio on 0.0.0.0:%d\n", listenPort)
	fmt.Println("🔊 Saving incoming audio streams to .wav files...")

	// Channel to handle Ctrl+C signal for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Map to store clients, protected by a mutex for safe concurrent access
	clients := make(map[string]*Client)
	var clientsMutex sync.Mutex // Use a simple Mutex for clarity and safety

	// Start a goroutine to handle incoming packets
	go func() {
		buf := make([]byte, 1600) // MTU for RTP is usually around 1500
		for {
			n, addr, err := listener.ReadFromUDP(buf)
			if err != nil {
				// This error is expected when the listener is closed, so we can exit gracefully.
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				fmt.Printf("Error reading from UDP: %v\n", err)
				continue
			}

			// debug
			//fmt.Printf("%v: %v\n", addr.String(), buf[80:100])

			packet := &rtp.Packet{}
			if err := packet.Unmarshal(buf[:n]); err != nil {
				fmt.Printf("Error unmarshalling RTP packet from %s: %v\n", addr.String(), err)
				continue
			}

			// --- FIXED CLIENT LOOKUP AND CREATION ---
			// Lock the mutex to ensure exclusive access to the map.
			clientsMutex.Lock()

			client, ok := clients[addr.String()]
			if !ok {
				// If the client is new, create a WAV file and encoder for it.
				fmt.Printf("✅ New client connected: %s. Creating WAV file.\n", addr.String())

				// Sanitize address for a valid filename
				fileName := fmt.Sprintf("%s_%d.wav", strings.ReplaceAll(addr.String(), ":", "_"), time.Now().Unix())

				outFile, err := os.Create(fileName)
				if err != nil {
					fmt.Printf("Error creating WAV file for %s: %v\n", addr.String(), err)
					clientsMutex.Unlock() // Unlock before continuing
					continue
				}

				// Create a new WAV encoder and client struct
				encoder := wav.NewEncoder(outFile, sampleRate, bitDepth, numChannels, 1) // 1 = PCM
				client = &Client{
					encoder: encoder,
					file:    outFile,
				}
				clients[addr.String()] = client
			}

			// Unlock the mutex as soon as we're done with the map.
			clientsMutex.Unlock()
			// --- END OF FIX ---

			// Convert the s16be RTP payload into an audio buffer
			numSamples := len(packet.Payload) / 2 // 2 bytes per sample
			if numSamples == 0 {
				continue
			}

			samples := make([]int, numSamples)
			for i := 0; i < numSamples; i++ {
				// Read 2 bytes as a big-endian signed 16-bit integer
				sample := int16(binary.BigEndian.Uint16(packet.Payload[i*2 : (i*2)+2]))
				samples[i] = int(sample)
			}

			audioBuf := &audio.IntBuffer{
				Format: &audio.Format{
					NumChannels: numChannels,
					SampleRate:  sampleRate,
				},
				Data:           samples,
				SourceBitDepth: bitDepth,
			}

			// Write the audio buffer to the correct WAV file
			if err := client.encoder.Write(audioBuf); err != nil {
				fmt.Printf("Error writing to WAV file for %s: %v\n", addr.String(), err)
			}
		}
	}()

	// Wait for shutdown signal
	<-sigs
	fmt.Println("\n🛑 Shutting down server...")

	// Close the listener to stop the reader goroutine
	listener.Close()

	// Lock the map and close all open files and encoders
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	fmt.Println("💾 Closing all WAV files...")
	for addr, client := range clients {
		if err := client.encoder.Close(); err != nil {
			fmt.Printf("Error closing WAV encoder for %s: %v\n", addr, err)
		}
		if err := client.file.Close(); err != nil {
			fmt.Printf("Error closing WAV file for %s: %v\n", addr, err)
		}
		fmt.Printf("Closed file: %s\n", client.file.Name())
	}
	fmt.Println("✅ Cleanup complete.")
}
