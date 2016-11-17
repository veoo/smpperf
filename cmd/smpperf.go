package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
)

var numMessages = flag.Int("n", 5000, "number of messages")
var msgRate = flag.Int("r", 20, "rate of sending messages in msg/s")
var wait = flag.Int("w", 60, "seconds to wait for message receipts")
var user = flag.String("u", "user", "user of SMPP server")
var password = flag.String("p", "", "password of SMPP server")
var host = flag.String("h", "127.0.0.1:2775", "host of SMPP server")
var purge = flag.Bool("purge", false, "waits to receive any pending receipts")

var mode = flag.String("mode", "static", "Mode of destination address (static or dynamic)")
var dstStart = flag.Int("start", "", "Start msisdn")
var dst = flag.String("dst", "447582668509", "Destination address (when in static mode)")
var src = flag.String("src", "447582668506", "Source address")

type SafeInt struct {
	val int
	m   *sync.RWMutex
}

func NewSafeInt(n int) *SafeInt {
	return &SafeInt{val: n, m: &sync.RWMutex{}}
}

func (s *SafeInt) Increment() {
	s.m.Lock()
	defer s.m.Unlock()
	s.val += 1
}

func (s *SafeInt) Val() int {
	s.m.RLock()
	defer s.m.RUnlock()
	return s.val
}

func getTransceiver() *smpp.Transceiver {
	return &smpp.Transceiver{
		Addr:        *host,
		User:        *user,
		Passwd:      *password,
		RespTimeout: 10 * time.Second,
		EnquireLink: 1 * time.Second,
	}
}

func closeTransceiverOnSignal(trans *smpp.Transceiver) {
	go func() {
		signalChannel := make(chan os.Signal, 1)
		signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
		sig := <-signalChannel
		log.Println("WARNING:", sig, "signal caught, exiting.")
		trans.Close()
		os.Exit(0)
	}()
}

func purgeReceipts() {
	receiptCount := NewSafeInt(0)

	transceiverHandler := func(p pdu.Body) {
		switch p.Header().ID {
		case pdu.DeliverSMID:
			go receiptCount.Increment()
		}
	}

	transceiver := getTransceiver()
	transceiver.Handler = transceiverHandler

	conn := transceiver.Bind() // make persistent connection.
	defer transceiver.Close()
	for c := range conn {
		if c.Error() == nil {
			break
		}
		log.Println("ERROR: Error connecting:", c.Error())
	}
	closeTransceiverOnSignal(transceiver)

	time.Sleep(time.Duration(*wait) * time.Second)
	log.Println("receiptCount:", receiptCount.Val())
}

func sendMessages(numMessages int, messageText string) {
	successCount := NewSafeInt(0)
	sendErrorCount := NewSafeInt(0)
	unknownRespCount := NewSafeInt(0)
	connErrorCount := NewSafeInt(0)
	submittedCount := NewSafeInt(0)

	transceiverHandler := func(p pdu.Body) {
		switch p.Header().ID {
		case pdu.DeliverSMID:
			// TODO: check here the resp data is correct
			go successCount.Increment()
		case pdu.UnbindID:
			log.Println("ERROR: They are unbinding me :(")
		case pdu.SubmitSMRespID:
			// Fix something florix?
		default:
			go log.Println(p.Header().ID.String(), p.Header().Status.Error())
			go unknownRespCount.Increment()
		}
	}

	transceiver := getTransceiver()
	transceiver.Handler = transceiverHandler

	conn := transceiver.Bind() // make persistent connection.
	defer transceiver.Close()
	for c := range conn {
		if c.Error() == nil {
			break
		}
		log.Println("ERROR: connection failed:", c.Error())
	}
	closeTransceiverOnSignal(transceiver)

	go func() {
		for c := range conn {
			if c.Error() != nil {
				log.Println("ERROR: SMPP connection status: ", c.Status(), c.Error())
				go connErrorCount.Increment()
			}
		}
	}()

	req := &smpp.ShortMessage{
		Src:      *src,
		Dst:      *dst,
		Text:     pdutext.Raw(messageText),
		Register: smpp.FinalDeliveryReceipt,
	}
	go func() {
		now := time.Now()
		burstLimit := 100
		rl := rate.NewLimiter(rate.Limit(*msgRate), burstLimit)
		for i := 0; i < numMessages; i += 1 {
			r := rl.Reserve()
			if r == nil {
				panic("Something is wrong with rate limiter")
			}
			time.Sleep(r.Delay())
			go func() {
				_, err := transceiver.Submit(req)
				if err != nil {
					go sendErrorCount.Increment()
				} else {
					submittedCount.Increment()
				}
			}()
		}
		log.Println("Time elapsed sending:", time.Since(now))
	}()

	now := time.Now()
	loopTime := 100 * time.Millisecond
	loops := *wait * int(time.Second/loopTime)

	for i := 0; i < loops; i += 1 {
		time.Sleep(loopTime)
		if successCount.Val()+unknownRespCount.Val()+sendErrorCount.Val() >= numMessages {
			break
		}
		// Every 10 secs print a progress
		if i%100 == 0 {
			log.Println("Time since start:", time.Since(now))
			log.Println("successCount:", successCount.Val())
			log.Println("unknownRespCount:", unknownRespCount.Val())
			log.Println("sendErrorCount:", sendErrorCount.Val())
			log.Println("connErrorCount:", connErrorCount.Val())
			log.Println("Submitted Messages:", submittedCount.Val())
		}
	}
	if successCount.Val()+unknownRespCount.Val()+sendErrorCount.Val() < numMessages {
		log.Println("WARNING: Waiting time is over and didn't receive enough responses.")
	}

	log.Println("Time elapsed receiving:", time.Since(now))
	log.Println("successCount:", successCount.Val())
	log.Println("unknownRespCount:", unknownRespCount.Val())
	log.Println("sendErrorCount:", sendErrorCount.Val())
	log.Println("connErrorCount:", connErrorCount.Val())
}

func main() {
	flag.Parse()
	messageText := strings.Join(flag.Args(), " ")
	if len(messageText) > 0 {
		// all good
	} else {
		messageText = "text"
	}

	if *numMessages <= 0 {
		panic("invalid value for number of messages")
	}
	if *purge {
		purgeReceipts()
	} else {
		sendMessages(*numMessages, messageText)
	}
}
