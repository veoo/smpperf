package main

import (
	"flag"
	"strings"

	"github.com/veoo/smpperf"
)

var numMessages = flag.Int("n", 5000, "number of messages")
var numSessions = flag.Int("s", 1, "number of sessions, messages will be distributed")
var windowSize = flag.Int("ws", 1, "number of concurrent messages to be sent")
var msgRate = flag.Int("r", 20, "rate of sending messages in msg/s")
var wait = flag.Int("w", 60, "seconds to wait for message receipts")
var user = flag.String("u", "user", "user of SMPP server")
var password = flag.String("p", "", "password of SMPP server")
var host = flag.String("h", "127.0.0.1:2775", "host of SMPP server")
var purge = flag.Bool("purge", false, "waits to receive any pending receipts")

var mode = flag.String("mode", "static", "Mode of destination address (static or dynamic)")
var dst = flag.Int("dst", 447582668509, "Destination address")
var src = flag.String("src", "447582668506", "Source address")
var verbose = flag.Bool("verbose", false, "Be verbose")

func main() {
	flag.Parse()
	messageText := strings.Join(flag.Args(), " ")
	if len(messageText) <= 0 {
		messageText = "text"
	}

	if *numMessages <= 0 {
		panic("invalid value for number of messages")
	}
	smpperf := &smpperf.SMPPerf{
		NumMessages: *numMessages,
		NumSessions: *numSessions,
		MessageRate: *msgRate,
		WindowSize:  *windowSize,
		MessageText: messageText,
		Wait:        *wait,
		User:        *user,
		Password:    *password,
		Host:        *host,
		Mode:        *mode,
		Dst:         *dst,
		Src:         *src,
		Verbose:     *verbose,
	}

	if *purge {
		smpperf.Purge()
	} else {
		smpperf.SendMessages()
	}
}
