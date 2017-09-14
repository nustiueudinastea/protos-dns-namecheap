package main

import (
	"os"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	namecheap "github.com/billputer/go-namecheap"
	"github.com/nustiueudinastea/protoslib-go"
	"gopkg.in/urfave/cli.v1"
)

var log = logrus.New()

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
			if phost.Address == nhost.Address && phost.Name == nhost.Name && phost.TTL == nhost.TTL && phost.Type == nhost.Type {
				matchCount++
			}
		}
	}
	if len(protosHosts) != matchCount {
		return false
	}
	return true
}

func activityLoop(interval time.Duration, domain string, protosURL string, apiuser string, apitoken string, username string) {

	domainParts := strings.Split(domain, ".")

	log.Info("Starting with a check interval of ", interval*time.Second)
	log.Info("Using ", protosURL, " to connect to Protos.")

	// Clients to interact with Protos and Namecheap
	pclient := protos.NewClient(protosURL)
	nclient := namecheap.NewClient(apiuser, apitoken, username)

	// Each service provider needs to register with protos
	log.Info("Registering as DNS provider")
	err := pclient.RegisterProvider("dns")
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
	for {

		time.Sleep(interval * time.Second)

		// Retrieving Protos resources
		resources, err := pclient.GetResources()
		if err != nil {
			log.Error(err)
			continue
		}
		newHosts := []namecheap.DomainDNSHost{}
		logResources := map[string]protos.Resource{}
		for id, resource := range resources {
			var record protos.DNSResource
			record = resource.Record.(protos.DNSResource)
			host := namecheap.DomainDNSHost{Name: record.Host, Type: record.Type, Address: record.Value, TTL: record.TTL}
			newHosts = append(newHosts, host)
			logResources[id] = *resource
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
			} else {
				log.Info("Updating the status for all DNS resources")
				pclient.SetStatusBatch(resources, "created")
			}
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
	var domain string
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
		if loglevel == "debug" {
			log.Level = logrus.DebugLevel
		} else if loglevel == "info" {
			log.Level = logrus.InfoLevel
		} else if loglevel == "warn" {
			log.Level = logrus.WarnLevel
		} else if loglevel == "error" {
			log.Level = logrus.ErrorLevel
		}
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
