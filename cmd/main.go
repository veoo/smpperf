package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"golang.org/x/time/rate"

	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
)

const (
	dst = "447582668509"
	src = "447582668506"
)

var numMessages = flag.Int("n", 5000, "number of messages")
var msgRate = flag.Int("r", 20, "rate of sending messages in msg/s")
var wait = flag.Int("w", 60, "seconds to wait for message receipts")
var user = flag.String("u", "user", "user of SMPP server")
var password = flag.String("p", "", "password of SMPP server")
var host = flag.String("h", "127.0.0.1:2775", "host of SMPP server")
var purge = flag.Bool("purge", false, "waits to receive any pending receipts")

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
		signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGQUIT)
		sig := <-signalChannel
		fmt.Println(sig, "signal caught, exiting.")
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
		fmt.Println("Error connecting:", c.Error())
	}
	closeTransceiverOnSignal(transceiver)

	time.Sleep(time.Duration(*wait) * time.Second)
	fmt.Println("receiptCount:", receiptCount.Val())
}

func sendMessages(numMessages int) {
	successCount := NewSafeInt(0)
	sendErrorCount := NewSafeInt(0)
	unknownRespCount := NewSafeInt(0)
	connErrorCount := NewSafeInt(0)

	transceiverHandler := func(p pdu.Body) {
		switch p.Header().ID {
		case pdu.DeliverSMID:
			// TODO: check here the resp data is correct
			go successCount.Increment()
		case pdu.UnbindID:
			fmt.Println("They are unbinding me :(")
		case pdu.SubmitSMRespID:
			// Fix your stuff fiorix
		default:
			go fmt.Println(p.Header().ID.String(), p.Header().Status.Error())
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
		fmt.Println("Error connecting:", c.Error())
	}
	closeTransceiverOnSignal(transceiver)

	go func() {
		for c := range conn {
			if c.Error() != nil {
				log.Info("SMPP connection status: ", c.Status(), c.Error())
				go connErrorCount.Increment()
			}
		}
	}()

	req := &smpp.ShortMessage{
		Src:      src,
		Dst:      dst,
		Text:     pdutext.Raw("text"),
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
				}
			}()
		}
		fmt.Println("Time elapsed sending:", time.Since(now))
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
			fmt.Println("Time since start:", time.Since(now))
			fmt.Println("successCount:", successCount.Val())
			fmt.Println("unknownRespCount:", unknownRespCount.Val())
			fmt.Println("sendErrorCount:", sendErrorCount.Val())
			fmt.Println("connErrorCount:", connErrorCount.Val())
		}
	}
	fmt.Println("Time elapsed receiving:", time.Since(now))
	fmt.Println("successCount:", successCount.Val())
	fmt.Println("unknownRespCount:", unknownRespCount.Val())
	fmt.Println("sendErrorCount:", sendErrorCount.Val())
	fmt.Println("connErrorCount:", connErrorCount.Val())
}

func main() {
	flag.Parse()
	if *numMessages <= 0 {
		panic("invalid value for number of messages")
	}
	if *purge {
		purgeReceipts()
	} else {
		sendMessages(*numMessages)
	}
}
