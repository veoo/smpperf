package smpperf

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdufield"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
	"github.com/veoo/go-smpp/smpp/pdu/pdutlv"
)

type Semaphore struct {
	tokens chan struct{}
}

func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		n = 5
	}
	return &Semaphore{
		tokens: make(chan struct{}, n),
	}
}

func (s *Semaphore) Increase() {
	s.tokens <- struct{}{}
}

func (s *Semaphore) Decrease() {
	<-s.tokens
}

func (s *Semaphore) IncreaseN(n int) {
	for i := 0; i < n; i++ {
		s.tokens <- struct{}{}
	}
}

type SMPPerf struct {
	NumMessages          int
	NumSessions          int
	MessageRate          int
	MessageText          string
	Wait                 int
	User                 string
	Password             string
	Host                 string
	Mode                 string
	Dst                  int
	Src                  string
	Verbose              bool
	transceivers         []*transceiverConn
	counters             *counters
	msgIDToTransceiverID *ConcurrentStringMap
}

type transceiverConn struct {
	transceiver *smpp.Transceiver
	err         error
	m           *sync.RWMutex
}

func (s *SMPPerf) setTransceiverErr(transceiverIndex int, err error) {
	s.transceivers[transceiverIndex].m.Lock()
	defer s.transceivers[transceiverIndex].m.Unlock()
	s.transceivers[transceiverIndex].err = err
	log.Println(err)
}

func (s *SMPPerf) checkTransceiverErr(transceiverIndex int) bool {
	s.transceivers[transceiverIndex].m.Lock()
	defer s.transceivers[transceiverIndex].m.Unlock()
	return s.transceivers[transceiverIndex].err != nil

}

func (s *SMPPerf) submitMessage(transceiverIndex int, req *smpp.ShortMessage) {
	if len(req.Text.Encode()) > 140 {
		s.submitLongMessage(transceiverIndex, req)
		return
	}
	sm, err := s.transceivers[transceiverIndex].transceiver.Submit(req)
	if err != nil {
		if err == smpp.ErrNotConnected {
			go s.counters.connErrorCount.Increment()
		} else {
			go s.counters.sendErrorCount.Increment()
		}
	} else {
		transceiverID := strconv.Itoa(transceiverIndex)
		s.msgIDToTransceiverID.Set(sm.RespID(), transceiverID)
	}
}

func (s *SMPPerf) submitLongMessage(transceiverIndex int, req *smpp.ShortMessage) {
	sms, err := s.transceivers[transceiverIndex].transceiver.SubmitLongMsg(req)
	if err != nil {
		if err == smpp.ErrNotConnected {
			go s.counters.connErrorCount.Increment()
		} else {
			go s.counters.sendErrorCount.Increment()
		}
	} else {
		for _, sm := range sms {
			transceiverID := strconv.Itoa(transceiverIndex)
			s.msgIDToTransceiverID.Set(sm.RespID(), transceiverID)
		}
	}
}

type counters struct {
	successCount     *SafeInt
	sendErrorCount   *SafeInt
	unknownRespCount *SafeInt
	connErrorCount   *SafeInt
	submittedCount   *SafeInt
	stateCounters    *ConcurrentIntMap
}

