package main

import (
	"sync/atomic"
	"time"

	"github.com/stianeikeland/go-rpio/v4"
)

const micOn = 0
const micOff = 1

func redLight(on bool) {
	if !usingGpio {
		return
	}
	pin := rpio.Pin(22)
	pin.Output()
	if on {
		pin.High()
	} else {
		pin.Low()
	}
}

func greenLight(on bool) {
	if !usingGpio {
		return
	}
	pin := rpio.Pin(12)
	pin.Output()
	if on {
		pin.High()
	} else {
		pin.Low()
	}
}

func updateButton() *atomic.Bool {
	if !usingGpio {
		return nil
	}
	pin := rpio.Pin(5)
	pin.Input()
	pin.PullUp()
	buttonPressed := atomic.Bool{}
	// Update button status 10 times/second
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			buttonPressed.Store(pin.Read() == micOn)
		}
	}()
	return &buttonPressed
}
