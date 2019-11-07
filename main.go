package main

import (
	"flag"
	"log"
	"os"

	"github.com/abligh/gonbdserver/nbd"

	"github.com/javgh/sia-nbdserver/nbdadapter"
	"github.com/javgh/sia-nbdserver/siaadapter"
)

func runInit() {
	err := siaadapter.InitFromFile(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
}

func runServe() {
	flag.Parse()

	getSiaReaderWriter := func(size uint64) (nbdadapter.SiaReaderWriter, error) {
		return siaadapter.New(size)
	}
	siaBackendFactory := nbdadapter.NewSiaBackendFactory(getSiaReaderWriter)
	nbd.RegisterBackend("sia", siaBackendFactory)
	nbd.Run(nil)
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "init" {
		runInit()
	} else {
		runServe()
	}
}
