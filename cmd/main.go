package main

import (
	"flag"
	"fmt"
	"sync"
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

	transceiver := &smpp.Transceiver{
		Addr:        *host,
		User:        *user,
		Passwd:      *password,
		Handler:     transceiverHandler,
		RespTimeout: 10 * time.Second,
		EnquireLink: 1 * time.Second,
	}

	conn := transceiver.Bind() // make persistent connection.
	defer transceiver.Close()
	for c := range conn {
		if c.Error() == nil {
			break
		}
		fmt.Println("Error connecting:", c.Error())
	}

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
	for i := 0; i < *wait*10; i += 1 {
		time.Sleep(100 * time.Millisecond)
		if successCount.Val()+unknownRespCount.Val()+sendErrorCount.Val() >= numMessages {
			break
		}
		// Every minute print a progress
		if i%100 == 0 {
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
	sendMessages(*numMessages)
}
