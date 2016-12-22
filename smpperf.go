package smpperf

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdufield"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
)

type SMPPerf struct {
	NumMessages int
	NumSessions int
	MessageRate int
	MessageText string
	Wait        int
	User        string
	Password    string
	Host        string
	Mode        string
	Dst         int
	Src         string
	Verbose     bool
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

func getMessageID(p pdu.Body) string {
	tlv := p.TLVFields()
	if tlv == nil {
		return ""
	}
	field := tlv[pdufield.ReceiptedMessageID]
	if field == nil {
		return ""
	}
	return strings.TrimRight(string(field.Bytes()), "\x00")
}

func (s *SMPPerf) getTransceiver() *smpp.Transceiver {
	return &smpp.Transceiver{
		Addr:        s.Host,
		User:        s.User,
		Passwd:      s.Password,
		RespTimeout: 10 * time.Second,
		EnquireLink: 1 * time.Second,
	}
}

func (s *SMPPerf) SendMessages() {
	successCount := NewSafeInt(0)
	sendErrorCount := NewSafeInt(0)
	unknownRespCount := NewSafeInt(0)
	connErrorCount := NewSafeInt(0)
	submittedCount := NewSafeInt(0)
	msgIDToTransceiverID := NewConcurrentMap()

	transceivers := []*smpp.Transceiver{}
	for i := 0; i < s.NumSessions; i++ {
		transceiverID := strconv.Itoa(i)
		transceiverHandler := func(p pdu.Body) {
			switch p.Header().ID {
			case pdu.DeliverSMID:
				// TODO: check here the resp data is correct
				msgID := getMessageID(p)
				t, ok := msgIDToTransceiverID.Get(msgID)
				if !ok {
					log.Printf("ERROR: message %s not found in transceiver %v", msgID, transceiverID)
				} else if t != transceiverID {
					log.Printf("ERROR: message %s was received in wrong transceiver %s", msgID, transceiverID)
				}
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

		transceiver := s.getTransceiver()
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
		transceivers = append(transceivers, transceiver)
	}

	go func() {
		now := time.Now()
		burstLimit := 100
		rl := rate.NewLimiter(rate.Limit(s.MessageRate), burstLimit)
		var dest int
		currTransceiver := 0
		for i := 0; i < s.NumMessages; i += 1 {

			if s.Mode == "dynamic" {
				dest = s.Dst + i
			} else {
				dest = s.Dst
			}

			req := &smpp.ShortMessage{
				Src:      s.Src,
				Dst:      strconv.Itoa(dest),
				Text:     pdutext.Raw(s.MessageText),
				Register: smpp.FinalDeliveryReceipt,
			}

			if s.Verbose == true {
				log.Println("Sending to ", dest)
			}

			r := rl.Reserve()
			if r == nil {
				panic("Something is wrong with rate limiter")
			}
			time.Sleep(r.Delay())
			currTransceiver = (currTransceiver + 1) % s.NumSessions
			go func(transceiverIndex int) {
				sm, err := transceivers[transceiverIndex].Submit(req)
				if err != nil {
					go sendErrorCount.Increment()
				} else {
					transceiverID := strconv.Itoa(transceiverIndex)
					msgIDToTransceiverID.Set(sm.RespID(), transceiverID)
					submittedCount.Increment()
				}
			}(currTransceiver)
		}
		log.Println("Time elapsed sending:", time.Since(now))
	}()

	now := time.Now()
	loopTime := 100 * time.Millisecond
	loops := s.Wait * int(time.Second/loopTime)

	for i := 0; i < loops; i += 1 {
		time.Sleep(loopTime)
		if successCount.Val()+unknownRespCount.Val()+sendErrorCount.Val() >= s.NumMessages {
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
	if successCount.Val()+unknownRespCount.Val()+sendErrorCount.Val() < s.NumMessages {
		log.Println("WARNING: Waiting time is over and didn't receive enough responses.")
	}

	log.Println("Time elapsed receiving:", time.Since(now))
	log.Println("successCount:", successCount.Val())
	log.Println("unknownRespCount:", unknownRespCount.Val())
	log.Println("sendErrorCount:", sendErrorCount.Val())
	log.Println("connErrorCount:", connErrorCount.Val())
}

func (s *SMPPerf) Purge() {
	receiptCount := NewSafeInt(0)

	transceiverHandler := func(p pdu.Body) {
		switch p.Header().ID {
		case pdu.DeliverSMID:
			go receiptCount.Increment()
		}
	}

	transceiver := s.getTransceiver()
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

	time.Sleep(time.Duration(s.Wait) * time.Second)
	log.Println("receiptCount:", receiptCount.Val())
}
