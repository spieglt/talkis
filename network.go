package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
)

type connection struct {
	lastHeartbeat time.Time    // last time we've received a heartbeat
	micStream     *net.UDPConn // socket on which we send mic data to this peer
	heartbeatChan chan bool    // channel to cancel sending heartbeats once we lose contact
}

type pool struct {
	mutex       sync.RWMutex
	connections map[string]*connection
}

func (p *pool) checkHeartbeats() {
	for {
		p.mutex.Lock()
		connected := false
		for _, conn := range p.connections {
			if time.Since(conn.lastHeartbeat) > 2*time.Second {
				// Stop this micStream, but don't stop heartbeating to it or erase it from the list of connections
				if conn.micStream != nil {
					conn.micStream.Close()
					conn.micStream = nil
				}
			} else {
				connected = true
			}
		}
		greenLight(connected)
		redLight(!connected)
		p.mutex.Unlock()
		time.Sleep(2 * time.Second)
	}
}

func (p *pool) streamAudio(stream *portaudio.Stream, micBuffer []int16, button *atomic.Bool) {
	err := stream.Start()
	defer stream.Stop()
	if err != nil {
		log.Println("Error starting output stream:", err)
		return
	}

	log.Println("Sending audio...")
	for {
		if button == nil || !button.Load() { // If button status is not micOn
			time.Sleep(100 * time.Millisecond)
		} else {
			err := stream.Read()
			if err != nil {
				log.Println("Error reading audio from mic stream:", err)
				break
			}
			// Make copy of micBuffer
			backup := make([]int16, len(micBuffer))
			copy(backup, micBuffer)
			// Write audio to each of the connection's micStreams
			p.mutex.Lock()
			for _, conn := range p.connections {
				if conn.micStream != nil {
					_, err = conn.micStream.Write(int16SliceToBytes(backup))
					if err != nil {
						log.Println("Error writing audio to UDP socket:", err)
						break
					}
				}
			}
			p.mutex.Unlock()
		}
	}
}

func (p *pool) trackStreams(remoteAddr *net.UDPAddr) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if conn, ok := p.connections[remoteAddr.IP.String()]; ok {
		if conn.micStream == nil {
			log.Println("Adding micStream to existing connection", remoteAddr)
			remoteAddr.Port = audioPort
			peerSocket, err := net.DialUDP("udp", nil, remoteAddr)
			if err != nil {
				log.Println("Could not dial audio port:", err)
			}
			p.connections[remoteAddr.IP.String()].lastHeartbeat = time.Now()
			p.connections[remoteAddr.IP.String()].micStream = peerSocket
		}
	} else {
		log.Println("Got heartbeat, connected to", remoteAddr)
		remoteAddr.Port = audioPort
		peerSocket, err := net.DialUDP("udp", nil, remoteAddr)
		if err != nil {
			log.Println("Could not dial audio port:", err)
		}
		p.connections[remoteAddr.IP.String()] = &connection{
			lastHeartbeat: time.Now(),
			micStream:     peerSocket,
		}
	}
}

func heartbeat(peerConn *net.UDPConn, cancel chan bool) {
	for {
		select {
		case <-cancel:
			log.Printf("Lost contact, canceling heartbeat to %s\n", peerConn.RemoteAddr())
			return
		default:
			peerConn.Write([]byte{1})
			time.Sleep(1 * time.Second)
		}
	}
}

func (p *pool) listenToHeartbeats(listenerConn *net.UDPConn) {
	buffer := make([]byte, 1024)
	go p.checkHeartbeats()
	for {
		_, remoteAddr, err := listenerConn.ReadFromUDP(buffer)
		if err != nil {
			log.Println("Read UDP error:", err)
		}
		p.trackStreams(remoteAddr)
		// Set lastConnected for the peer sending this heartbeat
		for peerAddr, conn := range p.connections {
			if peerAddr == remoteAddr.IP.String() {
				conn.lastHeartbeat = time.Now()
			}
		}
		// Turn light green because we have at least one connection
		greenLight(true)
		redLight(false)
	}
}

