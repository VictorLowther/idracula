package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/VictorLowther/simplexml/dom"
	"github.com/VictorLowther/simplexml/search"
	"github.com/VictorLowther/wsman"
	"log"
	"math/big"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
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

func waitForJob(client *wsman.Client, jobinfo *dom.Element) bool {
	jobID := string(search.First(search.Attr("Name", "*", "InstanceID"), jobinfo.All()).Content)
	log.Printf("%s: Waiting for job %s to finish\n", client.Endpoint(), jobID)
	var code string
	ret := false
	for {
		time.Sleep(10 * time.Second)
		msg := client.Get("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_LifecycleJob")
		msg.Selectors("InstanceID", jobID)
		res, err := msg.Send()
		if err != nil {
			log.Printf("Error monitoring job: %v\n", err)
			if res != nil {
				log.Printf("Response: %s\n", res.String())
			}
			goto out
		}
		code = strings.TrimSpace(
			string(search.First(
				search.Tag("JobStatus", "*"),
				res.AllBodyElements()).Content))
		switch code {
		case "Completed":
			ret = true
			goto out
		case "Completed with Errors":
			goto out
		case "Failed":
			goto out
		}
	}
out:
	log.Printf("Job %s finished with %s\n", jobID, code)
	return ret
}

func hasIdrac(client *wsman.Client) bool {
	res, err := client.Identify()
	if err != nil {
		log.Printf("No WSMAN endpoint at %s\n", client.Endpoint())
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

func getBootNic(client *wsman.Client, nics []*dom.Element) *dom.Element {
	fqdds := []string{}
	for _, nic := range nics {
		n := search.First(search.Tag("FQDD", "*"), nic.Children())
		if n == nil {
			log.Printf("Nic did not contain an FQDD")
			os.Exit(1)
		}
		fqdd := string(n.Content) // Only care about integrated nics
		if ! strings.HasPrefix(fqdd, "NIC.Integrated.") {
			log.Printf("%s is not integrated, skipping\n",fqdd)
			continue
		}
		speed := search.First(search.Tag("LinkSpeed","*"),nic.Children())
		// If there is not speed setting, then the server is too old to report it.
		// Happily enough, that also means it is too old for 10 gig ports to be a thing.
		if speed != nil && string(speed.Content) != "3" {
			log.Printf("%s is not a gigabit Ethernet port\n",fqdd)
			continue
		}
		fqdds = append(fqdds, fqdd)
	}
	if len(fqdds) < 1 {
		log.Printf("No integrated 1 GB nics!")
		os.Exit(1)
	}
	sort.Strings(fqdds)
	bootnic := fqdds[0]
	result := search.First(search.Content([]byte(bootnic)), nics[0].Parent().All()).Parent()
	if result == nil {
		log.Printf("Unable to find NIC with FQDD %s\n", bootnic)
		return nil
	}
	// Now, make sure it can PXE boot
	msg := client.Get("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_NICEnumeration")
	msg.Selectors("InstanceID", bootnic+":LegacyBootProto")
	res, err := msg.Send()
	if err != nil {
		log.Printf("Error checking whether %s can PXE boot: %v\n", bootnic, err)
		return result
	}
	currentval := string(search.First(search.Tag("CurrentValue", "*"), res.AllBodyElements()).Content)
	if currentval == "PXE" {
		return result
	}
	msg = client.Invoke("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_NICService", "SetAttribute")
	msg.Selectors(
		"SystemCreationClassName", "DCIM_ComputerSystem",
		"CreationClassName", "DCIM_NICService",
		"SystemName", "DCIM:ComputerSystem",
		"Name", "DCIM:NICService")
	msg.Parameters(
		"Target", bootnic,
		"AttributeName", "LegacyBootProto",
		"AttributeValue", "PXE")
	res, err = msg.Send()
	if err != nil {
		log.Printf("Error ensuring %s can PXE boot: %v\n", bootnic, err)
		return result
	}
	retvals := search.First(search.Tag("SetAttribute_OUTPUT", "*"), res.AllBodyElements())
	if retvals == nil {
		log.Printf("Method invocation result did not return an output element!\n%s\n", res.String())
		return result
	}
	code := search.First(search.Tag("ReturnValue", "*"), retvals.Children())
	if string(code.Content) != "0" {
		log.Printf("Error ensuring NIC %s can PXE boot:\n%s\n", bootnic, res.String())
		return result
	}
	needReboot := string(search.First(search.Tag("RebootRequired", "*"), retvals.Children()).Content)
	if needReboot != "Yes" {
		return result
	}
	// Create the config job.
	msg = client.Invoke("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_NICService", "CreateTargetedConfigJob")
	msg.Selectors(
		"SystemCreationClassName", "DCIM_ComputerSystem",
		"CreationClassName", "DCIM_NICService",
		"SystemName", "DCIM:ComputerSystem",
		"Name", "DCIM:NICService")
	msg.Parameters(
		"Target", bootnic,
		"RebootJobType", "1",
		"ScheduledStartTime", "TIME_NOW")
	res, err = msg.Send()
	if err != nil {
		log.Printf("Error ensuring %s can PXE boot: %v\n", bootnic, err)
		return result
	}
	retvals = search.First(search.Tag("CreateTargetedConfigJob_OUTPUT", "*"), res.AllBodyElements())
	if retvals == nil {
		log.Printf("Method invocation result did not return an output element!\n%s\n", res.String())
		return result
	}
	code = search.First(search.Tag("ReturnValue", "*"), retvals.Children())
	if string(code.Content) != "4096" {
		log.Printf("Error ensuring NIC %s can PXE boot:\n%s\n", bootnic, res.String())
		return result
	}
	job_service := search.First(search.Tag("ReferenceParameters", "*"), res.AllBodyElements())
	if job_service == nil {
		log.Printf("Did not get job info back!")
		return result
	}
	waitForJob(client, job_service)
	return result
}

func getMAC(client *wsman.Client) string {
	msg := client.Enumerate("http://schemas.dell.com/wbem/wscim/1/cim-schema/2/DCIM_NICView")
	msg.Selectors("InstanceID", "System.Embedded.1")
	res, err := msg.Send()
	if err != nil {
		log.Printf("Error getting nics: %v\n", err)
		return ""
	}
	bootnic := getBootNic(client,
		search.All(search.Tag("DCIM_NICView", "*"), res.AllBodyElements()))
	if bootnic == nil {
		return ""
	}
	return strings.ToLower(
		string(search.First(
			search.Tag("CurrentMACAddress", "*"),
			bootnic.Children()).Content))
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
				Mac:        []string{getMAC(client)},
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
	addrs := flag.String("scan", "", "Comma-seperated list of IP addresses to scan for iDRAC presence.\n      Ranges are allowed, and must be seperated by a hyphen.\n      IP4 and IP6 compatible, but only IP4 addresses tested.")
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
