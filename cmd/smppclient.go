package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/burntsushi/toml"
	"github.com/veoo/go-smpp/smpp"
	"github.com/veoo/go-smpp/smpp/pdu"
	"github.com/veoo/go-smpp/smpp/pdu/pdufield"
	"github.com/veoo/go-smpp/smpp/pdu/pdutext"
)

type Config struct {
	Address  string
	User     string
	Password string
	From     string
	To       string
	Content  string
	Encoding string
}

func main() {

	argsWithoutProg := os.Args[1:]
	config := ReadConfig(argsWithoutProg[0])

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
		Addr:        config.Address,
		User:        config.User,
		Passwd:      config.Password,
		Handler:     transceiverHandler,
		RespTimeout: 10 * time.Second,
	}

	connA := transceiver.Bind() // make persistent connection.
	go func() {
		for c := range connA {
			log.Info("SMPP connection status: ", c.Status(), c.Error())
		}
	}()

	var text pdutext.Codec
	switch config.Encoding {
	case "ucs2":
		text = pdutext.UCS2(config.Content)
	case "latin1":
		text = pdutext.Latin1(config.Content)
	default:
		text = pdutext.Raw(config.Content)
	}

	time.Sleep(2 * time.Second)
	req := &smpp.ShortMessage{
		Src:      config.From,
		Dst:      config.To,
		Text:     text,
		Register: smpp.FinalDeliveryReceipt,
	}

	var sm *smpp.ShortMessage
	var err error
	if len(config.Content) < 120 {
		log.Info("Submitting standard message")
		sm, err = transceiver.Submit(req)
	} else {
		log.Info("Submitting long message")
		sm, err = transceiver.SubmitLongMsg(req)
	}

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
		<-sigs
		log.Info("Received interrupt or sigterm unbinding.")
		transceiver.Close()
		done <- true
	}()

	<-done
	log.Info("Exited cleanly, all done!")
}

func ReadConfig(configfile string) Config {
	configLocation := "./" + configfile
	_, err := os.Stat(configLocation)
	if err != nil {
		log.Fatal("Config file is missing: ", configLocation)
	}

	var config Config
	if _, err := toml.DecodeFile(configLocation, &config); err != nil {
		log.Fatal(err)
	}

	return config
}