func getLocalAddress() string {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		log.Println("Could not get tailscale IP address:", err)
		return ""
	}
	return strings.TrimSuffix(string(out), "\n")
}

func getPeerAddresses() []string {
	localAddress := getLocalAddress()
	out, err := exec.Command("tailscale", "status").Output()
	if err != nil {
		log.Println("Could not get tailscale IP address:", err)
		return []string{}
	}
	return extractPeerAddresses(out, localAddress)
}

func extractPeerAddresses(commandOutput []byte, localAddress string) []string {
	peerAddresses := []string{}
	ipPattern := regexp.MustCompile(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`)
	lines := strings.Split(string(commandOutput), "\n")
	for _, line := range lines {
		if !strings.Contains(line, localAddress) { // Don't match this device's address
			peerIp := ipPattern.Find([]byte(line))
			if peerIp != nil {
				peerAddresses = append(peerAddresses, string(peerIp))
			}
		}
	}
	return peerAddresses
}

func (p *pool) discoverPeers() {
	for {
		// See current Tailscale peers
		p.mutex.Lock()
		for _, currentPeer := range getPeerAddresses() {
			peerHeartbeatEndpoint := fmt.Sprintf("%s:%d", currentPeer, heartbeatPort)
			peerAddr, err := net.ResolveUDPAddr("udp", peerHeartbeatEndpoint)
			if err != nil {
				log.Fatalf("Could not resolve peer address: %s\n", err)
			}
			if conn, ok := p.connections[currentPeer]; ok {
				// If we know of the connection, but we're not sending heartbeats, start
				if conn.heartbeatChan == nil {
					log.Printf("Dialing %s\n", peerHeartbeatEndpoint)
					peerConn, err := net.DialUDP("udp", nil, peerAddr)
					if err != nil {
						log.Fatalln("Error dialing on UDP port:", err)
					}
					conn.heartbeatChan = make(chan bool)
					go heartbeat(peerConn, conn.heartbeatChan)
				}
			} else {
				// If we don't know the connection, add it and start sending heartbeats
				log.Printf("Dialing %s\n", peerHeartbeatEndpoint)
				peerConn, err := net.DialUDP("udp", nil, peerAddr)
				if err != nil {
					log.Fatalln("Error dialing on UDP port:", err)
				}
				heartbeatChan := make(chan bool)
				p.connections[currentPeer] = &connection{
					heartbeatChan: heartbeatChan,
				}
				go heartbeat(peerConn, heartbeatChan)
			}
		}
		p.mutex.Unlock()
		time.Sleep(1 * time.Second)
	}
}

func playAudio(stream *portaudio.Stream, conn *net.UDPConn, speakerBuffer []int16) {
	// Create a ring buffer with ~100ms of audio (about 4-5 packets)
	const bufferPackets = 5
	jitterBuffer := make(chan []int16, bufferPackets)

	// Fill buffer before starting
	for i := 0; i < bufferPackets-1; i++ {
		packet := make([]int16, framesPerBuf)
		buf := make([]byte, len(packet)*2)
		conn.ReadFromUDP(buf)
		bytesToInt16Slice(buf, packet)
		jitterBuffer <- packet
	}

	// Goroutine to continuously receive packets
	go func() {
		log.Println("Receiving audio...")
		for {
			packet := make([]int16, framesPerBuf)
			buf := make([]byte, len(packet)*2)
			_, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				log.Println("Error reading audio:", err)
				continue
			}
			bytesToInt16Slice(buf, packet)

			select {
			case jitterBuffer <- packet:
			default:
				// Buffer full, drop oldest (or this) packet
			}
		}
	}()

	err := stream.Start()
	if err != nil {
		log.Println("Error starting speaker stream:", err)
		return
	}
	defer stream.Stop()

	log.Println("Playing audio...")
	for {
		select {
		case packet := <-jitterBuffer:
			copy(speakerBuffer, packet)
			if err := stream.Write(); err != nil {
				log.Println("Error writing to speaker:", err)
			}
		case <-time.After(50 * time.Millisecond):
			// Timeout - write silence to prevent underrun
			for i := range speakerBuffer {
				speakerBuffer[i] = 0
			}
			stream.Write()
		}
	}
}
