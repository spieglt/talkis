package main

import "github.com/gordonklaus/portaudio"

func getInputStream(micBuffer []int16) (inputAudioStream *portaudio.Stream, err error) {
	inputDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		return
	}
	deviceParameters := portaudio.StreamDeviceParameters{
		Device:   inputDevice,
		Channels: 1,
		Latency:  inputDevice.DefaultLowInputLatency,
	}
	inputParameters := portaudio.StreamParameters{
		Input:           deviceParameters,
		SampleRate:      sampleRate,
		FramesPerBuffer: framesPerBuf,
	}
	return portaudio.OpenStream(inputParameters, micBuffer)
}

func getOutputStream(speakerBuffer []int16) (*portaudio.Stream, error) {
	outputDevice, err := portaudio.DefaultOutputDevice()
	if err != nil {
		return nil, err
	}
	deviceParameters := portaudio.StreamDeviceParameters{
		Device:   outputDevice,
		Channels: 1,
		Latency:  outputDevice.DefaultHighOutputLatency,
	}
	outputParameters := portaudio.StreamParameters{
		Output:          deviceParameters,
		SampleRate:      sampleRate,
		FramesPerBuffer: framesPerBuf,
	}
	return portaudio.OpenStream(outputParameters, speakerBuffer)
}

func bytesToInt16Slice(b []byte, out []int16) {
	for i := range out {
		// out[i] = int16(b[2*i]) | int16(b[2*i+1])<<8
		low := uint16(b[2*i])
		high := uint16(b[2*i+1])
		out[i] = int16(low | (high << 8))
	}
}

func int16SliceToBytes(data []int16) []byte {
	b := make([]byte, len(data)*2)
	for i, v := range data {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	return b
}
