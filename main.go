package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/VictorLowther/simplexml/search"
	"github.com/VictorLowther/wsman"
	"log"
	"math/big"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

type NodeInfo struct {
	PmType     string   `json:"pm_type"`
	PmUser     string   `json:"pm_user"`
	PmPassword string   `json:"pm_password"`
	PmAddr     string   `json:"pm_addr"`
	Mac        []string `json:"mac"`
	Cpu        string   `json:"cpu"`
	Memory     string   `json:"memory"`
	Disk       string   `json:"disk"`
	Arch       string   `json:"arch"`
}

type Nodes struct {
	Nodes []*NodeInfo `json:"nodes"`
}

type scanInfo struct {
	addr               net.IP
	username, password string
}

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
	n := search.First(search.Tag("ProductName", "*"), res.AllBodyElements())
	if n != nil && string(n.Content) == "iDRAC" {
		log.Printf("Found iDRAC at %s\n", client.Endpoint())
		return true
	}
	log.Printf("No iDRAC at WSMAN endpoint %s\n", client.Endpoint())
	return false
}

func getMemory(client *wsman.Client) string {
	msg := client.Enumerate("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_SystemView")
	msg.Selectors("InstanceID", "System.Embedded.1")
	res, err := msg.Send()
	if err != nil {
		log.Printf("Error getting memory: %v\n", err)
		return "-1"
	}
	n := search.First(search.Tag("SysMemTotalSize", "*"), res.AllBodyElements())
	if n == nil {
		log.Println("Could not find total system memory")
		return "-1"
	}
	return string(n.Content)
}

func getDisk(client *wsman.Client) string {
	msg := client.Enumerate("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_VirtualDiskView")
	msg.Selectors("InstanceID", "System.Embedded.1")
	res, err := msg.Send()
	if err != nil {
		log.Printf("Error getting disks: %v\n", err)
		return "-1"
	}
	vds := search.All(search.Tag("DCIM_VirtualDiskView", "*"), res.AllBodyElements())
	return strconv.Itoa(len(vds))
}

func getCPU(client *wsman.Client) string {
	msg := client.Enumerate("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_CPUView")
	msg.Selectors("InstanceID", "System.Embedded.1")
	res, err := msg.Send()
	if err != nil {
		log.Printf("Error getting cpus: %v\n", err)
		return "-1"
	}
	activeCores := 0
	procs := search.All(search.Tag("DCIM_CPUView", "*"), res.AllBodyElements())
	for _, proc := range procs {
		cores := search.First(search.Tag("NumberOfEnabledCores", "*"), proc.Children())
		if cores == nil {
			log.Println("Could not find number of enabled cores!")
			os.Exit(1)
		}
		count, err := strconv.Atoi(string(cores.Content))
		if err != nil {
			log.Println("Error parsing %s into an integer\n", string(cores.Content))
			os.Exit(1)
		}
		activeCores += count
	}
	return strconv.Itoa(activeCores)
}

func getMACs(client *wsman.Client) []string {
	result := []string{}
	msg := client.Enumerate("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_NICView")
	msg.Selectors("InstanceID", "System.Embedded.1")
	res, err := msg.Send()
	if err != nil {
		log.Printf("Error getting nics: %v\n", err)
		return result
	}
	macs := search.All(search.Tag("CurrentMACAddress", "*"), res.AllBodyElements())
	for _, mac := range macs {
		m := strings.ToLower(string(mac.Content))
		result = append(result, m)
	}
	sort.Strings(result)
	return result
}

func scanOne(in chan *scanInfo, out chan *NodeInfo, done chan int) {
	for {
		c, ok := <-in
		if !ok {
			break
		}
		endpoint := ""
		if c.addr.To4() != nil {
			endpoint = fmt.Sprintf("https://%s/wsman", c.addr.String())
		} else if c.addr.To16() != nil {
			endpoint = fmt.Sprintf("https://[%s]/wsman", c.addr.String())
		} else {
			log.Printf("Address %s is not useable!", c.addr.String())
			continue
		}
		client := wsman.NewClient(endpoint, c.username, c.password)
		if hasIdrac(client) {
			node := &NodeInfo{
				PmType:     "pxe_drac",
				PmUser:     c.username,
				PmPassword: c.password,
				PmAddr:     c.addr.String(),
				Memory:     getMemory(client),
				Cpu:        getCPU(client),
				Disk:       getDisk(client),
				Arch:       "x86_64",
				Mac:        getMACs(client),
			}
			out <- node
		}
	}
	done <- 1
}

func scan(addrs, username, password string) (res []*NodeInfo) {
	res = []*NodeInfo{}
	workers := 100
	scanChan := make(chan *scanInfo, workers)
	resChan := make(chan *NodeInfo)
	doneChan := make(chan int)
	// Make some workers
	for i := 0; i < workers; i++ {
		go scanOne(scanChan, resChan, doneChan)
	}
	// Produce a stream of work for them.
	go func() {
		for _, addr := range strings.Split(addrs, ",") {
			addrRange := strings.SplitN(addr, "-", 2)
			if len(addrRange) == 1 {
				toScan := net.ParseIP(strings.TrimSpace(addr))
				if toScan == nil {
					log.Printf("Invalid IP address%s\n", addr)
					continue
				}
				scanChan <- &scanInfo{addr: toScan, username: username, password: password}
			} else {
				first := net.ParseIP(addrRange[0])
				last := net.ParseIP(addrRange[1])
				if first == nil || last == nil {
					log.Printf("Invalid IP address in %s\n", addrs)
					continue
				}
				start := big.NewInt(int64(0))
				end := big.NewInt(int64(0))
				stride := big.NewInt(int64(1))
				start.SetBytes([]byte(first))
				end.SetBytes([]byte(last))
				for i := big.NewInt(int64(0)).Set(start); i.Cmp(end) < 1; i.Add(i, stride) {
					numBytes := i.Bytes()
					numLen := len(numBytes)
					toScan := make([]byte, 16, 16)
					copy(toScan[16-numLen:], numBytes)
					scanChan <- &scanInfo{addr: toScan, username: username, password: password}
				}
			}
		}
		close(scanChan)
	}()
	// Gather the results
	for workers > 0 {
		select {
		case c1 := <-doneChan:
			workers -= c1
		case c2 := <-resChan:
			res = append(res, c2)
		}
	}
	close(doneChan)
	close(resChan)
	return res
}

func main() {
	addrs := flag.String("scan", "", "Comma-seperated list of IP addresses to scan for iDRAC presence")
	username := flag.String("u", "", "Username to try to log in as")
	password := flag.String("p", "", "Password to try and use")
	flag.Parse()
	if *addrs != "" {
		nodes := &Nodes{Nodes: scan(*addrs, *username, *password)}
		res, err := json.MarshalIndent(nodes, "", "  ")
		if err != nil {
			log.Printf("Error formatting output: %v\n", err)
		}
		os.Stdout.Write(res)
		os.Exit(0)
	}
	flag.Usage()
	os.Exit(1)
}
