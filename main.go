package main

import (
	"fmt"
	"log"
	"flag"
	"github.com/VictorLowther/wsman"
	"github.com/VictorLowther/simplexml/search"
	"os"
	"strings"
)

func hasIdrac(client *wsman.Client) bool {
	res, err := client.Identify()
	if err != nil {
		log.Printf("No WSMAN endpoint at %s\n", client.Endpoint())
		return false
	}
	if res.Fault() != nil {
		log.Printf("SOAP fault communicating with %s\n", client.Endpoint())
		return false
	}
	n := search.First(search.Tag("ProductName","*"),res.AllBodyElements())
	if n != nil && string(n.Content) == "iDRAC" {
		log.Printf("Found iDRAC at %s\n",client.Endpoint())
		return true
	}
	log.Printf("No iDRAC at WSMAN endpoint %s\n",client.Endpoint())
	return false
}

func main() {
	addrs := flag.String("scan","","Comma-seperated list of IP addresses to scan for iDRAC presence")
	username := flag.String("u","","Username to try to log in as")
	password := flag.String("p","","Password to try and use")
	flag.Parse()
	if *addrs == "" {
		flag.Usage()
		os.Exit(1)
	}
	for _,addr := range strings.Split(*addrs,",") {
		endpoint := fmt.Sprintf("https://%s/wsman",strings.TrimSpace(addr))
		client := wsman.NewClient(endpoint,*username,*password)
		if hasIdrac(client) {
			fmt.Printf("iDRAC at %s\n",client.Endpoint())
		}
	}
}

		
