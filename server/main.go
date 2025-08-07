package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/pion/rtp"
)

const (
	// Port to listen on
	listenPort = 6001
)

func main() {
	// Create a UDP listener
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: listenPort})
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Printf("Listening for RTP on 0.0.0.0:%d\n", listenPort)

	// Channel to handle Ctrl+C signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Map to store clients
	clients := make(map[string]*net.UDPAddr)

	go func() {
		buf := make([]byte, 1600)
		for {
			n, addr, err := listener.ReadFromUDP(buf)
			if err != nil {
				fmt.Printf("Error reading from UDP: %v\n", err)
				continue
			}

			// Check if the client is new
			if _, ok := clients[addr.String()]; !ok {
				fmt.Printf("New client connected: %s\n", addr.String())
				clients[addr.String()] = addr
			}

			// Create a new RTP packet
			packet := &rtp.Packet{}
			if err := packet.Unmarshal(buf[:n]); err != nil {
				fmt.Printf("Error unmarshalling RTP packet: %v\n", err)
				continue
			}

			// Print some info about the packet
			fmt.Printf("Received RTP packet from %s: PT=%d, Seq=%d, TS=%d, Len=%d\n",
				addr.String(), packet.Header.PayloadType, packet.Header.SequenceNumber, packet.Header.Timestamp, len(packet.Payload))

		}
	}()

	// Wait for Ctrl+C
	<-sigs
	fmt.Println("Shutting down server...")
}