func newCounters() *counters {
	return &counters{
		successCount:     NewSafeInt(0),
		sendErrorCount:   NewSafeInt(0),
		unknownRespCount: NewSafeInt(0),
		connErrorCount:   NewSafeInt(0),
		submittedCount:   NewSafeInt(0),
		stateCounters:    NewConcurrentIntMap(),
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

func getMessageID(p pdu.Body) string {
	tlv := p.TLVFields()
	if tlv == nil {
		return ""
	}
	field := tlv[pdutlv.ReceiptedMessageID]
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

func (s *SMPPerf) countState(counterMap *ConcurrentIntMap, state pdutlv.MessageStateType) {
	if _, ok := counterMap.Get(state); !ok {
		counterMap.Create(state)
	}
	go counterMap.Increment(state)
	return
}

func (s *SMPPerf) isFinalState(state pdutlv.MessageStateType) bool {
	// Expired, Delivered, Undeliverable, Rejected, unsure about Deleted
	return state == pdutlv.Expired || state == pdutlv.Delivered || state == pdutlv.Undeliverable || state == pdutlv.Rejected || state == pdutlv.Deleted
}

func (s *SMPPerf) SendMessages() {
	s.counters = newCounters()
	s.transceivers = make([]*transceiverConn, s.NumSessions)
	s.msgIDToTransceiverID = NewConcurrentStringMap()

	for i := 0; i < s.NumSessions; i++ {
		transceiverID := strconv.Itoa(i)
		transceiverHandler := func(p pdu.Body) {
			switch p.Header().ID {
			case pdu.DeliverSMID:
				// TODO: check here the resp data is correct
				msgID := getMessageID(p)

				t, ok := s.msgIDToTransceiverID.Get(msgID)
				if !ok {
					log.Printf("ERROR: message %s not found in transceiver %v", msgID, transceiverID)
				} else if t != transceiverID {
					log.Printf("ERROR: message %s was received in wrong transceiver %s", msgID, transceiverID)
				}

				state := pdutlv.MessageStateType(p.TLVFields()[pdutlv.MessageStateOption].Bytes()[0])
				s.countState(s.counters.stateCounters, state)

				if s.isFinalState(state) {
					go s.counters.successCount.Increment()
				}

			case pdu.UnbindID:
				log.Println("ERROR: They are unbinding me :(")
			case pdu.SubmitSMRespID:
				// Fix something florix?
			default:
				go log.Println(p.Header().ID.String(), p.Header().Status.Error())
				go s.counters.unknownRespCount.Increment()
			}
		}

		transceiver := s.getTransceiver()
		transceiver.Handler = transceiverHandler

		conn := transceiver.Bind() // make persistent connection.
		defer transceiver.Close()
		// make sure connection is alright
		for c := range conn {
			if c.Error() == nil {
				break
			}
			log.Println("ERROR: connection failed:", c.Error())
		}
		// close connection if Interrupted
		closeTransceiverOnSignal(transceiver)

		t := &transceiverConn{
			transceiver: transceiver,
			err:         nil,
			m:           &sync.RWMutex{},
		}

		// report error on failed conn and Increment error count
		go func(transceiverIndex int) {
			for c := range conn {
				s.setTransceiverErr(transceiverIndex, c.Error())
				if c.Error() != nil {
					go s.counters.connErrorCount.Increment()
					log.Printf("ERROR: transciever %v SMPP connection status: %v %v", transceiverIndex, c.Status(), c.Error())
				}
			}
		}(i)

		s.transceivers[i] = t
	}

	go func() {
		now := time.Now()
		burstLimit := 100
		rl := rate.NewLimiter(rate.Limit(s.MessageRate), burstLimit)
		var dest int
		currTransceiver := 0
		i := 0
		for i < s.NumMessages {

			if s.Mode == "dynamic" {
				dest = s.Dst + i
			} else {
				dest = s.Dst
			}

			req := &smpp.ShortMessage{
				Src:      s.Src,
				Dst:      strconv.Itoa(dest),
				Text:     pdutext.Raw(s.MessageText),
				Register: pdufield.FinalDeliveryReceipt,
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
			if !s.checkTransceiverErr(currTransceiver) {
				go s.submitMessage(currTransceiver, req)
				s.counters.submittedCount.Increment()
				i++
			} else {
				i--
				go s.counters.connErrorCount.Increment()
			}
		}
		log.Println("Time elapsed sending:", time.Since(now))
	}()

	now := time.Now()
	loopTime := 100 * time.Millisecond
	loops := s.Wait * int(time.Second/loopTime)

	for i := 0; i < loops; i += 1 {
		time.Sleep(loopTime)
		if s.counters.successCount.Val()+s.counters.unknownRespCount.Val()+s.counters.sendErrorCount.Val() >= s.NumMessages {
			break
		}
		// Every 10 secs print a progress
		if i%100 == 0 {
			s.printStats(now)
		}
	}
	if s.counters.successCount.Val()+s.counters.unknownRespCount.Val()+s.counters.sendErrorCount.Val() < s.NumMessages {
		log.Println("WARNING: Waiting time is over and didn't receive enough responses.")
	}
	s.printStats(now)

}

func (s *SMPPerf) printStats(now time.Time) {
	log.Println("Time since start:", time.Since(now))
	log.Println("successCount:", s.counters.successCount.Val())
	log.Println("unknownRespCount:", s.counters.unknownRespCount.Val())
	log.Println("sendErrorCount:", s.counters.sendErrorCount.Val())
	log.Println("connErrorCount:", s.counters.connErrorCount.Val())
	log.Println("Submitted Messages:", s.counters.submittedCount.Val())
	for k, v := range s.counters.stateCounters.GetAll() {
		log.Println(k, v)
	}
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
