package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdufield"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
)

func main() {

	// implement toml config reading here

	transceiverHandler := func(p pdu.Body) {
		fmt.Println("RECEIVED SOMETHING", p.Header().ID.String())
		switch p.Header().ID {
		case pdu.DeliverSMID:
			f := p.Fields()
			src := f[pdufield.SourceAddr]
			dst := f[pdufield.DestinationAddr]
			txt := f[pdufield.ShortMessage]
			fmt.Println("TLVs:", p.TLVFields())
			fmt.Println(string(p.TLVFields()[pdufield.MessageStateOption].Bytes()))
			fmt.Println(string(p.TLVFields()[pdufield.ReceiptedMessageID].Bytes()))
			log.Info(fmt.Sprintf("Client: (DeliverSMID) Short message from=%s to=%s: %s", src, dst, txt))
		case pdu.EnquireLinkID:
			log.Info("Enquire link received, sending an enquire link resp")
			// TODO: Implement sending an enquire_link_resp here
		}
	}

	transceiver := &smpp.Transceiver{
		Addr:        address,
		User:        user,
		Passwd:      password,
		Handler:     transceiverHandler,
		RespTimeout: 10 * time.Second,
	}

	connA := transceiver.Bind() // make persistent connection.
	go func() {
		for c := range connA {
			log.Info("SMPP connection status: ", c.Status(), c.Error())
		}
	}()

	time.Sleep(2 * time.Second)
	req := &smpp.ShortMessage{
		Src:      *fromAddr,
		Dst:      *toAddr,
		Text:     pdutext.UCS2(text),
		Register: smpp.FinalDeliveryReceipt,
	}
	sm, err := transceiver.SubmitLongMsg(req)
	if err != nil {
		log.Fatal("Error:", err)
	}

	log.Info(sm)

	// Add signal interrupt here, currently need to confirm an unbund is sent
	// on CTRL - C
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Info("Received interrupt or sigterm unbinding.")
		transceiver.Close()
		done <- true
	}()

	<-done
	log.Info("Exited cleanly, all done!")
}
