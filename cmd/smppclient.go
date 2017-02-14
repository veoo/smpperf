package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdufield"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
)

func main() {

	toAddr := flag.String("to", "0", "to address")
	fromAddr := flag.String("from", "447779124374", "from address")
	user := flag.String("u", "0", "Username to use to connect.")
	password := flag.String("p", "0", "Password to connect.")
	address := flag.String("a", "0", "Address to connect to. host:port")

	flag.Parse()

	fmt.Println("to:", *toAddr)
	fmt.Println("from:", *fromAddr)

	text := strings.Join(flag.Args(), " ")

	fmt.Println("message:", text)

	if *toAddr == "0" {
		flag.Usage()
		os.Exit(1)
	}

	deliverChan := make(chan bool)
	transceiverHandler := func(p pdu.Body) {
		fmt.Println("RECEIVED SOMETHING", p.Header().ID.String())
		switch p.Header().ID {
		case pdu.DeliverSMID:
			f := p.Fields()
			src := f[pdufield.SourceAddr]
			dst := f[pdufield.DestinationAddr]
			txt := f[pdufield.ShortMessage]
			fmt.Println("TLVs:", p.TLVFields())
			// fmt.Println(string(p.TLVFields()[pdufield.MessageStateOption].Bytes()))
			// fmt.Println(string(p.TLVFields()[pdufield.ReceiptedMessageID].Bytes()))
			log.Info(fmt.Sprintf("Client: (DeliverSMID) Short message from=%s to=%s: %s", src, dst, txt))
			deliverChan <- true
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
	defer transceiver.Close()
	go func() {
		for c := range connA {
			log.Info("SMPP connection status: ", c.Status(), c.Error())
		}
	}()

	log.Info("Running basic tests")

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
	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(10 * time.Second)
		timeout <- true
	}()
	select {
	case <-deliverChan:
		return
	case <-timeout:
		log.Error("timed out waiting for response")
	}
}
