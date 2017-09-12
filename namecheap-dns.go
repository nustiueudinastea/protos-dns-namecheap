package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	namecheap "github.com/billputer/go-namecheap"
	"gopkg.in/urfave/cli.v1"
)

type DNSResource struct {
	Host  string `json:"host"`
	Value string `json:"value" hash:"-"`
	Type  string `json:"type"`
	TTL   int    `json:"ttl" hash:"-"`
}

type Resource struct {
	Type   string      `json:"type"`
	Record DNSResource `json:"value"`
	Status string      `json:"status"`
	ID     string      `json:"id"`
}

var apiuser string
var apitoken string
var username string
var domain string
var protosURL string

var log = logrus.New()

//
// Namecheap operations
//

func createResource(resource Resource) {
	client := namecheap.NewClient(apiuser, apitoken, username)

	// Get a list of your domains
	domains, _ := client.DomainsGetList()
	for _, domain := range domains {
		fmt.Printf("Domain: %+v\n\n", domain.Name)
	}
}

func checkDomain(dom string) (*namecheap.DomainInfo, error) {
	log.Info("Checking domain ", dom)
	client := namecheap.NewClient(apiuser, apitoken, username)
	domainInfo, err := client.DomainGetInfo(dom)
	return domainInfo, err
}

func getDomainHosts(domain string) (*namecheap.DomainDNSGetHostsResult, error) {
	client := namecheap.NewClient(apiuser, apitoken, username)
	domainParts := strings.Split(domain, ".")
	domainHosts, err := client.DomainsDNSGetHosts(domainParts[0], domainParts[1])
	return domainHosts, err
}

func setDomainHost(domain string, name string, hosttype string, address string, ttl int) (*namecheap.DomainDNSSetHostsResult, error) {
	client := namecheap.NewClient(apiuser, apitoken, username)
	domainParts := strings.Split(domain, ".")
	host := namecheap.DomainDNSHost{Name: name, Type: hosttype, Address: address, TTL: ttl}
	return client.DomainDNSSetHosts(domainParts[0], domainParts[1], []namecheap.DomainDNSHost{host})
}

//
// Protos operations
//

func getDNSResources() (map[string]Resource, error) {
	client := &http.Client{}
	resourcesReq, err := http.NewRequest("GET", protosURL+"internal/resource/provider", nil)
	resources := make(map[string]Resource)
	resp, err := client.Do(resourcesReq)
	if err != nil {
		return map[string]Resource{}, err
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&resources)
	if err != nil {
		return map[string]Resource{}, err
	}
	resp.Body.Close()
	log.Debug("Found ", len(resources), " DNS resources.")
	return resources, nil
}

func setResourceStatus(resourceID string, rstatus string) error {

	log.Info("Setting status for resource ", resourceID)
	statusJSON, err := json.Marshal(&struct {
		Status string `json:"status"`
	}{
		Status: rstatus,
	})
	if err != nil {
		return err
	}

	url := protosURL + "internal/resource/" + resourceID
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(statusJSON))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	return nil
}

func setStatusBatch(resources map[string]Resource, rstatus string) {
	for _, resource := range resources {
		err := setResourceStatus(resource.ID, rstatus)
		if err != nil {
			log.Error("Could not set status for resource ", resource.ID, ": ", err)
		}
	}
}

func regsiterDNSProvider() error {
	var jsonStr = []byte(`{"type": "dns"}`)
	req, err := http.NewRequest("POST", protosURL+"internal/provider", bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func compareRecords(protosHosts []namecheap.DomainDNSHost, namecheapHosts []namecheap.DomainDNSHost) bool {
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

func activityLoop(interval time.Duration) {

	log.Info("Starting with a check interval of ", interval*time.Second)
	log.Info("Using ", protosURL, " to connect to Protos.")

	client := namecheap.NewClient(apiuser, apitoken, username)
	log.Info("Registering as DNS provider")
	err := regsiterDNSProvider()
	if err != nil {
		log.Error("Failed to register as DNS provider: ", err)
		os.Exit(1)
	}

	domainInfo, err := checkDomain(domain)
	if err != nil {
		log.Error("Cant find domain: ", domain, ". ", err)
		os.Exit(1)
	}
	log.Info("Found domain ", domain, " with nameservers ", domainInfo.DNSDetails.Nameservers)

	for {

		resources, err := getDNSResources()
		if err != nil {
			log.Error(err)
		}
		domainHosts, err := getDomainHosts(domain)
		if err != nil {
			log.Error(err)
		}

		newHosts := []namecheap.DomainDNSHost{}
		for _, resource := range resources {
			record := resource.Record
			host := namecheap.DomainDNSHost{Name: record.Host, Type: record.Type, Address: record.Value, TTL: record.TTL}
			newHosts = append(newHosts, host)
		}

		if compareRecords(newHosts, domainHosts.Hosts) {
			log.Info("Records are the same")
			setStatusBatch(resources, "created")
		} else {
			log.Info("Records are not the same. Creating all hosts")
			domainParts := strings.Split(domain, ".")
			_, err := client.DomainDNSSetHosts(domainParts[0], domainParts[1], newHosts)
			if err != nil {
				log.Error(err)
			} else {
				setStatusBatch(resources, "created")
			}
		}

		time.Sleep(interval * time.Second)

	}
}

func main() {

	app := cli.NewApp()
	app.Name = "protos-dns-namecheap"
	app.Author = "Alex Giurgiu"
	app.Email = "alex@giurgiu.io"
	app.Version = "0.0.1"

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
			Action: func(c *cli.Context) error {
				if username == "" || apiuser == "" || apitoken == "" || domain == "" {
					log.Fatal("username, apiuser, token and domain are required.")
				}
				activityLoop(time.Duration(interval))
				return nil
			},
		},
	}

	app.Run(os.Args)
}
