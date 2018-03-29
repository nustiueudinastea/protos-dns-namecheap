package main

import (
	"errors"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	namecheap "github.com/billputer/go-namecheap"
	dns "github.com/miekg/dns"
	resource "github.com/nustiueudinastea/protos/resource"
	protos "github.com/nustiueudinastea/protoslib-go"
	logrus "github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

var log = logrus.New()
var domain string

func stringInSlice(a string, list []string) (bool, int) {
	for i, b := range list {
		if strings.TrimSuffix(b, ".") == strings.TrimSuffix(a, ".") {
			return true, i
		}
	}
	return false, 0
}

func waitQuit(pclient protos.Protos) {
	sigchan := make(chan os.Signal, 10)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	<-sigchan
	log.Info("Deregistering as DNS provider")
	err := pclient.DeregisterProvider("dns")
	if err != nil {
		log.Error("Could not deregister as DNS provider: ", err.Error())
	}
	log.Info("Stopping Namecheap DNS provider")

	os.Exit(0)
}

func compareRecords(protosHosts []namecheap.DomainDNSHost, namecheapHosts []namecheap.DomainDNSHost) bool {
	// The following 'if' is required because Namecheap has two default hosts (www and @) for a domain that doesn't have any custom hosts
	if len(protosHosts) == 0 && len(namecheapHosts) == 2 && namecheapHosts[0].Name == "www" && namecheapHosts[1].Name == "@" {
		return true
	}
	if len(protosHosts) != len(namecheapHosts) {
		return false
	}
	var matchCount = 0
	for _, phost := range protosHosts {
		for _, nhost := range namecheapHosts {
			if strings.TrimSuffix(phost.Address, ".") == strings.TrimSuffix(nhost.Address, ".") && strings.ToLower(phost.Name) == strings.ToLower(nhost.Name) && phost.TTL-120 < nhost.TTL && phost.TTL+120 > nhost.TTL && strings.ToLower(phost.Type) == strings.ToLower(nhost.Type) {
				matchCount++
			}
		}
	}
	if len(protosHosts) != matchCount {
		return false
	}
	return true
}

func lookUpDNS(dmn string, rtype string) ([]string, error) {
	c := dns.Client{}
	m := dns.Msg{}

	server := "8.8.8.8"
	log.Debugf("Checking DNS record %s of type %s using server %s", dmn, rtype, server)

	// Setting the question
	switch strings.ToUpper(rtype) {
	case "TXT":
		m.SetQuestion(dmn, dns.TypeTXT)
	case "A":
		m.SetQuestion(dmn, dns.TypeA)
	case "MX":
		m.SetQuestion(dmn, dns.TypeMX)
	default:
		return []string{""}, errors.New("DNS record type " + rtype + " not supported")
	}

	// Performing the question
	r, _, err := c.Exchange(&m, server+":53")
	if err != nil {
		return []string{""}, err
	}

	if len(r.Answer) == 0 {
		return []string{""}, errors.New("No DNS result for record " + dmn)
	}
	result := []string{}
	for _, ans := range r.Answer {
		parts := strings.Split(ans.String(), "\t")
		value := strings.Replace(parts[4], "\"", "", -1)
		if strings.ToUpper(rtype) == "MX" {
			value = strings.Split(value, " ")[1]
		}
		result = append(result, value)
	}
	return result, nil
}

func checkRecords(protosHosts []namecheap.DomainDNSHost) bool {
	for _, record := range protosHosts {

		var fqdn string
		if record.Name == "@" {
			fqdn = domain + "."
		} else {
			fqdn = record.Name + "." + domain + "."
		}
		values, err := lookUpDNS(fqdn, record.Type)
		if err != nil {
			log.Warnf("Record %s does not have a value: %s", record.Name, err.Error())
			return false
		}
		if ok, _ := stringInSlice(record.Address, values); ok == false {
			log.Warnf("Record %s does not have value %s", record.Name, record.Address)
			return false
		}
	}
	return true
}

func activityLoop(interval time.Duration, domain string, protosURL string, apiuser string, apitoken string, username string) {

	domainParts := strings.Split(domain, ".")

	appID, err := protos.GetAppID()
	if err != nil {
		log.Fatal(err)
	}

	log.Info("Starting with a check interval of ", interval*time.Second)
	log.Info("Using ", protosURL, " to connect to Protos.")

	// Clients to interact with Protos and Namecheap
	pclient := protos.NewClient(protosURL, appID)
	nclient := namecheap.NewClient(apiuser, apitoken, username)

	go waitQuit(pclient)

	// Each service provider needs to register with protos
	log.Info("Registering as DNS provider")
	time.Sleep(4 * time.Second) // Giving Docker some time to assign us an IP
	err = pclient.RegisterProvider("dns")
	if err != nil {
		if strings.Contains(err.Error(), "already registered") {
			log.Error("Failed to register as DNS provider: ", strings.TrimRight(err.Error(), "\n"))
		} else {
			log.Fatal("Failed to register as DNS provider: ", err)
		}
	}

	// Checking that the given domain exists in the Namecheap account
	log.Info("Checking domain ", domain)
	domainInfo, err := nclient.DomainGetInfo(domain)
	if err != nil {
		log.Fatal("Cant find domain: ", domain, ". ", err)
	}
	log.Info("Found domain ", domain, " with nameservers ", domainInfo.DNSDetails.Nameservers)

	// The following periodically checks the resources and creates new ones in Namecheap
	first := true
	for {

		if first == false {
			time.Sleep(interval * time.Second)
		}
		first = false

		// Retrieving Protos resources
		resources, err := pclient.GetResources()
		if err != nil {
			log.Error(err)
			continue
		}
		newHosts := []namecheap.DomainDNSHost{}
		logResources := map[string]*resource.Resource{}
		for id, rsc := range resources {
			var record *resource.DNSResource
			record = rsc.Value.(*resource.DNSResource)
			host := namecheap.DomainDNSHost{Name: record.Host, Type: record.Type, Address: record.Value, TTL: record.TTL}
			newHosts = append(newHosts, host)
			logResources[id] = resources[id]
		}
		log.Debugf("Retrieved %v resources from Protos: %v", len(resources), logResources)

		// Retrieving all subdomains for given domain
		domainHosts, err := nclient.DomainsDNSGetHosts(domainParts[0], domainParts[1])
		if err != nil {
			log.Error(err)
			continue
		}
		log.Debugf("Retrieved %v hosts from Namecheap: %v", len(domainHosts.Hosts), domainHosts.Hosts)

		if compareRecords(newHosts, domainHosts.Hosts) {
			log.Debug("Records are the same. Doing nothing")
		} else {
			log.Info("Records are not the same. Synchronizing.")
			domainParts := strings.Split(domain, ".")
			_, err := nclient.DomainDNSSetHosts(domainParts[0], domainParts[1], newHosts)
			if err != nil {
				log.Error(err)
				continue
			}

			for checkRecords(newHosts) == false {
				log.Debug("Records not active yet. Creating bogus record. HACK")
				testHost := namecheap.DomainDNSHost{Name: "temp", Type: "TXT", Address: strconv.FormatInt(time.Now().Unix(), 10)}
				extraHosts := append(newHosts, testHost)
				nclient.DomainDNSSetHosts(domainParts[0], domainParts[1], extraHosts)
				time.Sleep(20 * time.Second)
			}

			nclient.DomainDNSSetHosts(domainParts[0], domainParts[1], newHosts)
			log.Info("All records have been created and are active")
			log.Info("Updating the status for all DNS resources")
			pclient.SetStatusBatch(resources, "created")

		}

	}
}

func main() {

	app := cli.NewApp()
	app.Name = "protos-dns-namecheap"
	app.Author = "Alex Giurgiu"
	app.Email = "alex@giurgiu.io"
	app.Version = "0.0.1"

	var apiuser string
	var apitoken string
	var username string
	var protosURL string
	var interval int
	var loglevel string

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "username",
			Usage:       "Specify your Namecheap username",
			Destination: &username,
		},
		cli.StringFlag{
			Name:        "apiuser",
			Usage:       "Specify your Namecheap API user",
			Destination: &apiuser,
		},
		cli.StringFlag{
			Name:        "token",
			Usage:       "Specify your Namecheap API token",
			Destination: &apitoken,
		},
		cli.StringFlag{
			Name:        "domain",
			Usage:       "Specify your Namecheap hosted domain",
			Destination: &domain,
		},
		cli.IntFlag{
			Name:        "interval",
			Value:       30,
			Usage:       "Specify check interval in seconds",
			Destination: &interval,
		},
		cli.StringFlag{
			Name:        "loglevel",
			Value:       "info",
			Usage:       "Specify log level: debug, info, warn, error",
			Destination: &loglevel,
		},
		cli.StringFlag{
			Name:        "protosurl",
			Value:       "http://protos:8080/",
			Usage:       "Specify url used to connect to Protos API",
			Destination: &protosURL,
		},
	}

	app.Before = func(c *cli.Context) error {
		level, err := logrus.ParseLevel(loglevel)
		if err != nil {
			return err
		}
		log.SetLevel(level)
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:  "start",
			Usage: "start the Namecheap DNS service",
			Action: func(c *cli.Context) {
				if username == "" || apiuser == "" || apitoken == "" || domain == "" {
					log.Fatal("username, apiuser, token and domain are required.")
				}

				activityLoop(time.Duration(interval), domain, protosURL, apiuser, apitoken, username)
			},
		},
	}

	app.Run(os.Args)
}
