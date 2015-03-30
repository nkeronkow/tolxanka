//
// sequencer.go
//
// This is synchronization logic for access to the hive. HandlerFunc's sent to
// seqHandle are wrapped in seq and are sent over a channel to the goroutine
// within initSequencer to be run one-by-one, with free access to hive
// resources.

package main

import (
	"code.google.com/p/go.net/websocket"
	"log"
)

type wsRegistration struct {
	Thread threadId
	Done   chan bool
	Sock   *websocket.Conn
}

type threadReq struct {
	Thread threadId
	Out    chan []byte
}

type hiveCmd func(*hive)

var regListener chan wsRegistration
var threadRequest chan threadReq
var hiveReqListener chan hiveCmd

// submit hiveCmd and block until complete.
func hiveReq(f hiveCmd) {
	done := make(chan bool)
	hiveReqListener <- func(h *hive) {
		f(h)
		done <- true
	}
	<-done
}

func initSequencer() {
	regListener = make(chan wsRegistration, 100)
	threadRequest = make(chan threadReq, 100)
	hiveReqListener = make(chan hiveCmd, 100)

	h := newHive()

	go func() {
		log.Println("Starting hive command sequencer.")
		for {
			select {
			case reg := <-regListener:
				// log.Println("sequencer: adding websocket listener")
				h.AddThreadListener(reg)

			case threadReq := <-threadRequest:
				// log.Println("sequencer: received page request from cache")
				t, ok := h.Threads[threadReq.Thread]
				if !ok {
					threadReq.Out <- []byte{}
					continue
				}

				threadReq.Out <- t.CreateThreadPage()

			case cmd := <-hiveReqListener:
				// log.Println("sequencer: hiveReq")
				cmd(h)
			}
		}
	}()

	h.recoverFromDatabase()
}
