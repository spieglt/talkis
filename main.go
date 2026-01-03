package main

import (
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/gordonklaus/portaudio"
	"github.com/stianeikeland/go-rpio/v4"
)

const (
	audioPort     = 7416
	heartbeatPort = 7417
	sampleRate    = 44100
	framesPerBuf  = 512
	usingGpio     = true
)

func main() {
	log.Println("No talkis!")

	// Prepare audio stream
	portaudio.Initialize()
	defer portaudio.Terminate()

	// Get mic stream
	micBuffer := make([]int16, framesPerBuf)
	inputAudioStream, err := getInputStream(micBuffer)
	if err != nil {
		log.Println("Error opening input stream:", err)
	}
	defer inputAudioStream.Close()

	// Get speaker stream
	speakerBuffer := make([]int16, framesPerBuf)
	outputAudioStream, err := getOutputStream(speakerBuffer)
	if err != nil {
		log.Println("Error opening output stream:", err)
	}
	defer outputAudioStream.Close()

	// Open GPIO ports
	err = rpio.Open()
	if err != nil {
		log.Printf("Could not open GPIO ports: %s\n", err)
	}
	defer rpio.Close()

	// Handle LEDs
	defer func() {
		redLight(false)
		greenLight(false)
	}()

	// Watch for button events
	button := updateButton()

	// Get local and peer IPs
	localIp := getLocalAddress()
	peerIps := getPeerAddresses()
	log.Printf("Local IP: %s, peer IPs: %v", localIp, peerIps)

	// Both peers listen for heartbeats
	localAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", localIp, heartbeatPort))

	if err != nil {
		log.Fatalf("Could not resolve UDP address: %s", err)
	}
	heartbeatListenerConn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		log.Fatalln("Error listening on UDP port:", err)
	}

	// Both listen to audio
	localAudioAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", localIp, audioPort))
	if err != nil {
		log.Fatalf("Could not resolve UDP address: %s", err)
	}
	audioListenerConn, err := net.ListenUDP("udp", localAudioAddr)
	if err != nil {
		log.Fatalln("Error listening on UDP port:", err)
	}
	go playAudio(outputAudioStream, audioListenerConn, speakerBuffer)

	// Track connections
	pool := &pool{
		connections: make(map[string]*connection),
		mutex:       sync.RWMutex{},
	}

	// Start streaming audio from microphone
	go pool.streamAudio(inputAudioStream, micBuffer, button)

	// Start receiving heartbeats
	go pool.listenToHeartbeats(heartbeatListenerConn)

	// Start looking for peers. Heartbeats will be sent as they're discovered.
	go pool.discoverPeers()

	log.Println("Waiting...")
	select {}
}

// NOTES
// why was audio coming out of same mic it goes into? because pi didn't have a mic plugged in and it was feeding audio out into audio in for some reason and sending it back over the network: fixed.
// discovery and authentication: tailscale? double nat means server. if not tailscale, need encryption, meaning https, meaning webrtc instead of udp and cert for server... too complicated. discover tailscale peers with cli if necessary.
// tailscale means we don't actually need client/server architecture, dialing UDP heartbeat port on all tailscale peers should allow discovery? each peer listens, each peer starts dialing, when each receives a response it sets peerAddr
